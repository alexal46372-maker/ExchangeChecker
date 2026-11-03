package monitor

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
)

// fix/tray-wizard-autofind (CHK-TRAY-83..85): the real chain of the
// «Добавить базу» wizard — traysettings.Wizard/Editor → ipc.Client →
// ipc.Server (fitAutoPick) → Monitor.Discover → sqlcheck.Discover over an
// in-memory 1C 8.1 store (exchange plans with message counters only, no
// time registers). No canned AutoPick anywhere.

// store81 is an in-memory SQL Server database for the chain: 1C 8.1
// exchange plans with message counters only (_NodeNNN: _IDRRef, _Code,
// _Description, _SentNo, _ReceivedNo), optionally an information
// register linked to a plan (_InfoRgNNN: _FldNRRef → _Node._IDRRef, two
// datetime columns), a 1C directory with its own datetime columns linked
// from a register too (two candidates for one table), and a non-1C table
// (generic). Statistics answer like a live database: every row matches,
// the times are an hour old.
type store81 struct {
	plans   map[string]int // table -> number of points
	order   []string
	linked  map[string]string // _InfoRgNNN -> owner table
	dirs    map[string]int    // _ReferenceNNN with datetime columns -> rows
	generic map[string]int    // non-1C table -> rows
}

func newStore81(plans ...interface{}) *store81 {
	s := &store81{plans: map[string]int{}, linked: map[string]string{}, dirs: map[string]int{}, generic: map[string]int{}}
	for i := 0; i+1 < len(plans); i += 2 {
		t := plans[i].(string)
		s.plans[t] = plans[i+1].(int)
		s.order = append(s.order, t)
	}
	return s
}

// link adds _InfoRg<n> linked to owner (a plan or a directory).
func (s *store81) link(reg, owner string) *store81 { s.linked[reg] = owner; return s }

// dir adds a 1C directory with datetime columns.
func (s *store81) dir(name string, rows int) *store81 { s.dirs[name] = rows; return s }

// gen adds a non-1C exchange log table.
func (s *store81) gen(name string, rows int) *store81 { s.generic[name] = rows; return s }

func (s *store81) rowsOf(t string) int {
	if n, ok := s.plans[t]; ok {
		return n
	}
	if n, ok := s.dirs[t]; ok {
		return n
	}
	if n, ok := s.generic[t]; ok {
		return n
	}
	if o, ok := s.linked[t]; ok {
		return s.rowsOf(o)
	}
	return 0
}

func key81(table string, i int) []byte {
	k := make([]byte, 16)
	copy(k, table)
	k[15] = byte(i + 1)
	return k
}

