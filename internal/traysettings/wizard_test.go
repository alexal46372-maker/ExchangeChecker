package traysettings

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func newWizEditor() *Editor {
	e := NewEditor(i18n.RU, Admin)
	e.LoadConfig(model.ConfigView{Settings: model.DefaultSettings()}, "r1")
	e.New()
	return e
}

// TestCHK_TRAY_30_WizardSteps — CHK-TRAY-30: availability, server step
// checks (ValidateSQLServerAddr reasons, login), the requests built from
// the wizard input, browse / database lists, autoPick decision.
func TestCHK_TRAY_30_WizardSteps(t *testing.T) {
	all := func(string) bool { return true }
	if !WizardAvailable(Admin, all) || WizardAvailable(ReadOnly, all) ||
		WizardAvailable(Admin, func(c string) bool { return c != model.CmdSQLDatabases }) {
		t.Error("availability")
	}
	e := newWizEditor()
	z := NewWizard(i18n.RU)
	if pr := z.ServerProblem(e); pr == nil || pr.Field != FieldHost {
		t.Errorf("empty server %+v", pr)
	}
	z.Server, z.Login = "tcp:srv,1433", "mon"
	if pr := z.ServerProblem(e); pr == nil || pr.Field != FieldHost || !strings.Contains(pr.Text, i18n.T(i18n.KeyValReasonTcp, i18n.RU)) {
		t.Errorf("tcp %+v", pr)
	}
	z.Server, z.Login = `srv\SQL1`, ""
	if pr := z.ServerProblem(e); pr == nil || pr.Field != FieldLogin {
		t.Errorf("login %+v", pr)
	}
	z.Windows = true
	if pr := z.ServerProblem(e); pr != nil {
		t.Errorf("windows %+v", pr)
	}
	z.Windows, z.Login = false, "mon"
	if r := z.BrowseRequest(); r.Cmd != model.CmdSQLBrowse || r.Host != "srv" {
		t.Errorf("browse %+v", r)
	}
	if r := (&Wizard{}).BrowseRequest(); r.Host != "" {
		t.Error("broadcast")
	}
	txt := z.BrowseDone(model.SQLBrowseResult{Instances: []model.SQLServerInstance{{Host: "SRV", Address: "10.0.0.5", Instance: "SQL1", Version: "10.50", TCPPort: 1500},
		{Host: "SRV", Instance: "MSSQLSERVER"}}, Responders: 1, Partial: true})
	if !strings.Contains(txt, "2") || !strings.Contains(txt, i18n.T(i18n.KeyWizFoundPartial, i18n.RU)) {
		t.Errorf("browse text %q", txt)
	}
	if l := InstanceLine(z.Instances[0]); l != `SRV\SQL1  (10.50, TCP 1500, 10.0.0.5)` {
		t.Errorf("line %q", l)
	}
	if !z.PickInstance(1) || z.Server != "SRV" || z.PickInstance(5) {
		t.Errorf("pick %q", z.Server)
	}
	if !strings.Contains((&Wizard{Lang: i18n.EN}).BrowseDone(model.SQLBrowseResult{}), "SQL Server Browser") {
		t.Error("none found mentions the SQL Browser")
	}
	z.Server, z.Port = `srv\SQL1`, ""
	z.ToForm(e, true)
	req, pr := e.InteractiveRequest(model.CmdSQLDatabases, "pw")
	if pr != nil || req.Profile == nil || req.Profile.Server != `srv\SQL1` || req.Profile.Database != "master" || req.Password != "pw" || req.Profile.Login != "mon" {
		t.Fatalf("databases %+v %+v", req, pr)
	}
	z.DatabasesDone(model.SQLDatabasesResult{Databases: []model.SQLDatabaseInfo{{Name: "ut", Is1C: true, HasNodeTables: true, Probed: true, SizeMB: 120}, {Name: "other"}}, Truncated: true})
	if l := z.DatabaseLine(z.Databases[0]); !strings.Contains(l, "1С") || !strings.Contains(l, "_Node") || !strings.Contains(l, "120") {
		t.Errorf("db line %q", l)
	}
	if z.DatabaseLine(z.Databases[1]) != "other" || !z.PickDatabase(0) || z.Database != "ut" {
		t.Error("db pick")
	}
	z.Step = WizFinish
	z.ToForm(e, true)
	treq, pr := e.TestRequest("pw")
	if pr != nil || treq.Profile.Database != "ut" || treq.Profile.Name != "ut" {
		t.Fatalf("test %+v %+v", treq, pr)
	}
	// autoPick: autoApply; a single candidate ≥ autoPickMin; several -> ask
	c := []model.DiscoveryCandidate{{Table: "_Node1", PointCol: "_Description", SendCol: "a", RecvCol: "b"}, {Table: "_Node2", PointCol: "_Description"}}
	if i, _ := z.DiscoveryDone(model.IPCMessage{Candidates: c, AutoPick: &model.SQLAutoPick{AutoApply: true, Index: 1}}); i != 1 {
		t.Errorf("autoApply %d", i)
	}
	ranks := []model.SQLCandidateRank{{Rank: 1, Confidence: 0.7}, {Rank: 2, Confidence: 0.3}}
	if i, _ := z.DiscoveryDone(model.IPCMessage{Candidates: c, AutoPick: &model.SQLAutoPick{Index: 0, AutoPickMin: 0.6, Ranks: ranks}}); i != 0 {
		t.Errorf("single %d", i)
	}
	ranks[1].Confidence = 0.65
	i, why := z.DiscoveryDone(model.IPCMessage{Candidates: c, AutoPick: &model.SQLAutoPick{ReasonKey: model.MsgSQLAutoPickAmbiguous, AutoPickMin: 0.6, Ranks: ranks}})
	if i != -1 || !strings.Contains(why, i18n.Message(model.MsgSQLAutoPickAmbiguous, i18n.RU)) {
		t.Errorf("ambiguous %d %q", i, why)
	}
	if i, _ := z.DiscoveryDone(model.IPCMessage{Candidates: c}); i != -1 {
		t.Error("no autoPick (older service)")
	}
	if !strings.HasPrefix(z.CandidateLine(0), "") || !z.Choose(e, 0) || e.MapEdit == nil || e.MapEdit.Table != "_Node1" {
		t.Error("choose")
	}
	sreq, pr := e.SaveRequest("pw")
	if pr != nil || sreq.Cmd != model.CmdSaveProfile || sreq.ExpectedRevision != "r1" || sreq.Profile.Mapping.Table != "_Node1" || sreq.Password != "pw" {
		t.Fatalf("save %+v %+v", sreq, pr)
	}
	z.DiscoveryDone(model.IPCMessage{Candidates: c, AutoPick: &model.SQLAutoPick{Ranks: []model.SQLCandidateRank{{Confidence: 0.42}}}})
	if !strings.HasPrefix(z.CandidateLine(0), "[42%]") {
		t.Errorf("confidence %q", z.CandidateLine(0))
	}
}

