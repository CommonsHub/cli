package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MirrorSource returns the value of the CHB_SYNC_SOURCE environment variable.
//
// Setting CHB_SYNC_SOURCE switches `chb` into a thin-client (mirror) mode:
// instead of running every provider locally (which needs API keys per
// teammate), the binary rsyncs from a trusted host that already did the work.
//
// Value format: `user@host:/abs/path/to/.chb` — the same syntax that ssh and
// rsync accept. Plain local paths (`/tmp/some/.chb`) work too, which keeps
// the end-to-end test loop cheap.
//
// Behaviour matrix (only applies when CHB_SYNC_SOURCE is set):
//
//   - `chb pull`      → rsync from remote (data + settings + outbox)
//                       instead of running every provider locally.
//   - `chb generate`  → no-op; the trusted host already generated.
//   - `chb push`      → only push Nostr (if a local NOSTR_SECRET_KEY exists).
//                       Refuse Odoo push: that requires credentials only the
//                       trusted host has.
//   - `chb sync`      → mirror-pull, then flush local Nostr if keys are
//                       present. Odoo writes refuse.
//   - read-only       → unchanged.
//     commands
//
// Per-invocation override: passing `--no-mirror` on `pull` / `sync` / `push`
// ignores CHB_SYNC_SOURCE. Useful for the trusted host running the same
// binary.
//
// SSH auth is delegated entirely to the user's ssh-agent / keys; chb does
// not manage credentials.
//
// Lock: a flock-protected lock file at $APP_DATA_DIR/.sync.lock is held for
// the entire rsync sequence so two concurrent mirror pulls can't corrupt the
// local copy.
//
// See docs/mirror-mode.md for the architecture rationale.
func MirrorSource() string {
	return strings.TrimSpace(os.Getenv("CHB_SYNC_SOURCE"))
}

// MirrorEnabled reports whether mirror mode is active for this invocation.
// It returns true only when CHB_SYNC_SOURCE is set AND --no-mirror is not
// present in args.
func MirrorEnabled(args []string) bool {
	if MirrorSource() == "" {
		return false
	}
	if HasFlag(args, "--no-mirror") {
		return false
	}
	return true
}

// FilterMirrorFlags strips mirror-mode-only flags from args before they
// reach sub-commands. Mirror flags are handled at the dispatcher level.
func FilterMirrorFlags(args []string) []string {
	return filterFlag(args, "--no-mirror")
}

// RequireOdooWriteCapability fails fast when mirror mode is active and the
// local environment doesn't have an Odoo password. The trusted host has the
// credentials; thin clients should refuse to attempt the write so the operator
// gets a clear "run this on the trusted host" message instead of a cryptic
// auth failure.
//
// Safe to call from every Odoo-write entry point (push, reconcile, journal
// sync, …); returns nil when not in mirror mode or when credentials are
// available, so existing single-host setups stay unchanged.
func RequireOdooWriteCapability() error {
	if MirrorSource() == "" {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ODOO_PASSWORD")) != "" {
		return nil
	}
	return fmt.Errorf("Odoo writes are disabled in mirror mode (CHB_SYNC_SOURCE is set and ODOO_PASSWORD is unset).\n  ↪ Run this command on the trusted host, or unset CHB_SYNC_SOURCE locally if you have credentials")
}

// mirrorRunner is the shell-out indirection that lets tests stub rsync.
// Production code uses runRsyncDefault.
type mirrorRunner func(args []string, label string) error

var mirrorRunRsync mirrorRunner = runRsyncDefault

// runRsyncDefault execs the system rsync binary with the given args, streams
// stdout/stderr line-by-line (dim for stdout, yellow ⚠ prefix for stderr),
// and returns a non-nil error if the rsync exit code is non-zero. The label
// is used in the per-line prefix so the operator can tell which rsync
// invocation is talking.
func runRsyncDefault(args []string, label string) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return fmt.Errorf("rsync is required for mirror mode but was not found on PATH — install rsync (e.g. `sudo apt install rsync`) and retry")
	}
	cmd := exec.Command("rsync", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("rsync stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("rsync stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rsync start: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go streamRsyncLines(&wg, stdout, label, false)
	go streamRsyncLines(&wg, stderr, label, true)
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rsync %s failed: %w", label, err)
	}
	return nil
}