func (s *store81) Ping(context.Context) error { return nil }
func (s *store81) Columns(context.Context) ([]sqlcheck.Column, error) {
	var out []sqlcheck.Column
	col := func(t, name, typ string, ml int) {
		out = append(out, sqlcheck.Column{Table: sqlcheck.TableRef{Schema: "dbo", Name: t}, Name: name, DataType: typ, MaxLength: ml, Ordinal: len(out) + 1})
	}
	for _, t := range s.order {
		col(t, "_IDRRef", "binary", 16)
		col(t, "_Code", "nvarchar", 9)
		col(t, "_Description", "nvarchar", 50)
		col(t, "_SentNo", "numeric", 0)
		col(t, "_ReceivedNo", "numeric", 0)
	}
	for t := range s.dirs {
		col(t, "_IDRRef", "binary", 16)
		col(t, "_Code", "nvarchar", 9)
		col(t, "_Description", "nvarchar", 50)
		col(t, "_Fld5", "datetime", 0)
		col(t, "_Fld6", "datetime", 0)
	}
	for t := range s.linked {
		col(t, "_Fld1RRef", "binary", 16)
		col(t, "_Fld2", "datetime", 0)
		col(t, "_Fld3", "datetime", 0)
	}
	for t := range s.generic {
		col(t, "NodeName", "nvarchar", 50)
		col(t, "LastSend", "datetime", 0)
		col(t, "LastRecv", "datetime", 0)
	}
	// a distractor: no exchange data
	col("_Reference7", "_IDRRef", "binary", 16)
	col("_Reference7", "_Description", "nvarchar", 50)
	return out, nil
}
func (s *store81) RowCounts(context.Context) (map[sqlcheck.TableRef]int64, error) {
	out := map[sqlcheck.TableRef]int64{}
	for _, m := range []map[string]int{s.plans, s.dirs, s.generic} {
		for t, n := range m {
			out[sqlcheck.TableRef{Schema: "dbo", Name: t}] = int64(n)
		}
	}
	for t := range s.linked {
		out[sqlcheck.TableRef{Schema: "dbo", Name: t}] = int64(s.rowsOf(t))
	}
	return out, nil
}
func (s *store81) SampleColumns(_ context.Context, t sqlcheck.TableRef, cols []string, limit int) ([][]interface{}, error) {
	owner, isReg := s.linked[t.Name], false
	if owner != "" {
		isReg = true
	} else {
		owner = t.Name
	}
	var out [][]interface{}
	for i := 0; i < s.rowsOf(t.Name) && (limit <= 0 || i < limit); i++ {
		r := make([]interface{}, len(cols))
		for j, c := range cols {
			if (!isReg && c == "_IDRRef") || (isReg && c == "_Fld1RRef") {
				r[j] = key81(owner, i)
			}
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *store81) hourOld(n int) []*time.Time {
	at := t0.Add(-time.Hour)
	out := make([]*time.Time, n)
	for i := range out {
		v := at.Add(-time.Duration(i) * time.Minute)
		out[i] = &v
	}
	return out
}
func (s *store81) LinkStats(_ context.Context, q sqlcheck.LinkQuery) (sqlcheck.LinkStats, error) {
	if s.linked[q.Times.Name] != q.Points.Name {
		return sqlcheck.LinkStats{}, nil
	}
	n := int64(s.rowsOf(q.Points.Name))
	return sqlcheck.LinkStats{Sampled: n, Matched: n, Points: n, Max: s.hourOld(len(q.DateCols))}, nil
}
func (s *store81) SingleStats(_ context.Context, q sqlcheck.SingleQuery) (sqlcheck.SingleStats, error) {
	n := int64(s.rowsOf(q.Table.Name))
	return sqlcheck.SingleStats{Sampled: n, Points: n, Max: s.hourOld(len(q.DateCols))}, nil
}
func (s *store81) Points(_ context.Context, m model.Mapping, limit int) ([]sqlcheck.RawPoint, error) {
	var out []sqlcheck.RawPoint
	for i := 0; i < s.rowsOf(m.Table) && (limit <= 0 || i < limit); i++ {
		ts := s.hourOld(2)
		out = append(out, sqlcheck.RawPoint{Key: key81(m.Table, i), Name: fmt.Sprintf("%s point %d", m.Table, i+1), Send: ts[0], Recv: ts[1]})
	}
	return out, nil
}
func (s *store81) SearchText(context.Context, sqlcheck.TableRef, []string, []string, string, int) ([][]interface{}, error) {
	return nil, nil
}
func (s *store81) SearchRef(context.Context, sqlcheck.TableRef, []string, []string, []byte, int) ([][]interface{}, error) {
	return nil, nil
}
func (s *store81) SearchComposite(context.Context, sqlcheck.TableRef, []string, sqlcheck.CompositeRef, []byte, int) ([][]interface{}, error) {
	return nil, nil
}
func (s *store81) Counters(_ context.Context, m model.Mapping, limit int) ([]sqlcheck.RawCounter, error) {
	n := s.plans[m.Table]
	var out []sqlcheck.RawCounter
	for i := 0; i < n && (limit <= 0 || i < limit); i++ {
		out = append(out, sqlcheck.RawCounter{Key: key81(m.Table, i), Name: fmt.Sprintf("%s point %d", m.Table, i+1), ID: fmt.Sprint(i + 1),
			SentNo: int64(100 + i), ReceivedNo: int64(90 + i)})
	}
	return out, nil
}
func (s *store81) ManualPoints(context.Context, string, int) ([]sqlcheck.RawPoint, error) {
	return nil, nil
}
func (s *store81) Close() error { return nil }

// reader81 is the InteractiveReader of the service over store81: Discover
// is the production sqlcheck.Discover.
type reader81 struct {
	st  sqlcheck.Store
	opt sqlcheck.Options
}

func (r reader81) Discover(ctx context.Context) ([]model.DiscoveryCandidate, error) {
	return sqlcheck.Discover(ctx, r.st, r.opt.Discover, t0)
}
func (r reader81) DiscoverByValue(context.Context, model.ValueQuery) ([]model.DiscoveryCandidate, error) {
	return nil, nil
}
func (r reader81) NodePoints(context.Context) ([]model.NodePoint, error) { return nil, nil }
func (r reader81) PreviewMapping(context.Context, model.Mapping, int) ([]model.SampleRow, error) {
	return nil, nil
}
func (r reader81) Close() error { return nil }

type chainEnv struct {
	t  *testing.T
	m  *Monitor
	c  *ipc.Client
	st *store81
}

// newChain: the service (Monitor + ipc.Server, administrator client).
func newChain(t *testing.T, st *store81) *chainEnv {
	t.Helper()
	cs := config.NewStore(t.TempDir())
	cs.Protector = plainProtector{}
	opt := baseOpt(newFactory(), FileConfig{Store: cs}, &fakeClock{t: t0})
	opt.Log, opt.CommandInterval = &memLog{}, -1
	opt.Interactive = func(p model.Profile, pw string, o sqlcheck.Options) (InteractiveReader, error) {
		return reader81{st: st, opt: o}, nil
	}
	m := New(opt)
	m.reloadConfig(true)
	srv := ipc.NewServer(m, &memLog{})
	srv.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `HOST\admin`}, nil }
	l := ipc.NewMemListener()
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	var hello model.IPCMessage
	if err := ipc.ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	c := ipc.NewClient(rw, nil)
	t.Cleanup(func() { c.Close() })
	e := &chainEnv{t: t, m: m, c: c, st: st}
	// the real client_hello of the tray over this connection: the service
	// sends autoPick v2 (Plans, AutoApply for 1C 8.1) only after it
	if r := e.do(model.IPCRequest{Cmd: model.CmdClientHello, Capabilities: append([]string(nil), trayipc.ClientHelloCaps...)}); r.Type != model.MsgClientHello {
		t.Fatalf("client_hello %+v", r)
	}
	return e
}