// TestCHK_TRAY_31_WizardErrors — CHK-TRAY-31: code + diagnosis title and
// steps, the local text without a diagnosis (instance_not_found names the
// SQL Browser), sql_diagnose requests, access_denied / rate_limited / busy.
func TestCHK_TRAY_31_WizardErrors(t *testing.T) {
	d := model.SQLDiagnoseCode(model.SQLCodeLoginFailed, "")
	s := ErrorText(model.SQLCodeLoginFailed, &d, "Login failed for user 'mon'.", i18n.RU)
	if !strings.Contains(s, "login_failed") || !strings.Contains(s, d.Title.RU) || !strings.Contains(s, "1. "+d.Steps[0].RU) || !strings.HasSuffix(s, "'mon'.") {
		t.Errorf("diag %q", s)
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		if s := ErrorText(model.SQLCodeInstanceNotFound, nil, "", lang); !strings.Contains(s, "SQL Server Browser") {
			t.Errorf("%s instance_not_found %q", lang, s)
		}
	}
	for _, c := range model.SQLErrorCodes() {
		if FallbackKey(c) == "" || !i18n.Has(FallbackKey(c)) {
			t.Errorf("no local text for %s", c)
		}
	}
	if FallbackKey("x") != i18n.KeyWizFBOther {
		t.Error("other")
	}
	req, code, ok := DiagnoseRequest(model.IPCMessage{Type: model.MsgError, ErrorInfo: &model.SQLErrorInfo{Code: "tls", TLSKey: model.MsgSQLTLSEnableTLS10}})
	if !ok || code != "tls" || req.Cmd != model.CmdSQLDiagnose || req.Code != "tls" || req.TLSKey != model.MsgSQLTLSEnableTLS10 {
		t.Errorf("info %+v", req)
	}
	req, code, ok = DiagnoseRequest(model.IPCMessage{Type: model.MsgTestResult, Test: &model.TestProfileResult{Category: model.TestLogin, ErrorKey: "k"}})
	if !ok || code != model.TestLogin || req.Category != model.TestLogin || req.ErrorKey != "k" {
		t.Errorf("test %+v", req)
	}
	if _, _, ok := DiagnoseRequest(model.IPCMessage{Type: model.MsgTestResult, Test: &model.TestProfileResult{OK: true}}); ok {
		t.Error("ok test")
	}
	e := newWizEditor()
	if pr := e.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeAccessDenied}, nil); pr.Text != i18n.T(i18n.KeyWizAccessDenied, i18n.RU) {
		t.Errorf("access %q", pr.Text)
	}
	if pr := e.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeRateLimited, RetryAfterMs: 2500}, nil); !strings.Contains(pr.Text, "3 с") {
		t.Errorf("rate %q", pr.Text)
	}
	if pr := e.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeBusy, ErrorKey: model.MsgDiscoverBusy}, nil); pr.Text != i18n.T(i18n.KeyWizBusy, i18n.RU) {
		t.Errorf("busy %q", pr.Text)
	}
	pr := e.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorBadRequest,
		ErrorArgs: []string{"server", model.ValSQLServerForbidden, "via"}}, nil)
	if pr.Field != FieldHost || !strings.Contains(pr.Text, i18n.T(i18n.KeyValReasonVia, i18n.RU)) {
		t.Errorf("bad_request %+v", pr)
	}
}

