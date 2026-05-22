package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMirrorSourceReadsEnv(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "user@host:/path/to/.chb")
	if got, want := MirrorSource(), "user@host:/path/to/.chb"; got != want {
		t.Fatalf("MirrorSource() = %q, want %q", got, want)
	}
	t.Setenv("CHB_SYNC_SOURCE", "")
	if got := MirrorSource(); got != "" {
		t.Fatalf("MirrorSource() = %q, want empty", got)
	}
}

func TestMirrorSourceTrimsWhitespace(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "   /tmp/foo\n")
	if got, want := MirrorSource(), "/tmp/foo"; got != want {
		t.Fatalf("MirrorSource() = %q, want %q", got, want)
	}
}

func TestMirrorEnabledRespectsNoMirrorOverride(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "/tmp/foo")
	if !MirrorEnabled(nil) {
		t.Fatalf("MirrorEnabled(nil) = false, want true when CHB_SYNC_SOURCE set")
	}
	if MirrorEnabled([]string{"--no-mirror"}) {
		t.Fatalf("MirrorEnabled with --no-mirror = true, want false")
	}
	t.Setenv("CHB_SYNC_SOURCE", "")
	if MirrorEnabled(nil) {
		t.Fatalf("MirrorEnabled(nil) = true when CHB_SYNC_SOURCE unset")
	}
}

func TestRequireOdooWriteCapability(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "")
	t.Setenv("ODOO_PASSWORD", "")
	if err := RequireOdooWriteCapability(); err != nil {
		t.Fatalf("not in mirror mode, want nil error, got %v", err)
	}
	t.Setenv("CHB_SYNC_SOURCE", "user@host:/path")
	t.Setenv("ODOO_PASSWORD", "")
	err := RequireOdooWriteCapability()
	if err == nil {
		t.Fatalf("mirror+no password: want error, got nil")
	}
	if !strings.Contains(err.Error(), "mirror mode") {
		t.Fatalf("error message %q should mention mirror mode", err.Error())
	}
	t.Setenv("ODOO_PASSWORD", "secret")
	if err := RequireOdooWriteCapability(); err != nil {
		t.Fatalf("mirror+password set: want nil, got %v", err)
	}
}

func TestFilterMirrorFlagsStripsNoMirror(t *testing.T) {
	got := FilterMirrorFlags([]string{"--verbose", "--no-mirror", "--since", "2024-01"})
	want := []string{"--verbose", "--since", "2024-01"}
	if !equalStringSlice(got, want) {
		t.Fatalf("FilterMirrorFlags = %v, want %v", got, want)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// captureRsyncCalls records every mirrorRunRsync invocation in argSets so a
// test can assert which rsync commands the mirror code issued.
type capturedRsync struct {
	argSets [][]string
	labels  []string
	err     error
}

func (c *capturedRsync) runner() mirrorRunner {
	return func(args []string, label string) error {
		c.argSets = append(c.argSets, append([]string(nil), args...))
		c.labels = append(c.labels, label)
		return c.err
	}
}

// withMirrorRunner swaps the package-level mirrorRunRsync for the duration
// of the test and restores it on cleanup. Returns the captured value.
func withMirrorRunner(t *testing.T) *capturedRsync {
	t.Helper()
	cap := &capturedRsync{}
	orig := mirrorRunRsync
	mirrorRunRsync = cap.runner()
	t.Cleanup(func() { mirrorRunRsync = orig })
	return cap
}

func TestMirrorPullSkippedWhenSourceUnset(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "")
	cap := withMirrorRunner(t)
	if err := MirrorPull(nil); err != nil {
		t.Fatalf("MirrorPull when unset: want nil err, got %v", err)
	}
	if len(cap.argSets) != 0 {
		t.Fatalf("expected no rsync calls when CHB_SYNC_SOURCE is unset, got %d", len(cap.argSets))
	}
}

func TestMirrorPullSkippedWhenNoMirrorFlag(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "/tmp/source")
	cap := withMirrorRunner(t)
	if err := MirrorPull([]string{"--no-mirror"}); err != nil {
		t.Fatalf("MirrorPull with --no-mirror: want nil err, got %v", err)
	}
	if len(cap.argSets) != 0 {
		t.Fatalf("expected no rsync calls when --no-mirror is passed, got %d", len(cap.argSets))
	}
}

