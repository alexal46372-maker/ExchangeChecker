package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCHK_CFG_09_EmailSettingsJSON(t *testing.T) {
	e := DefaultEmailSettings()
	e.Enabled = true
	e.Host, e.Port, e.TLSMode, e.From = "smtp.example.com", 587, EmailTLSStartTLS, "bot@example.com"
	e.Recipients = []EmailRecipient{{ID: "r1", Addr: "a@b.co", Name: "Ann"}}
	e.HasPassword = true
	e.Status = &ViberStatus{Sent: 1}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"hasPassword":true`) || !strings.Contains(s, `"addr":"a@b.co"`) {
		t.Fatalf("view json %s", s)
	}

	ex := e.ForExport()
	xb, _ := json.Marshal(ex)
	xs := string(xb)
	if strings.Contains(xs, "hasPassword") || strings.Contains(xs, "status") || strings.Contains(xs, "smtpPassword") {
		t.Fatalf("export must omit secrets: %s", xs)
	}
	if !strings.Contains(xs, `"a@b.co"`) {
		t.Fatalf("export recipients: %s", xs)
	}

	bad := DefaultEmailSettings()
	bad.Enabled = true
	if err := bad.Validate(); err == nil {
		t.Fatal("enabled without host")
	}
	bad.Host, bad.Port, bad.From = "h", 25, "x@y.z"
	bad.TLSMode = EmailTLSNone
	bad.Recipients = []EmailRecipient{{ID: "r1", Addr: "not-an-email", Name: ""}}
	if fe := AsFieldError(bad.Validate()); fe == nil || fe.Field != "email.recipients[0].addr" {
		t.Fatalf("bad addr: %v", bad.Validate())
	}
}