// TestCHK_TRAY_32_GrantScript — CHK-TRAY-32: the principal (SQL login,
// NT AUTHORITY\SYSTEM locally, DOMAIN\COMPUTER$ remotely), the tables of
// the chosen mapping, the notes text (never the script).
func TestCHK_TRAY_32_GrantScript(t *testing.T) {
	z := NewWizard(i18n.RU)
	z.Server, z.Login, z.Database = `srv\SQL1`, "mon", "ut"
	if r := z.GrantRequest("PC1", "DOM"); r.Cmd != model.CmdSQLGrantScript || r.Principal != "mon" || r.Windows || r.Database != "ut" || len(r.Tables) != 0 {
		t.Errorf("sql %+v", r)
	}
	z.Windows = true
	if p := z.GrantPrincipal("PC1", "DOM"); p != `DOM\PC1$` {
		t.Errorf("remote %q", p)
	}
	for _, h := range []string{".", "localhost", "(local)", "pc1", `PC1\SQL1`} {
		z.Server = h
		if p := z.GrantPrincipal("PC1", "DOM"); p != `NT AUTHORITY\SYSTEM` {
			t.Errorf("%s: %q", h, p)
		}
	}
	z.Candidates = []model.DiscoveryCandidate{{Mode: model.CandidateLinked, Schema: "dbo", Table: "_Node1", PointCol: "_Description", PointKeyCol: "_IDRRef",
		TimesTable: "_InfoRg5", LinkCol: "_Fld6RRef", SendCol: "_Fld7", RecvCol: "_Fld8"}}
	z.Chosen = 0
	r := z.GrantRequest("PC1", "DOM")
	if !r.Windows || strings.Join(r.Tables, ",") != "dbo._Node1,dbo._InfoRg5" {
		t.Errorf("tables %+v", r.Tables)
	}
	sc := model.SQLGrantScript{SQL: "CREATE LOGIN [mon] WITH PASSWORD = N'<PASSWORD>'", PasswordPlaceholder: "<PASSWORD>",
		Notes: []model.LocalizedText{{RU: "заметка", EN: "note"}}}
	txt := z.GrantText(sc)
	if strings.Contains(txt, "CREATE") || !strings.Contains(txt, "<PASSWORD>") || !strings.Contains(txt, "• заметка") {
		t.Errorf("text %q", txt)
	}
}