func streamRsyncLines(wg *sync.WaitGroup, r io.Reader, label string, isErr bool) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if isErr {
			fmt.Fprintf(os.Stderr, "  %s⚠ %s: %s%s\n", Fmt.Yellow, label, line, Fmt.Reset)
			continue
		}
		fmt.Printf("  %s%s: %s%s\n", Fmt.Dim, label, line, Fmt.Reset)
	}
}

// mirrorLockPath returns the path of the flock used to serialise mirror
// operations. One lock per APP_DATA_DIR is enough — a second concurrent
// `chb pull` waits for the first to finish before starting its own rsync.
func mirrorLockPath() string {
	return filepath.Join(AppDataDir(), ".sync.lock")
}

// withMirrorLock acquires an exclusive flock on the mirror lock file, runs
// fn, and releases the lock. Blocking — concurrent callers wait. The lock
// file is created if missing; we never delete it.
func withMirrorLock(fn func() error) error {
	if err := os.MkdirAll(AppDataDir(), 0755); err != nil {
		return err
	}
	path := mirrorLockPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open mirror lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire mirror lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// mirrorRemoteSubpath joins a sub-path onto the rsync source spec. Handles
// both `user@host:/abs` and plain `/abs` forms. The result is always
// terminated with the source segment caller passed (no implicit slash).
func mirrorRemoteSubpath(base, sub string) string {
	base = strings.TrimRight(base, "/")
	sub = strings.TrimLeft(sub, "/")
	if sub == "" {
		return base + "/"
	}
	return base + "/" + sub
}

// localSubpath joins a sub-path onto AppDataDir.
func localSubpath(sub string) string {
	dir := AppDataDir()
	sub = strings.TrimLeft(sub, "/")
	return filepath.Join(dir, sub)
}

// MirrorPull runs the read-side of mirror mode: it rsyncs the trusted host's
// data/ tree into the local AppDataDir and prints a compact summary. Returns
// nil when CHB_SYNC_SOURCE is unset (a no-op so the caller can invoke it
// unconditionally inside main.go's dispatch).
//
// Phase 3 scope: data/ is mirrored authoritatively (--delete), the nostr
// outbox + sent are synced bidirectionally (--update), and settings/ runs
// through the merge-with-pending-updates reconciler — unedited files
// auto-update, locally-edited files surface as PendingDefaultUpdates so
// `chb settings` shows a diff the operator can accept.
func MirrorPull(args []string) error {
	if !MirrorEnabled(args) {
		return nil
	}
	src := MirrorSource()
	verbose := HasFlag(args, "--verbose", "-v") || HasFlag(args, "--debug")
	started := time.Now()
	fmt.Printf("\n%sMirroring from %s%s\n\n", Fmt.Bold, src, Fmt.Reset)
	if _, err := exec.LookPath("rsync"); err != nil {
		return fmt.Errorf("rsync is required for mirror mode but was not found on PATH — install rsync (e.g. `sudo apt install rsync`) and retry")
	}
	err := withMirrorLock(func() error {
		// data/ — authoritative pull. The trusted host always wins.
		if err := mirrorRsyncData(src, verbose); err != nil {
			return err
		}
		// Outbox: bidirectional (preserve queued local annotations).
		if err := mirrorRsyncOutbox(src, verbose); err != nil {
			return err
		}
		// Settings: merge-with-pending-updates. Per-machine files
		// (secrets, identity, tracker) are excluded.
		return mirrorRsyncSettings(src, verbose)
	})
	elapsed := time.Since(started).Round(100 * time.Millisecond)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s✗ Mirror pull failed after %s: %v%s\n\n", Fmt.Red, FormatElapsedFixed(elapsed), err, Fmt.Reset)
		return err
	}
	fmt.Printf("\n%s✓ Mirrored in %s%s\n\n", Fmt.Green, FormatElapsedFixed(elapsed), Fmt.Reset)
	UpdateSyncSource("pull", false)
	UpdateSyncActivity(false)
	return nil
}

