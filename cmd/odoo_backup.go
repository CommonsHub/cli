package cmd

import (
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OdooBackup requests a full database backup from Odoo's /web/database/backup
// endpoint and writes the zip (SQL dump + filestore) to
// APP_DATA_DIR/backups/odoo/YYYYMMDD-HHMM.zip.
//
// Odoo's database manager requires the *master* password (admin_passwd from
// odoo.conf), which is distinct from a user's login password. If
// ODOO_MASTER_PASSWORD is unset we fall back to ODOO_PASSWORD on the chance
// they match on a given deployment.
//
// On Odoo.com SaaS the endpoint is usually open; on Odoo.sh it may be disabled.
func OdooBackup(args []string) error {
	if HasFlag(args, "--help", "-h", "help") {
		printOdooBackupHelp()
		return nil
	}

	creds, err := ResolveOdooCredentials()
	if err != nil {
		return err
	}

	if isOdooSaaS(creds.URL) {
		return printSaaSBackupInstructions(creds)
	}

	masterPwd := os.Getenv("ODOO_MASTER_PASSWORD")
	if masterPwd == "" {
		masterPwd = creds.Password
	}
	if masterPwd == "" {
		return fmt.Errorf("no master password available (set ODOO_MASTER_PASSWORD)")
	}

	backupDir := odooBackupDir()
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	stamp := time.Now().In(BrusselsTZ()).Format("20060102-1504")
	dstPath := filepath.Join(backupDir, stamp+".zip")

	fmt.Printf("\n%s💾 Odoo backup%s  %s%s (db: %s)%s\n",
		Fmt.Bold, Fmt.Reset, Fmt.Dim, creds.URL, creds.DB, Fmt.Reset)
	fmt.Printf("  %sDestination: %s%s\n", Fmt.Dim, dstPath, Fmt.Reset)

	endpoint := strings.TrimRight(creds.URL, "/") + "/web/database/backup"
	form := neturl.Values{
		"master_pwd":    {masterPwd},
		"name":          {creds.DB},
		"backup_format": {"zip"},
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Minute}
	fmt.Printf("  %sRequesting dump (this can take a few minutes)...%s\n", Fmt.Dim, Fmt.Reset)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request backup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		hint := ""
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			hint = " (check ODOO_MASTER_PASSWORD)"
		}
		return fmt.Errorf("backup endpoint returned %d%s: %s", resp.StatusCode, hint, strings.TrimSpace(string(body)))
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected Content-Type %q — Odoo likely rejected the request: %s",
			ct, strings.TrimSpace(string(body)))
	}

	tmpPath := dstPath + ".partial"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	written, copyErr := io.Copy(f, resp.Body)
	if cerr := f.Close(); cerr != nil && copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write backup: %w", copyErr)
	}
	if written < 1024 {
		// A valid Odoo backup is always much larger; treat tiny responses as errors.
		body, _ := os.ReadFile(tmpPath)
		os.Remove(tmpPath)
		return fmt.Errorf("backup too small (%d bytes) — likely an error page: %s",
			written, strings.TrimSpace(string(body)))
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename backup: %w", err)
	}

	fmt.Printf("  %s✓ Saved %s (%s)%s\n", Fmt.Green, dstPath, humanBytes(written), Fmt.Reset)
	fmt.Println()
	return nil
}

// isOdooSaaS reports whether the Odoo URL points at the Odoo.com SaaS
// platform, which disables /web/database/backup and forces users through the
// /my/databases UI instead.
func isOdooSaaS(rawURL string) bool {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "odoo.com" || strings.HasSuffix(host, ".odoo.com")
}

// printSaaSBackupInstructions explains why the API backup doesn't work on
// Odoo.com and prints the direct link to the /my/databases page.
func printSaaSBackupInstructions(creds *OdooCredentials) error {
	f := Fmt
	fmt.Printf("\n%s💾 Odoo backup%s  %s%s (db: %s)%s\n",
		f.Bold, f.Reset, f.Dim, creds.URL, creds.DB, f.Reset)
	fmt.Printf("\n  %sOdoo.com SaaS disables the /web/database/backup API.%s\n", f.Yellow, f.Reset)
	fmt.Printf("  %sDownload backups manually from your Odoo.com account:%s\n\n", f.Dim, f.Reset)
	fmt.Printf("    %s%shttps://www.odoo.com/my/databases%s\n\n", f.Bold, f.Cyan, f.Reset)
	fmt.Printf("  %s(Sign in with your odoo.com account, find the %q database, click Backup.)%s\n\n",
		f.Dim, creds.DB, f.Reset)
	return nil
}

func odooBackupDir() string {
	if d := os.Getenv("CHB_BACKUP_DIR"); d != "" {
		return d
	}
	return filepath.Join(AppDataDir(), "backups", "odoo")
}

func humanBytes(n int64) string {
	const (
		_  = iota
		kb = 1 << (10 * iota)
		mb
		gb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func printOdooBackupHelp() {
	f := Fmt
	fmt.Printf(`
%schb odoo backup%s — Download a full database backup from Odoo

%sUSAGE%s
  %schb odoo backup%s

%sDESCRIPTION%s
  Calls Odoo's /web/database/backup endpoint and streams the resulting
  zip (SQL dump + filestore) to:

      APP_DATA_DIR/backups/odoo/YYYYMMDD-HHMM.zip

  Override the destination directory with CHB_BACKUP_DIR.

%sENVIRONMENT%s
  %sODOO_URL%s               Odoo instance URL
  %sODOO_DATABASE%s          Database name (derived from URL if unset)
  %sODOO_MASTER_PASSWORD%s   Admin password from odoo.conf (admin_passwd).
                         Falls back to ODOO_PASSWORD if unset.
  %sAPP_DATA_DIR%s           App config/state directory (default: ~/.chb).
  %sCHB_BACKUP_DIR%s         Override backup destination directory.

%sNOTES%s
  - On Odoo.com SaaS (%s*.odoo.com%s), the database manager is disabled.
    Run this command to see a direct link to the /my/databases page
    where you can trigger a manual backup download.
  - On Odoo.sh, the database manager is usually disabled too; use
    Odoo.sh's own backup tooling.
  - The backup can be large; the HTTP timeout is 30 minutes.
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
	)
}