func TestMirrorPullInvokesRsyncForDataOutboxAndSettings(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	src := filepath.Join(t.TempDir(), "source-mirror")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	t.Setenv("CHB_SYNC_SOURCE", src)
	cap := withMirrorRunner(t)
	if err := MirrorPull(nil); err != nil {
		t.Fatalf("MirrorPull: %v", err)
	}
	// Phase 3: data + 4 outbox legs + settings → 6 invocations.
	wantLabels := []string{
		"data",
		"nostr/outbox up", "nostr/outbox down",
		"nostr/sent up", "nostr/sent down",
		"settings",
	}
	if !equalStringSlice(cap.labels, wantLabels) {
		t.Fatalf("rsync labels = %v, want %v", cap.labels, wantLabels)
	}
	// data uses --delete; outbox/sent legs use --update.
	if !sliceContains(cap.argSets[0], "--delete") {
		t.Fatalf("data rsync args = %v, want --delete", cap.argSets[0])
	}
	for i := 1; i <= 4; i++ {
		if sliceContains(cap.argSets[i], "--delete") {
			t.Fatalf("%s rsync used --delete; outbox legs must be --update", cap.labels[i])
		}
		if !sliceContains(cap.argSets[i], "--update") {
			t.Fatalf("%s rsync did not use --update", cap.labels[i])
		}
	}
	// Settings must exclude the per-machine files.
	settingsArgs := cap.argSets[5]
	for _, mustExclude := range []string{"config.env", "nostr.json", ".nostr-keys.json", ".installed-defaults.json"} {
		if !sliceContains(settingsArgs, "--exclude="+mustExclude) {
			t.Fatalf("settings rsync args %v missing --exclude=%s", settingsArgs, mustExclude)
		}
	}
}

// TestMirrorOutboxUpFailureContinuesToDown asserts the "up first, then
// down, surface up errors as warnings only" ordering. A failing up-leg
// must not block the down-leg — otherwise a thin-client that can't write
// to the trusted host would never get new annotations from teammates.
func TestMirrorOutboxUpFailureContinuesToDown(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	src := filepath.Join(t.TempDir(), "source-mirror")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	t.Setenv("CHB_SYNC_SOURCE", src)
	cap := &capturedRsync{}
	orig := mirrorRunRsync
	mirrorRunRsync = func(args []string, label string) error {
		cap.argSets = append(cap.argSets, append([]string(nil), args...))
		cap.labels = append(cap.labels, label)
		if strings.HasSuffix(label, " up") {
			return errors.New("permission denied")
		}
		return nil
	}
	t.Cleanup(func() { mirrorRunRsync = orig })
	if err := MirrorPull(nil); err != nil {
		t.Fatalf("MirrorPull: %v", err)
	}
	// Up failures should not abort the loop; we still expect every leg
	// (data, 4 outbox legs, settings = 6 invocations).
	if len(cap.labels) != 6 {
		t.Fatalf("expected 6 invocations even when up legs fail, got %d (%v)", len(cap.labels), cap.labels)
	}
}

func TestMirrorPullSurfacesRsyncError(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	t.Setenv("CHB_SYNC_SOURCE", "/some/path")
	cap := withMirrorRunner(t)
	cap.err = errors.New("rsync boom")
	err := MirrorPull(nil)
	if err == nil || !strings.Contains(err.Error(), "rsync boom") {
		t.Fatalf("MirrorPull should surface rsync error, got %v", err)
	}
}

func TestMirrorRemoteSubpathHandlesRemoteAndLocal(t *testing.T) {
	cases := []struct{ base, sub, want string }{
		{"user@host:/abs/.chb", "data", "user@host:/abs/.chb/data"},
		{"user@host:/abs/.chb/", "data", "user@host:/abs/.chb/data"},
		{"/local/.chb", "settings", "/local/.chb/settings"},
		{"/local/.chb/", "nostr/outbox", "/local/.chb/nostr/outbox"},
		{"/local/.chb", "", "/local/.chb/"},
	}
	for _, c := range cases {
		got := mirrorRemoteSubpath(c.base, c.sub)
		if got != c.want {
			t.Errorf("mirrorRemoteSubpath(%q, %q) = %q, want %q", c.base, c.sub, got, c.want)
		}
	}
}

func sliceContains(s []string, item string) bool {
	for _, v := range s {
		if v == item {
			return true
		}
	}
	return false
}