func (e *chainEnv) do(req model.IPCRequest) model.IPCMessage {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := e.c.Do(ctx, req)
	if err != nil {
		e.t.Fatalf("%s: %v", req.Cmd, err)
	}
	return m
}

func (e *chainEnv) editor() *traysettings.Editor {
	e.t.Helper()
	ed := traysettings.NewEditor(traysettings.Admin)
	cfg := e.do(model.IPCRequest{Cmd: model.CmdGetConfig})
	if cfg.Config == nil {
		e.t.Fatalf("get_config %+v", cfg)
	}
	ed.LoadConfig(*cfg.Config, cfg.Revision)
	return ed
}

// wizardDiscover runs the wizard up to the discover reply (test_profile
// first, like the dialog) and returns the reply.
func (e *chainEnv) wizardDiscover(ed *traysettings.Editor, z *traysettings.Wizard, windows bool) (model.IPCMessage, model.IPCMessage) {
	e.t.Helper()
	z.Server, z.Login, z.Windows, z.Database, z.Step = `srv\INST`, "mon", windows, "ut81", traysettings.WizFinish
	z.ToForm(ed, !windows)
	var tm model.IPCMessage
	if windows { // test_profile of the service (the SQL login one needs a real server)
		treq, pr := ed.TestRequest("pw")
		if pr != nil {
			e.t.Fatalf("test request %+v", pr)
		}
		tm = e.do(treq)
	}
	req, pr := ed.InteractiveRequest(model.CmdDiscover, "pw")
	if pr != nil {
		e.t.Fatalf("discover request %+v", pr)
	}
	return tm, e.do(req)
}

func plansOf(m model.Mapping, ms []model.Mapping) []string {
	out := []string{traysettings.PlanKey(m)}
	for _, x := range ms {
		out = append(out, traysettings.PlanKey(x))
	}
	return out
}

