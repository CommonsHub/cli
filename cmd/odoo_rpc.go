package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	odoosource "github.com/CommonsHub/chb/providers/odoo"
)

// wrapOdooAuthError relabels the wrapped error so a transport-layer
// rate-limit doesn't get reported as an auth failure (which used to send
// the operator chasing wrong-credentials ghosts). The underlying odoosource
// already detects HTTP 429 and surfaces "rate-limited by Odoo"; we just
// avoid prepending "Odoo authentication failed:" in that case.
func wrapOdooAuthError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "rate-limited by Odoo") {
		return err
	}
	return fmt.Errorf("Odoo authentication failed: %v", err)
}

func odooDBFromURL(odooURL string) string {
	return odoosource.DBFromURL(odooURL)
}

// OdooDBFromURL is the exported alias used by main.go's global-flag handler
// to derive the DB slug from a URL when only one of the two was specified.
func OdooDBFromURL(odooURL string) string {
	return odoosource.DBFromURL(odooURL)
}

func odooAuth(odooURL, db, login, password string) (int, error) {
	return odoosource.Auth(odooURL, db, login, password)
}

func odooExec(odooURL, db string, uid int, password, model, method string, args []interface{}, kwargs map[string]interface{}) (json.RawMessage, error) {
	return odoosource.Exec(odooURL, db, uid, password, model, method, args, kwargs)
}