// mirrorRsyncData pulls $remote/data/ → $local/data/ with --delete. The
// trusted host owns the canonical generated state; locally-modified files
// in data/ are blown away on every pull. This is intentional: data/ is a
// pure read-only mirror of provider output.
func mirrorRsyncData(src string, verbose bool) error {
	remote := mirrorRemoteSubpath(src, "data") + "/"
	local := localSubpath("data") + "/"
	if err := os.MkdirAll(local, 0755); err != nil {
		return err
	}
	rsyncArgs := baseRsyncFlags(verbose)
	rsyncArgs = append(rsyncArgs, "--delete", remote, local)
	return mirrorRunRsync(rsyncArgs, "data")
}

// mirrorRsyncOutbox does a bidirectional sync of the nostr outbox:
//  1. Push local outbox up FIRST (preserve teammate's queued annotations).
//  2. Pull remote outbox down to learn about other teammates' pending events.
//
// sent/ follows the same pattern so events that one teammate published are
// visible to the others (the trusted host's `chb nostr push` can then drop
// stale local entries from its own outbox by URI).
//
// rsync --update is used: the side with the newer mtime wins. Combined with
// the "outbox first up, then down" ordering, this means no event ever gets
// silently dropped — at worst we re-push something that's already been
// flushed, which the Nostr relay deduplicates by event ID.
//
// BOTH legs are treated as best-effort: the trusted host may be a read-only
// mount (up fails), or its nostr/ dir may not exist yet on a brand-new
// install (down fails). We warn and continue so the data/ pull — the
// load-bearing half of the mirror — is never blocked by an outbox glitch.
func mirrorRsyncOutbox(src string, verbose bool) error {
	for _, kind := range []string{"outbox", "sent"} {
		localDir := filepath.Join(AppDataDir(), "nostr", kind)
		if err := os.MkdirAll(localDir, 0755); err != nil {
			return err
		}
		remoteDir := mirrorRemoteSubpath(src, "nostr/"+kind) + "/"
		// Up: local → remote.
		upArgs := baseRsyncFlags(verbose)
		upArgs = append(upArgs, "--update", localDir+"/", remoteDir)
		if err := mirrorRunRsync(upArgs, "nostr/"+kind+" up"); err != nil {
			Warnf("%s⚠ mirror %s up: %v (continuing)%s", Fmt.Yellow, kind, err, Fmt.Reset)
		}
		// Down: remote → local.
		downArgs := baseRsyncFlags(verbose)
		downArgs = append(downArgs, "--update", remoteDir, localDir+"/")
		if err := mirrorRunRsync(downArgs, "nostr/"+kind+" down"); err != nil {
			Warnf("%s⚠ mirror %s down: %v (continuing)%s", Fmt.Yellow, kind, err, Fmt.Reset)
		}
	}
	return nil
}