// TestCHK_TRAY_83_WizardFindsAll81Plans — a full wizard pass over a 1C
// 8.1 database (counters only, three plans): the wizard finds the tables
// itself, applies the main plan and EVERY other plan of autoPick Plans
// without a manual pick, shows «Найдено: план X (+N), поля …» and saves;
// the saved profile has the main Mapping + Mappings of all other plans
// (count and identity checked: no plan lost). The tray declares
// autopick_plans_v2 in client_hello.
func TestCHK_TRAY_83_WizardFindsAll81Plans(t *testing.T) {
	e := newChain(t, newStore81("_Node12", 5, "_Node34", 3, "_Node56", 2))
	{
		ed := e.editor()
		z := traysettings.NewWizard()
		tm, m := e.wizardDiscover(ed, z, false)
		if tm.Type != "" {
			t.Fatalf("%s: unexpected test_profile %+v", i18n.RU, tm)
		}
		if m.Type != model.MsgDiscovery || m.AutoPick == nil || len(m.AutoPick.Plans) != 3 || !m.AutoPick.AutoApply ||
			m.AutoPick.ReasonKey != model.MsgSQLAutoPickCountersOK {
			t.Fatalf("%s: discover reply %+v", i18n.RU, m)
		}
		idx, text := z.DiscoveryDone(m)
		if idx < 0 || z.Problem != nil {
			t.Fatalf("%s: not found by itself: %d %q", i18n.RU, idx, text)
		}
		// no manual step: the wizard's own choice goes into the profile
		if !z.Choose(ed, idx) || ed.MapEdit == nil {
			t.Fatalf("%s: choose", i18n.RU)
		}
		want := []string{"dbo._node12", "dbo._node34", "dbo._node56"}
		if got := plansOf(*ed.MapEdit, ed.MapsEdit); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: applied plans %v, want %v", i18n.RU, got, want)
		}
		found := fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlan), "_Node12") + fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlans), 2)
		if !strings.Contains(text, found) || !strings.Contains(text, "_SentNo") ||
			!strings.Contains(text, i18n.Message(model.MsgSQLAutoPickCountersOK)) {
			t.Errorf("%s: result text %q", i18n.RU, text)
		}
		sreq, pr := ed.SaveRequest("pw")
		if pr != nil {
			t.Fatalf("%s: save request %+v", i18n.RU, pr)
		}
		if r := e.do(sreq); r.Type == model.MsgError {
			t.Fatalf("%s: save_profile %+v", i18n.RU, r)
		}
		cfg := e.do(model.IPCRequest{Cmd: model.CmdGetConfig})
		var saved *model.Profile
		for i := range cfg.Config.Profiles {
			if cfg.Config.Profiles[i].ID == sreq.Profile.ID || (sreq.Profile.ID == "" && cfg.Config.Profiles[i].Name == sreq.Profile.Name) {
				saved = &cfg.Config.Profiles[i]
			}
		}
		if saved == nil {
			t.Fatalf("%s: saved profile missing: %+v", i18n.RU, cfg.Config.Profiles)
		}
		if got := plansOf(saved.Mapping, saved.Mappings); len(saved.Mappings) != 2 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: saved plans %v (mappings %d), want %v", i18n.RU, got, len(saved.Mappings), want)
		}
		for _, x := range append([]model.Mapping{saved.Mapping}, saved.Mappings...) {
			if x.Mode != model.MappingCounters || x.SentNoColumn != "_SentNo" || x.ReceivedNoColumn != "_ReceivedNo" || x.PointNameColumn != "_Description" {
				t.Errorf("%s: saved mapping %+v", i18n.RU, x)
			}
		}
	}
	has := false
	for _, c := range trayipc.ClientHelloCaps {
		has = has || c == model.CapAutoPickPlans
	}
	if !has {
		t.Error("client_hello does not declare autopick_plans_v2")
	}
}

// TestCHK_TRAY_84_WizardEqualsExpert — on the same fixture the expert
// «Таблицы обмена…» dialog selects the same main mapping and offers the
// same other plans («все планы обмена») as the wizard applies.
func TestCHK_TRAY_84_WizardEqualsExpert(t *testing.T) {
	linked := newStore81("_Node12", 4, "_Node34", 3).link("_InfoRg100", "_Node12") // like CHK-SQL-17
	for _, st := range []*store81{newStore81("_Node12", 5, "_Node34", 3, "_Node56", 2), newStore81("_Node7", 1), newStore81("_Node40", 2, "_Node41", 9), linked} {
		e := newChain(t, st)
		ed := e.editor()
		z := traysettings.NewWizard()
		_, m := e.wizardDiscover(ed, z, false)
		idx, text := z.DiscoveryDone(m)
		if idx < 0 || !z.Choose(ed, idx) {
			t.Fatalf("%v: wizard %d %q", st.order, idx, text)
		}
		d := traysettings.NewDiscovery(model.Mapping{})
		d.DiscoveryDone(m)
		main, _ := d.Result()
		expert := plansOf(main, d.PlanMappings(main))
		wizard := plansOf(*ed.MapEdit, ed.MapsEdit)
		if strings.Join(expert, ",") != strings.Join(wizard, ",") || d.Sel != idx {
			t.Errorf("%v: expert %v (sel %d) != wizard %v (%d)", st.order, expert, d.Sel, wizard, idx)
		}
		rm, rp, pr := traysettings.ResolveDiscovery(m.Candidates)
		if pr != nil || rm != *ed.MapEdit || len(rp) != len(ed.MapsEdit) {
			t.Errorf("%v: ResolveDiscovery %+v %d %+v", st.order, rm, len(rp), pr)
		}
		if st == linked && (ed.MapEdit.Mode != model.MappingAuto || ed.MapEdit.Table != "_Node12" || ed.MapEdit.TimesTable != "_InfoRg100" ||
			len(ed.MapsEdit) != 1 || ed.MapsEdit[0].Table != "_Node34" || ed.MapsEdit[0].Mode != model.MappingCounters) {
			t.Errorf("linked + counters plan: main %+v extra %+v", *ed.MapEdit, ed.MapsEdit)
		}
		if len(z.Plans)+1 != len(st.order) {
			t.Errorf("%v: plans lost: %d of %d", st.order, len(z.Plans)+1, len(st.order))
		}
	}
}