// TestMirrorPullEndToEndLocal exercises the real rsync binary against a
// local-path CHB_SYNC_SOURCE. This is the closest we can get to a real
// remote test inside the unit-test harness — `rsync /src/ /dest/` is the
// same code path that runs against `user@host:/path`, just without the
// network hop. Skipped if rsync isn't on PATH (e.g. in a slim CI image).
func TestMirrorPullEndToEndLocal(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH; skipping end-to-end mirror test")
	}
	srcDir := filepath.Join(t.TempDir(), "src")
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	t.Setenv("CHB_SYNC_SOURCE", srcDir)

	// Trusted-host layout: a single provider raw archive, one
	// latest/ generated file, and an empty outbox/sent so the
	// bidirectional rsync legs have a remote dir to talk to.
	mustWrite(t, filepath.Join(srcDir, "data/2026/05/providers/stripe/balance.json"), `{"hello":"world"}`)
	mustWrite(t, filepath.Join(srcDir, "data/latest/generated/summary.txt"), "summary text")
	if err := os.MkdirAll(filepath.Join(srcDir, "nostr/outbox"), 0755); err != nil {
		t.Fatalf("mkdir outbox: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "nostr/sent"), 0755); err != nil {
		t.Fatalf("mkdir sent: %v", err)
	}

	if err := MirrorPull(nil); err != nil {
		t.Fatalf("MirrorPull: %v", err)
	}

	// data/ should be a faithful copy.
	if got, err := os.ReadFile(filepath.Join(appDir, "data/2026/05/providers/stripe/balance.json")); err != nil {
		t.Fatalf("read mirrored balance.json: %v", err)
	} else if strings.TrimSpace(string(got)) != `{"hello":"world"}` {
		t.Fatalf("mirrored balance.json = %q", string(got))
	}
	if got, err := os.ReadFile(filepath.Join(appDir, "data/latest/generated/summary.txt")); err != nil {
		t.Fatalf("read mirrored summary.txt: %v", err)
	} else if strings.TrimSpace(string(got)) != "summary text" {
		t.Fatalf("mirrored summary.txt = %q", string(got))
	}
}

// TestMirrorOutboxBidirectionalRoundtrip drives the real rsync against a
// local-path source and verifies that:
//
//  1. A teammate event sitting on the trusted host's outbox lands in our
//     local outbox after a pull (so we eventually publish it ourselves).
//  2. A locally-queued event lands on the trusted host's outbox (so the
//     trusted host's next `chb nostr push` flushes it).
//
// Both legs use --update, so the side with the newer mtime wins. We rely
// on touch order (default file mtime = creation time) to make each side
// "newer" for its own contribution.
func TestMirrorOutboxBidirectionalRoundtrip(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not on PATH")
	}
	srcDir := filepath.Join(t.TempDir(), "src")
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	t.Setenv("CHB_SYNC_SOURCE", srcDir)

	// Remote (trusted host) has one event waiting in the outbox.
	mustWrite(t, filepath.Join(srcDir, "nostr/outbox/remote-event.json"), `{"id":"remote"}`)
	// Local has its own queued event.
	mustWrite(t, filepath.Join(appDir, "nostr/outbox/local-event.json"), `{"id":"local"}`)
	// Provide an empty sent dir on the remote so its rsync legs don't
	// emit warnings (the warnings would still be non-fatal, but the
	// test signal stays clean).
	if err := os.MkdirAll(filepath.Join(srcDir, "nostr/sent"), 0755); err != nil {
		t.Fatalf("mkdir sent: %v", err)
	}
	mustWrite(t, filepath.Join(srcDir, "data/2026/05/providers/stripe/balance.json"), `{}`)

	if err := MirrorPull(nil); err != nil {
		t.Fatalf("MirrorPull: %v", err)
	}

	// Remote event should now exist locally (we learned about it on the
	// down-leg).
	if _, err := os.Stat(filepath.Join(appDir, "nostr/outbox/remote-event.json")); err != nil {
		t.Fatalf("remote event did not arrive locally: %v", err)
	}
	// Local event should now exist on the trusted host (we pushed it
	// on the up-leg).
	if _, err := os.Stat(filepath.Join(srcDir, "nostr/outbox/local-event.json")); err != nil {
		t.Fatalf("local event did not arrive on the trusted host: %v", err)
	}
}