// mirrorRsyncSettings pulls $remote/settings/ into a staging dir under
// $APP_DATA_DIR/.mirror-settings/, then runs reconcileMirroredSettings to
// merge unedited locals (auto-update) and surface edited locals as pending
// updates. Per-machine files are excluded from the rsync entirely:
//
//   - config.env             — secrets (API keys); each machine owns its own.
//   - nostr.json             — local Nostr identity.
//   - .nostr-keys.json       — legacy Nostr identity path.
//   - .installed-defaults.json — tracker for the embedded-defaults
//     bootstrap; reused by the mirror reconciler but never sent over
//     the wire.
//
// rsync errors are surfaced as a warning (best-effort, like the outbox)
// so a transient settings rsync failure can't block the data/ pull.
func mirrorRsyncSettings(src string, verbose bool) error {
	localSettings := AppSettingsDir()
	if err := os.MkdirAll(localSettings, 0755); err != nil {
		return err
	}
	stagingDir := filepath.Join(AppDataDir(), ".mirror-settings")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return err
	}
	remoteSettings := mirrorRemoteSubpath(src, "settings") + "/"
	rsyncArgs := baseRsyncFlags(verbose)
	rsyncArgs = append(rsyncArgs,
		"--exclude=config.env",
		"--exclude=nostr.json",
		"--exclude=.nostr-keys.json",
		"--exclude=.installed-defaults.json",
		"--delete-excluded",
	)
	rsyncArgs = append(rsyncArgs, remoteSettings, stagingDir+"/")
	if err := mirrorRunRsync(rsyncArgs, "settings"); err != nil {
		Warnf("%s⚠ mirror settings: %v (continuing)%s", Fmt.Yellow, err, Fmt.Reset)
		return nil
	}
	reconcileMirroredSettings(stagingDir, localSettings)
	return nil
}

// reconcileMirroredSettings walks the staging dir and decides what to do
// with each file. The decision tree mirrors reconcileDefaultSettings in
// cmd/settings.go — the .installed-defaults.json tracker records the last
// content we installed (regardless of whether the upstream was the embedded
// defaults or the mirror), so an unedited local file auto-updates and an
// edited local file is preserved with a pending update for `chb settings`.
//
// Per file:
//
//   - missing locally          → install from mirror, record its hash.
//   - identical to mirror      → no-op (refresh tracker).
//   - tracked-hash matches     → user hasn't edited; auto-update.
//   - tracked-hash differs     → user edited; surface as pending update.
//
// If a file is already pending from the embedded-defaults reconciler, the
// mirror's version wins (the trusted host's settings are the canonical
// upstream for thin clients; the embedded defaults are the fallback).
func reconcileMirroredSettings(stagingDir, localSettingsDir string) {
	tracker := loadInstalledDefaultsRecord(localSettingsDir)
	pending := append([]PendingDefaultUpdate(nil), pendingDefaultUpdates...)
	trackerDirty := false
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			// Settings nesting isn't used today; skip for now.
			continue
		}
		name := entry.Name()
		stagedPath := filepath.Join(stagingDir, name)
		mirrorBytes, err := os.ReadFile(stagedPath)
		if err != nil {
			continue
		}
		mirrorHash := sha256Hex(mirrorBytes)
		target := filepath.Join(localSettingsDir, name)
		localBytes, statErr := os.ReadFile(target)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := writeDefaultFile(target, mirrorBytes); err != nil {
				continue
			}
			tracker[name] = mirrorHash
			trackerDirty = true
			fmt.Printf("  %s✓%s mirrored %s\n", Fmt.Green, Fmt.Reset, name)
			continue
		} else if statErr != nil {
			continue
		}
		localHash := sha256Hex(localBytes)
		if localHash == mirrorHash {
			if tracker[name] != mirrorHash {
				tracker[name] = mirrorHash
				trackerDirty = true
			}
			continue
		}
		trackedHash, hadTracker := tracker[name]
		userEdited := hadTracker && trackedHash != localHash
		if !userEdited {
			if err := writeDefaultFile(target, mirrorBytes); err != nil {
				continue
			}
			tracker[name] = mirrorHash
			trackerDirty = true
			fmt.Printf("  %s↑%s updated %s from mirror\n", Fmt.Green, Fmt.Reset, name)
			continue
		}
		pending = appendOrReplacePending(pending, PendingDefaultUpdate{
			Name:          name,
			LocalContent:  localBytes,
			UpstreamBytes: mirrorBytes,
		})
	}
	if trackerDirty {
		_ = saveInstalledDefaultsRecord(localSettingsDir, tracker)
	}
	pendingDefaultUpdates = pending
}

func appendOrReplacePending(list []PendingDefaultUpdate, item PendingDefaultUpdate) []PendingDefaultUpdate {
	for i, existing := range list {
		if existing.Name == item.Name {
			list[i] = item
			return list
		}
	}
	return append(list, item)
}