// TestCHK_TRAY_85_WizardErrorTexts — errors only when real, each with its
// own RU and EN text: none (no exchange tables; offers the permissions
// script), ambiguous (names both candidates), windows_unsaved (the Windows
// login of a new profile, from the real test_profile), truncated (search
// cut short, nothing found).
func TestCHK_TRAY_85_WizardErrorTexts(t *testing.T) {
	texts := map[string]map[string]string{}
	{
		texts[i18n.RU] = map[string]string{}
		// none: a database without exchange plans, through the chain
		e := newChain(t, newStore81())
		ed := e.editor()
		z := traysettings.NewWizard()
		_, m := e.wizardDiscover(ed, z, false)
		if idx, txt := z.DiscoveryDone(m); idx != -1 || z.Problem == nil || z.Problem.Code != traysettings.ProblemNone || txt != z.Problem.Text {
			t.Fatalf("%s: none %d %q", i18n.RU, idx, txt)
		}
		if !strings.Contains(z.Problem.Text, i18n.T(i18n.KeyWizGrant)) {
			t.Errorf("%s: none does not offer the permissions script: %q", i18n.RU, z.Problem.Text)
		}
		texts[i18n.RU]["none"] = z.Problem.Text
		// truncated: nothing found and the search was cut short
		m.Truncated, m.TruncatedReason = true, model.TruncatedBudget
		if idx, _ := z.DiscoveryDone(m); idx != -1 || z.Problem == nil || z.Problem.Code != traysettings.ProblemTruncated {
			t.Fatalf("%s: truncated %+v", i18n.RU, z.Problem)
		}
		texts[i18n.RU]["truncated"] = z.Problem.Text
		// windows_unsaved: the real test_profile of a new Windows profile
		e2 := newChain(t, newStore81("_Node12", 5))
		ed2 := e2.editor()
		z2 := traysettings.NewWizard()
		tm, _ := e2.wizardDiscover(ed2, z2, true)
		pr := z2.TestProblem(tm)
		if pr == nil || pr.Code != traysettings.ProblemWindowsUnsaved {
			t.Fatalf("%s: windows_unsaved from %+v", i18n.RU, tm)
		}
		texts[i18n.RU]["windows_unsaved"] = pr.Text
		// ambiguous: two equally good candidates for the same table
		a := model.DiscoveryCandidate{Mode: model.CandidateSingle, Schema: "dbo", Table: "ExchLog", PointCol: "Node", SendCol: "Sent", RecvCol: "Recv", Score: 150, Points: 4}
		b := a
		b.PointCol = "NodeCode"
		_, _, apr := traysettings.ResolveDiscovery([]model.DiscoveryCandidate{a, b})
		if apr == nil || apr.Code != traysettings.ProblemAmbiguous {
			t.Fatalf("%s: ambiguous %+v", i18n.RU, apr)
		}
		if !strings.Contains(apr.Text, "(Node, Sent, Recv)") || !strings.Contains(apr.Text, "(NodeCode, Sent, Recv)") {
			t.Errorf("%s: ambiguous must name both candidates: %q", i18n.RU, apr.Text)
		}
		texts[i18n.RU]["ambiguous"] = apr.Text
		texts[i18n.RU]["low_score"] = i18n.T(i18n.KeyWizErrLowScore)
		texts[i18n.RU]["incomplete"] = i18n.T(i18n.KeyWizErrIncomplete)
		// ambiguous through the chain: a 1C directory with its own times
		// and a register linked to it — two candidates for one table
		e3 := newChain(t, newStore81().dir("_Reference20", 4).link("_InfoRg200", "_Reference20"))
		ed3 := e3.editor()
		z3 := traysettings.NewWizard()
		_, m3 := e3.wizardDiscover(ed3, z3, false)
		if idx, txt := z3.DiscoveryDone(m3); idx != -1 || z3.Problem == nil || z3.Problem.Code != traysettings.ProblemAmbiguous ||
			!strings.Contains(txt, "_Fld2") || !strings.Contains(txt, "_Fld5") || z3.Best < 0 || ed3.MapEdit != nil {
			t.Errorf("%s: chain ambiguous %d %q best %d", i18n.RU, idx, txt, z3.Best)
		}
		if texts[i18n.RU]["ambiguous"] == z3.Problem.Text {
			t.Errorf("%s: ambiguous texts must name the candidates", i18n.RU)
		}
		// a single good candidate is no error
		if _, _, p := traysettings.ResolveDiscovery([]model.DiscoveryCandidate{a}); p != nil {
			t.Errorf("%s: single candidate refused: %+v", i18n.RU, p)
		}
	}
	seen := map[string]string{}
	for lang, m := range texts {
		for k, s := range m {
			if s == "" || strings.Contains(s, "(") && strings.Contains(s, "wiz.") {
				t.Errorf("%s/%s: empty or raw key %q", lang, k, s)
			}
			if o, dup := seen[s]; dup {
				t.Errorf("%s/%s duplicates %s: %q", lang, k, o, s)
			}
			seen[s] = lang + "/" + k
		}
	}
	for _, key := range []string{model.MsgSQLAutoPickCountersOK} {
		ru := i18n.Message(key)
		if ru == "" || strings.Contains(ru, key) {
			t.Errorf("%s: RU %q", key, ru)
		}
	}
}

