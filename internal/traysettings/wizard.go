package traysettings

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9b part 2 (CHK-TRAY-30..32): the «Добавить базу» wizard of the
// administrator settings window. Server (sql_browse or a typed address,
// login kind, TLS mode) → database (sql_databases) → finish (test_profile,
// discover with autoPick, save_profile with expectedRevision). The wizard
// fills the editor of the settings window as a new profile, so the
// requests are exactly the ones of the form and «Расширенные настройки»
// continues in the old form with everything entered so far.

// WizardStep is a step of the wizard.
type WizardStep int

// Wizard steps.
const (
	WizServer WizardStep = iota
	WizDatabase
	WizFinish
)

// WizardTimeout: sql_browse and sql_databases are interactive commands
// (the service ends them by itself; this is the client-side limit).
var WizardTimeout = 30 * time.Second

// Wizard is the state of the «Добавить базу» wizard.
type Wizard struct {
	Step    WizardStep
	Server  string // host or host\instance, as typed or picked
	Port    string
	Windows bool // the computer account of the service (Windows login)
	Login   string
	Encrypt model.EncryptMode

	Instances     []model.SQLServerInstance
	BrowsePartial bool
	Responders    int
	BrowseAdvice  []string                 // sql_browse advice keys
	BrowseLocal   *model.SQLBrowseLocalInfo // local.browserState etc.

	CheckSteps []model.SQLCheckStep // sql_test_steps progress / result

	Databases   []model.SQLDatabaseInfo
	DBTruncated bool
	Database    string

	Tested     bool
	Candidates []model.DiscoveryCandidate
	Pick       *model.SQLAutoPick
	Chosen     int // candidate taken into the profile (-1 = none)
	// ResolveDiscovery of the last discover reply (CHK-TRAY-83): the main
	// mapping, the other plans, the main candidate; Problem when nothing
	// could be applied.
	Main    model.Mapping
	Plans   []model.Mapping
	Problem *Problem
	mainIdx int
	// Best: the candidate to preselect under «Подробнее» when nothing was
	// applied (low_score / incomplete / ambiguous; -1 = none).
	Best int
}

// NewWizard starts with the defaults of a new profile.
func NewWizard() *Wizard {
	f := FromProfile(model.NewProfile(""))
	return &Wizard{Encrypt: f.Encrypt, Chosen: -1, mainIdx: -1, Best: -1}
}

func (z *Wizard) t(key string) string { return i18n.T(key) }

// WizardAvailable: the wizard needs the administrator window and the
// commands it sends (else «Добавить» opens the old form).
func WizardAvailable(mode Mode, has func(string) bool) bool {
	if mode != Admin {
		return false
	}
	for _, c := range []string{model.CmdSQLBrowse, model.CmdSQLDatabases, model.CmdTestProfile, model.CmdDiscover} {
		if !has(c) {
			return false
		}
	}
	return true
}

// ToForm writes the wizard input into the editor form (a new profile).
// passwordTyped marks a typed password (read by the caller per request).
func (z *Wizard) ToForm(e *Editor, passwordTyped bool) {
	if !e.IsNew {
		e.New()
	}
	f := &e.Form
	f.Host, f.Instance = SplitServer(strings.TrimSpace(z.Server))
	f.Port = strings.TrimSpace(z.Port)
	f.Auth = model.AuthSQL
	if z.Windows {
		f.Auth = model.AuthWindows
	}
	f.Login = strings.TrimSpace(z.Login)
	f.Encrypt = z.Encrypt
	f.Database = z.Database
	if f.Database == "" {
		f.Database = "master" // sql_databases needs a database to log in to
	}
	f.Name = UniqueProfileName(e.Profiles, z.Database)
	f.PasswordTyped = passwordTyped && !z.Windows
}