// printMirrorModeHelpBanner prints a short note about mirror mode at the
// top of `chb pull --help` and `chb sync --help` when CHB_SYNC_SOURCE is
// set. Operators running on the trusted host see normal help; thin clients
// see "you're in mirror mode, here's what that means for this command".
func printMirrorModeHelpBanner() {
	src := MirrorSource()
	if src == "" {
		return
	}
	fmt.Printf(`
%sMIRROR MODE — CHB_SYNC_SOURCE is set%s
  %sSource:%s %s
  %sBehaviour:%s
    - pull / sync rsync data + nostr outbox from the trusted host
      instead of running every provider locally.
    - generate is a no-op (the trusted host already generated).
    - push only flushes the local Nostr outbox; Odoo writes refuse.
    - %s--no-mirror%s on the same command falls back to the legacy
      provider-driven path (used on the trusted host itself).

`, Fmt.Bold, Fmt.Reset,
		Fmt.Dim, Fmt.Reset, src,
		Fmt.Dim, Fmt.Reset,
		Fmt.Yellow, Fmt.Reset,
	)
}

// PrintMirrorGenerateSkipped prints the one-liner shown when `chb generate`
// is invoked in mirror mode. The trusted host already generated everything;
// running generate locally would only fight that state.
func PrintMirrorGenerateSkipped() {
	fmt.Printf("\n  %s↳ skipped: CHB_SYNC_SOURCE is set; remote already generated%s\n\n",
		Fmt.Dim, Fmt.Reset)
}

// MirrorPushNostrOnly is the push-side of mirror mode: Odoo is refused
// (the trusted host owns it), but local Nostr annotations are signed and
// pushed up so other teammates' relays see them too. If no local Nostr
// keys are configured, this is effectively a no-op with a friendly hint.
func MirrorPushNostrOnly(args []string) error {
	if MirrorSource() == "" {
		// Defensive: callers should already check MirrorEnabled, but
		// guard anyway so an accidental direct call doesn't change
		// behaviour for normal users.
		return nil
	}
	verbose := HasFlag(args, "--verbose", "-v") || HasFlag(args, "--debug")
	keys := LoadNostrKeys()
	if keys == nil || strings.TrimSpace(keys.PrivHex) == "" {
		fmt.Printf("\n  %s↳ Odoo push skipped: no credentials on this host (run on the trusted host instead).%s\n", Fmt.Dim, Fmt.Reset)
		fmt.Printf("  %s↳ Nostr push skipped: no local Nostr keys configured (run `chb setup nostr`).%s\n\n", Fmt.Dim, Fmt.Reset)
		return nil
	}
	fmt.Printf("\n  %sMirror push — Nostr only%s\n", Fmt.Bold, Fmt.Reset)
	fmt.Printf("  %s↳ Odoo push skipped: no credentials on this host.%s\n", Fmt.Dim, Fmt.Reset)
	if verbose {
		return NostrPush(args)
	}
	// Compact: silence chatter, just print a one-line summary.
	restore := silenceStdout()
	err := NostrPush(args)
	restore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s✗ Nostr push: %v%s\n", Fmt.Red, err, Fmt.Reset)
		return err
	}
	fmt.Printf("  %s✓ Nostr outbox flushed%s\n\n", Fmt.Green, Fmt.Reset)
	return nil
}

// baseRsyncFlags returns the flag set every mirror rsync uses. `--archive`
// preserves timestamps/permissions; `--safe-links` refuses to follow links
// that point outside the transfer; `--info=progress2` keeps the streamed
// stdout informative without scrolling per-file noise.
func baseRsyncFlags(verbose bool) []string {
	flags := []string{
		"--archive",
		"--safe-links",
		"--partial",
		"--human-readable",
	}
	if verbose {
		flags = append(flags, "--verbose")
	} else {
		flags = append(flags, "--info=stats1,flist0,progress0")
	}
	return flags
}
