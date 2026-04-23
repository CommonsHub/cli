package cmd

import "testing"

func TestOdooURIRoundTrip(t *testing.T) {
	uri := OdooURI("citizenspring.odoo.com", "citizenspring-test", "account.move", 42)
	want := "odoo:citizenspring.odoo.com:citizenspring-test:account.move:42"
	if uri != want {
		t.Fatalf("OdooURI = %q; want %q", uri, want)
	}
	ref, err := ParseOdooURI(uri)
	if err != nil {
		t.Fatalf("ParseOdooURI: %v", err)
	}
	if ref.Host != "citizenspring.odoo.com" || ref.DB != "citizenspring-test" ||
		ref.Model != "account.move" || ref.ID != 42 {
		t.Fatalf("round-trip lost data: %+v", ref)
	}
}

func TestParseOdooURIRejectsGarbage(t *testing.T) {
	bad := []string{
		"",
		"odoo:",
		"odoo:host:db:model",
		"stripe:txn:abc",
		"odoo:host:db:account.move:notanint",
	}
	for _, s := range bad {
		if _, err := ParseOdooURI(s); err == nil {
			t.Errorf("ParseOdooURI(%q) unexpectedly succeeded", s)
		}
	}
}

func TestOdooHost(t *testing.T) {
	cases := map[string]string{
		"https://citizenspring.odoo.com":       "citizenspring.odoo.com",
		"https://citizenspring.odoo.com/":      "citizenspring.odoo.com",
		"http://localhost:8069":                "localhost",
		"citizenspring-test.odoo.com":          "citizenspring-test.odoo.com",
		"https://erp.example.com/odoo/path":    "erp.example.com",
	}
	for in, want := range cases {
		if got := OdooHost(in); got != want {
			t.Errorf("OdooHost(%q) = %q; want %q", in, got, want)
		}
	}
}