// TestReconcileMirroredSettingsCoversFourCases drives the per-file decision
// tree directly. Four staged files representing each branch:
//
//  1. missing locally       → installed (no tracker entry yet)
//  2. identical to mirror   → no-op
//  3. tracked-hash matches  → auto-update (user hasn't edited)
//  4. tracked-hash differs  → pending update (user edited)
func TestReconcileMirroredSettingsCoversFourCases(t *testing.T) {
	appDir := t.TempDir()
	t.Setenv("APP_DATA_DIR", appDir)
	settingsDir := filepath.Join(appDir, "settings")
	stagingDir := filepath.Join(appDir, ".mirror-settings")
	for _, d := range []string{settingsDir, stagingDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Case 1: missing local.
	mustWrite(t, filepath.Join(stagingDir, "case1-new.json"), `{"k":"new"}`)

	// Case 2: identical local + mirror, tracker missing (will be added).
	mustWrite(t, filepath.Join(stagingDir, "case2-same.json"), `{"k":"same"}`)
	mustWrite(t, filepath.Join(settingsDir, "case2-same.json"), `{"k":"same"}`)

	// Case 3: tracker matches local → user hasn't edited → auto-update.
	mustWrite(t, filepath.Join(stagingDir, "case3-updatable.json"), `{"k":"new-upstream"}`)
	mustWrite(t, filepath.Join(settingsDir, "case3-updatable.json"), `{"k":"old-tracked"}`)

	// Case 4: tracker differs from local → user edited → pending.
	mustWrite(t, filepath.Join(stagingDir, "case4-edited.json"), `{"k":"new-upstream"}`)
	mustWrite(t, filepath.Join(settingsDir, "case4-edited.json"), `{"k":"user-edits"}`)

	// Seed tracker: case3 records its current local hash (so auto-update
	// is allowed); case4 records a hash that doesn't match the user's
	// edits (so it's surfaced as pending).
	tracker := map[string]string{
		"case3-updatable.json": sha256Hex([]byte(`{"k":"old-tracked"}`)),
		"case4-edited.json":    sha256Hex([]byte(`{"k":"original-shipped"}`)),
	}
	if err := saveInstalledDefaultsRecord(settingsDir, tracker); err != nil {
		t.Fatalf("save tracker: %v", err)
	}

	// Reset the package-level pending slice; the reconciler treats it
	// as the in-process state to extend.
	pendingDefaultUpdates = nil
	reconcileMirroredSettings(stagingDir, settingsDir)

	// Case 1: installed.
	if got, err := os.ReadFile(filepath.Join(settingsDir, "case1-new.json")); err != nil || strings.TrimSpace(string(got)) != `{"k":"new"}` {
		t.Fatalf("case1: got %q, err %v", string(got), err)
	}
	// Case 2: unchanged on disk.
	if got, _ := os.ReadFile(filepath.Join(settingsDir, "case2-same.json")); strings.TrimSpace(string(got)) != `{"k":"same"}` {
		t.Fatalf("case2 changed unexpectedly: %q", string(got))
	}
	// Case 3: auto-updated to the new upstream.
	if got, _ := os.ReadFile(filepath.Join(settingsDir, "case3-updatable.json")); strings.TrimSpace(string(got)) != `{"k":"new-upstream"}` {
		t.Fatalf("case3 should auto-update, got %q", string(got))
	}
	// Case 4: local untouched + pending update added.
	if got, _ := os.ReadFile(filepath.Join(settingsDir, "case4-edited.json")); strings.TrimSpace(string(got)) != `{"k":"user-edits"}` {
		t.Fatalf("case4 must not be overwritten, got %q", string(got))
	}
	pending := PendingSettingsUpdates()
	if len(pending) != 1 || pending[0].Name != "case4-edited.json" {
		t.Fatalf("pending updates = %v, want exactly case4", pending)
	}
	if strings.TrimSpace(string(pending[0].LocalContent)) != `{"k":"user-edits"}` {
		t.Fatalf("pending local = %q", string(pending[0].LocalContent))
	}
	if strings.TrimSpace(string(pending[0].UpstreamBytes)) != `{"k":"new-upstream"}` {
		t.Fatalf("pending upstream = %q", string(pending[0].UpstreamBytes))
	}

	// Tracker should now show case1/2/3 at the new hash.
	finalTracker := loadInstalledDefaultsRecord(settingsDir)
	if finalTracker["case1-new.json"] != sha256Hex([]byte(`{"k":"new"}`)) {
		t.Fatalf("tracker case1 = %q", finalTracker["case1-new.json"])
	}
	if finalTracker["case2-same.json"] != sha256Hex([]byte(`{"k":"same"}`)) {
		t.Fatalf("tracker case2 = %q", finalTracker["case2-same.json"])
	}
	if finalTracker["case3-updatable.json"] != sha256Hex([]byte(`{"k":"new-upstream"}`)) {
		t.Fatalf("tracker case3 = %q", finalTracker["case3-updatable.json"])
	}
}

// TestPrintMirrorModeHelpBannerOnlyWhenEnabled verifies the help banner
// only renders when CHB_SYNC_SOURCE is set — operators on the trusted
// host see vanilla help, thin clients see the mirror-mode summary first.
func TestPrintMirrorModeHelpBannerOnlyWhenEnabled(t *testing.T) {
	t.Setenv("CHB_SYNC_SOURCE", "")
	out := captureStdout(t, printMirrorModeHelpBanner)
	if out != "" {
		t.Fatalf("expected no output when CHB_SYNC_SOURCE unset, got %q", out)
	}
	t.Setenv("CHB_SYNC_SOURCE", "ops@host:/srv/chb")
	out = captureStdout(t, printMirrorModeHelpBanner)
	if !strings.Contains(out, "MIRROR MODE") {
		t.Fatalf("banner missing MIRROR MODE heading: %q", out)
	}
	if !strings.Contains(out, "ops@host:/srv/chb") {
		t.Fatalf("banner missing source URL: %q", out)
	}
	if !strings.Contains(out, "--no-mirror") {
		t.Fatalf("banner missing --no-mirror hint: %q", out)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