// TestCHK_TRAY_83_NotAutoAppliedWithoutAutoApply — only an autoPick with
// AutoApply is applied: a non-1C table below the threshold (low_score) and
// a 1C 8.1 counters plan with 0 points (incomplete) are NOT applied, through
// the wizard and the expert dialog alike: a problem with its own RU/EN
// text, the best candidate preselected, no mapping put into the profile.
func TestCHK_TRAY_83_NotAutoAppliedWithoutAutoApply(t *testing.T) {
	cases := []struct {
		name string
		st   *store81
		code string
		key  string
	}{
		{"generic", newStore81().gen("ExchangeNodeLog", 4), traysettings.ProblemLowScore, i18n.KeyWizErrLowScore},
		{"counters 0 points", newStore81("_Node12", 0), traysettings.ProblemIncomplete, i18n.KeyWizErrIncomplete},
	}
	{
		for _, tc := range cases {
			e := newChain(t, tc.st)
			ed := e.editor()
			z := traysettings.NewWizard()
			_, m := e.wizardDiscover(ed, z, false)
			if m.Type != model.MsgDiscovery || len(m.Candidates) == 0 || m.AutoPick == nil || m.AutoPick.AutoApply {
				t.Fatalf("%s/%s: reply %+v", i18n.RU, tc.name, m)
			}
			idx, text := z.DiscoveryDone(m)
			if idx != -1 || z.Problem == nil || z.Problem.Code != tc.code || text != i18n.T(tc.key) {
				t.Errorf("%s/%s: wizard applied or wrong text: %d %+v %q", i18n.RU, tc.name, idx, z.Problem, text)
			}
			if z.Best != m.AutoPick.Index || z.Best < 0 || ed.MapEdit != nil || z.Chosen != -1 {
				t.Errorf("%s/%s: best %d (autoPick %d) map %+v chosen %d", i18n.RU, tc.name, z.Best, m.AutoPick.Index, ed.MapEdit, z.Chosen)
			}
			if strings.Contains(text, fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlan), "")) {
				t.Errorf("%s/%s: «Найдено» shown for an unapplied result", i18n.RU, tc.name)
			}
			// expert: same decision, best preselected, nothing applied
			d := traysettings.NewDiscovery(model.Mapping{})
			d.DiscoveryDone(m)
			if d.Problem == nil || d.Problem.Code != tc.code || d.Sel != z.Best {
				t.Errorf("%s/%s: expert problem %+v sel %d", i18n.RU, tc.name, d.Problem, d.Sel)
			}
			if _, _, pr := traysettings.ResolveDiscovery(m.Candidates); pr == nil || pr.Code != tc.code {
				t.Errorf("%s/%s: ResolveDiscovery %+v", i18n.RU, tc.name, pr)
			}
		}
	}
}
