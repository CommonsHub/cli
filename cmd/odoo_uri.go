package cmd

import (
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"
)

// OdooRef points at a specific Odoo record. It is the parsed form of an
// `odoo:<host>:<db>:<model>:<id>` NIP-73 URI.
type OdooRef struct {
	Host  string
	DB    string
	Model string
	ID    int
}

// OdooURI renders a NIP-73 identifier for an Odoo record. Use it for both
// Nostr `I` tags and local caches. The scheme matches the other CHB URIs
// (`stripe:`, `ethereum:`) in shape.
//
//	odoo:<host>:<db>:<model>:<id>
func OdooURI(host, db, model string, id int) string {
	return fmt.Sprintf("odoo:%s:%s:%s:%d", host, db, model, id)
}

// ParseOdooURI reverses OdooURI. Returns an error when the string doesn't
// match the scheme or the id isn't an integer.
func ParseOdooURI(uri string) (OdooRef, error) {
	parts := strings.SplitN(uri, ":", 5)
	if len(parts) != 5 || parts[0] != "odoo" {
		return OdooRef{}, fmt.Errorf("not an odoo URI: %q", uri)
	}
	id, err := strconv.Atoi(parts[4])
	if err != nil {
		return OdooRef{}, fmt.Errorf("invalid id %q in %q", parts[4], uri)
	}
	return OdooRef{Host: parts[1], DB: parts[2], Model: parts[3], ID: id}, nil
}

// OdooHost returns the bare hostname of an Odoo base URL — scheme and path
// stripped. Used to build NIP-73 URIs that don't include `https://`.
func OdooHost(rawURL string) string {
	u, err := neturl.Parse(rawURL)
	if err != nil || u.Host == "" {
		// Fall back: assume the caller passed a bare host already.
		return strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	}
	return u.Hostname()
}

// OdooWebURL returns a human-clickable link to the Odoo record, for logs and
// TUI prompts. Example:
//
//	https://citizenspring.odoo.com/web#id=42&model=account.move
func OdooWebURL(baseURL, model string, id int) string {
	return fmt.Sprintf("%s/web#id=%d&model=%s", strings.TrimRight(baseURL, "/"), id, model)
}
