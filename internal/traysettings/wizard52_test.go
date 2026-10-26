package traysettings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_52_BrowseModes: default Find = empty mode; subnet only
// explicit; capabilities gate UI helpers; advice and progress text.
func TestCHK_TRAY_52_BrowseModes(t *testing.T) {
	z := NewWizard(i18n.RU)
	if r := z.BrowseRequest(); r.Cmd != model.CmdSQLBrowse || r.Mode != "" || r.Host != "" {
		t.Fatalf("default browse %+v", r)
	}
	z.Server = "sql01"
	z.Port = "1433"
	r := z.BrowseRequestMode(model.SQLBrowseSubnet)
	if r.Mode != model.SQLBrowseSubnet || r.Host != "sql01" || r.Port != 1433 {
		t.Fatalf("subnet %+v", r)
	}
	caps := map[string]bool{model.CapBrowseSubnet: true, model.CapTestSteps: true}
	has := func(c string) bool { return caps[c] }
	if !HasBrowseSubnet(has) || HasBrowseLocal(has) || !HasTestSteps(has) {
		t.Fatal("caps")
	}
	res := model.SQLBrowseResult{Instances: []model.SQLServerInstance{{Host: "H", Instance: "I"}}, Responders: 1,
		Advice: []string{model.MsgSQLBrowseAdviceEnterServer, MsgBrowsePublicNetwork}}
	txt := z.BrowseDone(res)
	if len(z.Instances) != 1 {
		t.Fatalf("instances %d", len(z.Instances))
	}
	if !strings.Contains(txt, i18n.T(i18n.KeyWizPublicNetwork, i18n.RU)) {
		t.Fatalf("public hint missing: %q", txt)
	}
	if !strings.Contains(txt, model.SQLMessage(model.MsgSQLBrowseAdviceEnterServer, i18n.RU)) {
		t.Fatalf("advice missing: %q", txt)
	}
	p := z.BrowseProgressLine(model.SQLBrowseProgress{Phase: "ssrp", Done: 1, Total: 3, Found: 2})
	if !strings.Contains(p, "ssrp") || !strings.Contains(p, "2") {
		t.Fatalf("progress %q", p)
	}
}

// TestCHK_TRAY_52_TestSteps: request has no password; step lines and advice.
func TestCHK_TRAY_52_TestSteps(t *testing.T) {
	z := NewWizard(i18n.EN)
	z.Server = `srv\INST`
	z.Port = "1500"
	req := z.TestStepsRequest("")
	if req.Cmd != model.CmdSQLTestSteps || req.Server != `srv\INST,1500` || req.Password != "" || req.ProfileID != "" {
		t.Fatalf("req %+v", req)
	}
	steps := []model.SQLCheckStep{
		{ID: model.SQLStepParse, OK: true, Ms: 1},
		{ID: model.SQLStepDNS, OK: false, Ms: 5, AdviceKey: model.MsgSQLCheckAdviceDNS, Detail: "nxdomain"},
	}
	line := z.TestStepLine(steps[1])
	if !strings.Contains(line, "✗") || !strings.Contains(line, "DNS") || !strings.Contains(line, "nxdomain") {
		t.Fatalf("line %q", line)
	}
	adv := z.TestStepsAdvice(steps)
	if !strings.Contains(adv, model.SQLMessage(model.MsgSQLCheckAdviceDNS, i18n.EN)) {
		t.Fatalf("advice %q", adv)
	}
	if RetryAfterSeconds(2500) != 3 || RetryAfterSeconds(0) != 1 {
		t.Fatal("retry")
	}
	e := NewEditor(i18n.RU, Admin)
	pr := e.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeBusy, RetryAfterMs: 4500}, nil)
	if pr == nil || !strings.Contains(pr.Text, "5") {
		t.Fatalf("busy retry %v", pr)
	}
}

// TestCHK_TRAY_52_ProgressJSON: flat pushes decode (id is a string for steps).
func TestCHK_TRAY_52_ProgressJSON(t *testing.T) {
	var m model.IPCMessage
	if err := json.Unmarshal([]byte(`{"type":"sql_browse_progress","ref":7,"phase":"subnet","done":10,"total":254,"found":1}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != model.MsgSQLBrowseProgress || m.Ref != 7 || m.BrowseProgress == nil || m.BrowseProgress.Phase != "subnet" || m.BrowseProgress.Done != 10 {
		t.Fatalf("%+v %+v", m, m.BrowseProgress)
	}
	m = model.IPCMessage{}
	if err := json.Unmarshal([]byte(`{"type":"sql_test_steps_progress","ref":3,"id":"dns","ok":false,"ms":12,"adviceKey":"sql.check_advice_dns"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != model.MsgSQLTestStepsProgress || m.Ref != 3 || m.TestStep == nil || m.TestStep.ID != "dns" || m.TestStep.OK {
		t.Fatalf("%+v %+v", m, m.TestStep)
	}
	// ordinary message still works
	m = model.IPCMessage{}
	if err := json.Unmarshal([]byte(`{"type":"ok","id":1}`), &m); err != nil || m.Type != model.MsgOK || m.ID != 1 {
		t.Fatalf("ok %+v %v", m, err)
	}
}
