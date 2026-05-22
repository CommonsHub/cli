package cmd

import (
	"bufio"
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
//   - `chb push`      → normal push path. Each target gates on its
//                       own credentials, so Odoo refuses on hosts
//                       without ODOO_PASSWORD and Nostr flushes if
//                       a local NOSTR identity exists. Nothing
//                       mirror-specific.
//   - `chb sync`      → mirror-pull + normal push (same credential
//                       gating). Generate is skipped.
//   - read-only       → unchanged.
//     commands
//
// Credentials, not mirror mode, gate writes. A thin-client without
// ODOO_PASSWORD refuses Odoo writes regardless of CHB_SYNC_SOURCE;
// a host with ODOO_PASSWORD writes regardless of mirror mode. The
// simpler rule keeps the mental model small.
//
// SSH auth is delegated entirely to the user's ssh-agent / keys; chb
// does not manage credentials.
//
// Lock: a flock-protected lock file at $APP_DATA_DIR/.sync.lock is
// held for the entire rsync sequence so two concurrent mirror pulls
// can't corrupt the local copy.
//
// See docs/mirror-mode.md for the architecture rationale.
func MirrorSource() string {
	return strings.TrimSpace(os.Getenv("CHB_SYNC_SOURCE"))
}

// MirrorEnabled reports whether mirror mode is active for this
// invocation — i.e. CHB_SYNC_SOURCE is set. The args slice is kept
// in the signature so future per-invocation overrides can hook in
// without changing every call site.
func MirrorEnabled(args []string) bool {
	_ = args
	return MirrorSource() != ""
}

// RequireOdooWriteCapability fails fast when this machine has no
// Odoo credentials. The check is now credential-only — mirror mode
// is irrelevant: a thin-client without ODOO_PASSWORD refuses, and a
// trusted host with ODOO_PASSWORD writes regardless of whether
// CHB_SYNC_SOURCE is set.
//
// The motivation for the simpler semantic: an operator running on a
// thin-client doesn't care WHY the write would fail (mirror mode vs
// missing creds) — only that it would. Treating both as the same
// refusal keeps the mental model small.
//
// Safe to call from every Odoo write entry point (push, reconcile,
// journal sync, …). Returns nil when credentials are present.
func RequireOdooWriteCapability() error {
	if strings.TrimSpace(os.Getenv("ODOO_PASSWORD")) != "" {
		return nil
	}
	if MirrorSource() != "" {
		return fmt.Errorf("Odoo writes are disabled: ODOO_PASSWORD is unset.\n  ↪ This thin-client mirrors from %s. Run the write on the trusted host instead.",
			MirrorSource())
	}
	return fmt.Errorf("Odoo writes are disabled: ODOO_PASSWORD is unset.\n  ↪ Run `chb setup odoo` or add ODOO_PASSWORD to %s/config.env",
		AppSettingsDir())
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

// MirrorPull runs the read-side of mirror mode: rsyncs the trusted
// host's tree into the local AppDataDir and prints a compact summary.
// Returns nil when CHB_SYNC_SOURCE is unset (a no-op so the caller can
// invoke it unconditionally inside main.go's dispatch).
//
// Three rsync legs run under the same flock:
//
//   - data/latest/         → --delete (authoritative snapshot)
//   - data/<YYYY>/<MM>/    → NO --delete (immutable historical archive;
//                            past months never get blown away even if
//                            the source has them gone)
//   - settings/            → --delete (master is source of truth);
//                            config.env + keys/ + .installed_defaults
//                            stay local
//   - nostr/outbox/ + sent → bidirectional --update (queued
//                            annotations from any teammate get
//                            picked up by the next push)
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
		if err := mirrorRsyncDataLatest(src, verbose); err != nil {
			return err
		}
		if err := mirrorRsyncDataMonths(src, verbose); err != nil {
			return err
		}
		if err := mirrorRsyncOutbox(src, verbose); err != nil {
			return err
		}
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

// mirrorRsyncDataLatest pulls data/latest/ with --delete. The latest
// snapshot is authoritative — files missing on the source should
// disappear locally too. Hard-fails: if the trusted host's
// data/latest is missing or unreachable, the operator should know
// rather than silently end up with stale data.
func mirrorRsyncDataLatest(src string, verbose bool) error {
	remote := mirrorRemoteSubpath(src, "data/latest") + "/"
	local := filepath.Join(localSubpath("data"), "latest") + "/"
	if err := os.MkdirAll(local, 0755); err != nil {
		return err
	}
	rsyncArgs := baseRsyncFlags(verbose)
	rsyncArgs = append(rsyncArgs, "--delete", remote, local)
	return mirrorRunRsync(rsyncArgs, "data/latest")
}

// mirrorRsyncDataMonths pulls every dated month under data/<YYYY>/<MM>/
// WITHOUT --delete. Past months are immutable history — if the
// trusted host's backup is partial or a teammate has months we
// don't, we keep both. `--exclude=/latest` keeps the latest/ tree
// out of this leg; it has its own dedicated rsync above.
func mirrorRsyncDataMonths(src string, verbose bool) error {
	remote := mirrorRemoteSubpath(src, "data") + "/"
	local := localSubpath("data") + "/"
	if err := os.MkdirAll(local, 0755); err != nil {
		return err
	}
	rsyncArgs := baseRsyncFlags(verbose)
	// No --delete on this leg. Past months are immutable history.
	rsyncArgs = append(rsyncArgs, "--exclude=/latest", remote, local)
	return mirrorRunRsync(rsyncArgs, "data/months")
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

// mirrorRsyncSettings pulls $remote/settings/ into $local/settings/
// with --delete. The trusted host is the source of truth for
// rules.json / accounts.json / categories / collectives / etc.
// Per-machine files stay local — never overwritten, never sent:
//
//   - config.env         — API keys + CHB_SYNC_SOURCE itself; per-host.
//   - keys/              — Nostr identity (and any other secret material).
//                          Lives outside settings/, but excluded here as
//                          belt-and-suspenders in case the trusted host
//                          ever lays it differently.
//   - .installed_defaults — embedded-defaults bootstrap tracker; per-host.
//
// rsync errors are surfaced as a warning and the function returns
// nil so a transient settings rsync failure (e.g. SSH hiccup mid-
// transfer) can't block the data/ pull, which is the load-bearing
// half of the mirror.
func mirrorRsyncSettings(src string, verbose bool) error {
	localSettings := AppSettingsDir()
	if err := os.MkdirAll(localSettings, 0755); err != nil {
		return err
	}
	remoteSettings := mirrorRemoteSubpath(src, "settings") + "/"
	rsyncArgs := baseRsyncFlags(verbose)
	rsyncArgs = append(rsyncArgs,
		"--delete",
		"--exclude=config.env",
		"--exclude=keys",
		"--exclude=.installed_defaults",
		"--exclude=.installed_defaults.json",
		remoteSettings, localSettings+"/",
	)
	if err := mirrorRunRsync(rsyncArgs, "settings"); err != nil {
		Warnf("%s⚠ mirror settings: %v (continuing)%s", Fmt.Yellow, err, Fmt.Reset)
	}
	return nil
}

// printMirrorModeHelpBanner prints a short note about mirror mode
// at the top of `chb pull --help` and `chb sync --help` when
// CHB_SYNC_SOURCE is set.
func printMirrorModeHelpBanner() {
	src := MirrorSource()
	if src == "" {
		return
	}
	fmt.Printf(`
%sMIRROR MODE — CHB_SYNC_SOURCE is set%s
  %sSource:%s %s
  %sBehaviour:%s
    - pull / sync rsync data + settings + nostr outbox from the trusted
      host instead of running every provider locally.
    - generate is a no-op (the trusted host already generated).
    - push goes through the normal path; each target refuses if its
      credentials are missing on this machine.

`, Fmt.Bold, Fmt.Reset,
		Fmt.Dim, Fmt.Reset, src,
		Fmt.Dim, Fmt.Reset,
	)
}

// PrintMirrorGenerateSkipped prints the one-liner shown when `chb generate`
// is invoked in mirror mode. The trusted host already generated everything;
// running generate locally would only fight that state.
func PrintMirrorGenerateSkipped() {
	fmt.Printf("\n  %s↳ skipped: CHB_SYNC_SOURCE is set; remote already generated%s\n\n",
		Fmt.Dim, Fmt.Reset)
}

// (MirrorPushNostrOnly was removed: PushAllTargets gates each
// target on its own credentials, so the mirror dispatcher just
// calls into the normal push path. Odoo refuses if creds are
// missing, Nostr flushes if local keys exist — no special path
// needed.)

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