// ServerProblem checks the server step locally: the address
// (model.ValidateSQLServerAddr, the reasons of CHK-TRAY-29) and the login.
func (z *Wizard) ServerProblem(e *Editor) *Problem {
	s := strings.TrimSpace(z.Server)
	if s == "" {
		return e.fieldProblem(&model.FieldError{Field: "server", Key: model.ValRequired})
	}
	if err := model.ValidateSQLServerAddr(s); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return e.fieldProblem(fe)
		}
		return &Problem{Text: err.Error(), Field: FieldHost}
	}
	if !z.Windows && strings.TrimSpace(z.Login) == "" {
		return e.fieldProblem(&model.FieldError{Field: "login", Key: model.ValRequired})
	}
	return nil
}

// BrowseRequest is sql_browse with empty Mode (local+broadcast, or host
// when the server box is filled) — CHK-TRAY-52 default search.
func (z *Wizard) BrowseRequest() model.IPCRequest {
	return z.BrowseRequestMode(model.SQLBrowseAuto)
}

// BrowseDone takes a sql_browse reply; the text is the result line plus
// advice (CHK-TRAY-52).
func (z *Wizard) BrowseDone(r model.SQLBrowseResult) string {
	return FormatBrowseDone(z, r)
}

// InstanceLine is a row of the instance list.
func InstanceLine(i model.SQLServerInstance) string {
	s := i.Server()
	var extra []string
	if i.Version != "" {
		extra = append(extra, i.Version)
	}
	if i.TCPPort > 0 {
		extra = append(extra, fmt.Sprintf("TCP %d", i.TCPPort))
	}
	if i.Address != "" && i.Address != i.Host {
		extra = append(extra, i.Address)
	}
	if len(extra) > 0 {
		s += "  (" + strings.Join(extra, ", ") + ")"
	}
	return s
}

// PickInstance takes a found instance: its server name; the port box is
// cleared (the SQL Browser resolves a named instance).
func (z *Wizard) PickInstance(i int) bool {
	if i < 0 || i >= len(z.Instances) {
		return false
	}
	z.Server, z.Port = z.Instances[i].Server(), ""
	return true
}

// DatabasesDone takes a sql_databases reply: 1C databases first, then by
// name; the result line.
func (z *Wizard) DatabasesDone(r model.SQLDatabasesResult) string {
	z.Databases = append([]model.SQLDatabaseInfo(nil), r.Databases...)
	z.DBTruncated = r.Truncated
	s := fmt.Sprintf(z.t(i18n.KeyWizDBCount), len(z.Databases))
	if r.Truncated {
		s += " " + z.t(i18n.KeyWizDBTruncated)
	}
	return s
}

// DatabaseLine is a row of the database list: name, 1C / _Node marks,
// size.
func (z *Wizard) DatabaseLine(d model.SQLDatabaseInfo) string {
	s := d.Name
	var marks []string
	if d.Is1C {
		marks = append(marks, z.t(i18n.KeyWizDB1C))
	}
	if d.HasNodeTables {
		marks = append(marks, z.t(i18n.KeyWizDBNode))
	}
	if d.SizeMB > 0 {
		marks = append(marks, fmt.Sprintf(z.t(i18n.KeyWizDBSize), d.SizeMB))
	}
	if len(marks) > 0 {
		s += "  — " + strings.Join(marks, ", ")
	}
	return s
}

// PickDatabase takes the database of list row i.
func (z *Wizard) PickDatabase(i int) bool {
	if i < 0 || i >= len(z.Databases) {
		return false
	}
	z.Database = z.Databases[i].Name
	z.Candidates, z.Pick, z.Chosen, z.Tested = nil, nil, -1, false
	z.Main, z.Plans, z.Problem, z.mainIdx, z.Best = model.Mapping{}, nil, nil, -1, -1
	return true
}

// DiscoveryDone takes the discover reply and decides by itself
// (fix/tray-wizard-autofind, CHK-TRAY-83): ResolveDiscovery — the same
// decision as the expert dialog — gives the main mapping and the mappings
// of every other exchange plan (autoPick Plans, 1C 8.1 counters
// included); the returned candidate is the main one, applied without
// asking (Choose). -1 only for a real problem (Problem: none, ambiguous,
// truncated, low_score, incomplete; Best is then the candidate to
// preselect); the text is «Найдено: план X (+N), поля …» or the problem.
func (z *Wizard) DiscoveryDone(m model.IPCMessage) (int, string) {
	z.Candidates = append([]model.DiscoveryCandidate(nil), m.Candidates...)
	z.Pick = m.AutoPick
	z.Chosen = -1
	z.Main, z.Plans, z.Problem = model.Mapping{}, nil, nil
	main, plans, idx, pr := resolveDiscovery(z.Candidates)
	z.Best = idx
	if pr != nil {
		if pr.Code == ProblemNone && m.Truncated {
			pr = &Problem{Code: ProblemTruncated, Text: z.t(i18n.KeyWizErrTruncated)}
		}
		z.Problem = pr
		// nothing applied; a candidate picked under «Подробнее» keeps the
		// other plans (offered, as in the expert dialog)
		z.Main, z.Plans = offeredPlans(z.Candidates)
		return -1, pr.Text
	}
	z.Main, z.Plans, z.mainIdx = main, plans, idx
	text := FoundText(main, plans)
	if main.Mode == model.MappingCounters || anyCounters(plans) {
		text += "\r\n" + i18n.Message(model.MsgSQLAutoPickCountersOK)
	}
	if m.Truncated {
		text += "\r\n" + z.t(i18n.KeyWizNoteTruncated)
	}
	return idx, text
}

func anyCounters(ms []model.Mapping) bool {
	for _, m := range ms {
		if m.Mode == model.MappingCounters {
			return true
		}
	}
	return false
}

// TestProblem is the wizard text of a failed test_profile that is not a
// connection problem: Windows login of an unsaved profile
// (monitor.test_windows_unsaved, CHK-TRAY-85); nil = show the diagnosis.
func (z *Wizard) TestProblem(m model.IPCMessage) *Problem {
	key := m.ErrorKey
	if m.Test != nil && m.Test.ErrorKey != "" {
		key = m.Test.ErrorKey
	}
	if key == model.MsgTestWindowsUnsaved {
		return &Problem{Code: ProblemWindowsUnsaved, Text: z.t(i18n.KeyWizErrWinUnsaved)}
	}
	return nil
}

// CandidateLine is a row of the candidate list with its confidence.
func (z *Wizard) CandidateLine(i int) string {
	d := Discovery{}
	s := d.CandidateLine(z.Candidates[i])
	if z.Pick != nil && i < len(z.Pick.Ranks) {
		s = fmt.Sprintf(z.t(i18n.KeyWizConfidence), int(z.Pick.Ranks[i].Confidence*100+0.5)) + "  " + s
	}
	return s
}

// Choose puts candidate i into the edited profile with the mappings of
// all other exchange plans: the resolved main candidate gives exactly
// ResolveDiscovery's (main, plans); another candidate picked under
// «Подробнее» becomes the main mapping and the resolved plans other than
// its own stay additional mappings (no plan is lost).
func (z *Wizard) Choose(e *Editor, i int) bool {
	if i < 0 || i >= len(z.Candidates) {
		return false
	}
	z.Chosen = i
	if z.Problem == nil && i == z.mainIdx && z.Main.Configured() {
		e.SetMapping(z.Main, z.Plans)
		return true
	}
	m := z.Candidates[i].Mapping()
	e.SetMapping(m, otherPlans(m, z.Main, z.Plans))
	return true
}

// otherPlans: the resolved mappings (main + plans) without the plan of m.
func otherPlans(m, main model.Mapping, plans []model.Mapping) []model.Mapping {
	var out []model.Mapping
	seen := map[string]bool{PlanKey(m): true}
	for _, x := range append([]model.Mapping{main}, plans...) {
		k := PlanKey(x)
		if !x.Configured() || k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, x)
	}
	if len(out)+1 > model.MaxMappings {
		out = out[:model.MaxMappings-1]
	}
	return out
}

// IsLocalServer: the SQL Server runs on this computer (the service then
// connects as NT AUTHORITY\SYSTEM, else as DOMAIN\COMPUTER$).
func IsLocalServer(host, computer string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	switch h {
	case ".", "localhost", "(local)", "127.0.0.1", "::1":
		return true
	}
	return computer != "" && strings.EqualFold(h, computer)
}

// GrantPrincipal is the login the grant script is for: the SQL login, or
// the account the service connects with (LocalSystem: NT AUTHORITY\SYSTEM
// on the same computer, DOMAIN\COMPUTER$ on another one).
func (z *Wizard) GrantPrincipal(computer, domain string) string {
	if !z.Windows {
		return strings.TrimSpace(z.Login)
	}
	host, _ := SplitServer(strings.TrimSpace(z.Server))
	if IsLocalServer(host, computer) || computer == "" {
		return `NT AUTHORITY\SYSTEM`
	}
	if domain == "" {
		domain = computer
	}
	return domain + `\` + computer + "$"
}

// GrantRequest is sql_grant_script for the chosen database: SELECT on the
// tables of the chosen mapping, else db_datareader.
func (z *Wizard) GrantRequest(computer, domain string) model.IPCRequest {
	req := model.IPCRequest{Cmd: model.CmdSQLGrantScript, Principal: z.GrantPrincipal(computer, domain), Windows: z.Windows, Database: z.Database}
	if z.Chosen >= 0 && z.Chosen < len(z.Candidates) {
		m := z.Candidates[z.Chosen].Mapping()
		ms := append([]model.Mapping{m}, otherPlans(m, z.Main, z.Plans)...)
		seen := map[string]bool{}
		add := func(schema, table string) {
			if table == "" {
				return
			}
			if schema == "" {
				schema = "dbo"
			}
			if k := strings.ToLower(schema + "." + table); !seen[k] {
				seen[k] = true
				req.Tables = append(req.Tables, schema+"."+table)
			}
		}
		for _, m := range ms { // every plan the profile will poll
			add(m.Schema, m.Table)
			ts := m.TimesSchema
			if ts == "" {
				ts = m.Schema
			}
			add(ts, m.TimesTable)
		}
	}
	return req
}

// GrantText is the note shown after the script was copied (never the
// script itself).
func (z *Wizard) GrantText(sc model.SQLGrantScript) string {
	lines := []string{z.t(i18n.KeyWizGrantCopied)}
	if sc.PasswordPlaceholder != "" {
		lines = append(lines, fmt.Sprintf(z.t(i18n.KeyWizGrantPlaceholder), sc.PasswordPlaceholder))
	}
	for _, n := range sc.Notes {
		lines = append(lines, "• "+n)
	}
	return strings.Join(lines, "\r\n")
}

// wizFallback: the local «что делать» texts per errorInfo code, used when
// the service gives no diagnosis (sql_diagnose missing or failed).
var wizFallback = map[string]string{
	model.SQLCodeLoginFailed: i18n.KeyWizFBLogin, model.SQLCodeDatabaseUnavailable: i18n.KeyWizFBDatabase,
	model.SQLCodeTLS: i18n.KeyWizFBTLS, model.SQLCodeTimeout: i18n.KeyWizFBTimeout, model.SQLCodeNetwork: i18n.KeyWizFBNetwork,
	model.SQLCodeBrowserUnreachable: i18n.KeyWizFBBrowser, model.SQLCodeInstanceNotFound: i18n.KeyWizFBInstance,
	model.SQLCodePermissionDenied: i18n.KeyWizFBPermission, model.SQLCodeNot1CDatabase: i18n.KeyWizFBNot1C,
	model.SQLCodeConfig: i18n.KeyWizFBConfig,
	// test_profile categories (no errorInfo in a test result)
	model.TestLogin: i18n.KeyWizFBLogin, model.TestDatabase: i18n.KeyWizFBDatabase,
}

// FallbackKey is the local text key of an error code / test category.
func FallbackKey(code string) string {
	if k, ok := wizFallback[code]; ok {
		return k
	}
	return i18n.KeyWizFBOther
}

// ErrorText renders an error for the wizard and the connection test: the
// code, then the diagnosis of sql_diagnose (title + numbered steps) or
// the local text, then the technical detail.
func ErrorText(code string, d *model.SQLDiagnosis, detail string) string {
	lines := []string{fmt.Sprintf(i18n.T(i18n.KeyWizErrCode), code)}
	if d != nil && d.Title != "" {
		lines = append(lines, d.Title)
		if len(d.Steps) > 0 {
			lines = append(lines, i18n.T(i18n.KeyWizWhatToDo))
			for i, s := range d.Steps {
				lines = append(lines, fmt.Sprintf("%d. %s", i+1, s))
			}
		}
	} else {
		lines = append(lines, i18n.T(i18n.KeyWizWhatToDo), i18n.T(FallbackKey(code)))
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		lines = append(lines, detail)
	}
	return strings.Join(lines, "\r\n")
}

// DiagnoseRequest is the sql_diagnose of an error reply (errorInfo code +
// tlsKey) or of a failed test (category + errorKey); ok = false: nothing
// to ask.
func DiagnoseRequest(m model.IPCMessage) (model.IPCRequest, string, bool) {
	if m.ErrorInfo != nil && m.ErrorInfo.Code != "" {
		return model.IPCRequest{Cmd: model.CmdSQLDiagnose, Code: m.ErrorInfo.Code, TLSKey: m.ErrorInfo.TLSKey}, m.ErrorInfo.Code, true
	}
	if m.Type == model.MsgTestResult && m.Test != nil && !m.Test.OK {
		return model.IPCRequest{Cmd: model.CmdSQLDiagnose, Category: m.Test.Category, ErrorKey: m.Test.ErrorKey}, m.Test.Category, true
	}
	return model.IPCRequest{}, "", false
}

// WizardProblem renders an error reply of a wizard command without a
// diagnosis: access_denied (no new elevation: the settings window must be
// the elevated one), rate_limited with retryAfterMs, busy; the rest as
// the form does (invalid_request focuses errorArgs[0]).
func (e *Editor) WizardProblem(m model.IPCMessage, err error) *Problem {
	if err != nil {
		return e.InteractiveProblem(m, err)
	}
	switch m.Code {
	case model.ErrCodeAccessDenied:
		return &Problem{Text: e.t(i18n.KeyWizAccessDenied)}
	case model.ErrCodeRateLimited:
		return &Problem{Text: fmt.Sprintf(e.t(i18n.KeyWizRateLimited), RetryAfterSeconds(m.RetryAfterMs))}
	case model.ErrCodeBusy:
		if m.RetryAfterMs > 0 {
			return &Problem{Text: fmt.Sprintf(e.t(i18n.KeyWizBusyRetry), RetryAfterSeconds(m.RetryAfterMs))}
		}
		return &Problem{Text: e.t(i18n.KeyWizBusy)}
	}
	return e.InteractiveProblem(m, nil)
}

// ClearMapping drops a mapping chosen for another database.
func (e *Editor) ClearMapping() { e.clearMapping() }

// UniqueProfileName is the database name, with « (2)», « (3)», … when a
// profile already has that name (case-insensitive, spaces trimmed).
func UniqueProfileName(ps []model.Profile, db string) string {
	db = strings.TrimSpace(db)
	if db == "" {
		return ""
	}
	taken := map[string]bool{}
	for _, p := range ps {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}
	name := db
	for n := 2; taken[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s (%d)", db, n)
	}
	return name
}
