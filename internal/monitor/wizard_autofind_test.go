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

// store81 is a 1C 8.1 database: _NodeNNN tables with _IDRRef, _Code,
// _Description, _SentNo, _ReceivedNo; points per plan.
type store81 struct {
	plans map[string]int // table -> number of points
	order []string
}

func newStore81(plans ...interface{}) *store81 {
	s := &store81{plans: map[string]int{}}
	for i := 0; i+1 < len(plans); i += 2 {
		t := plans[i].(string)
		s.plans[t] = plans[i+1].(int)
		s.order = append(s.order, t)
	}
	return s
}

func (s *store81) Ping(context.Context) error { return nil }
func (s *store81) Columns(context.Context) ([]sqlcheck.Column, error) {
	var out []sqlcheck.Column
	for _, t := range s.order {
		ref := sqlcheck.TableRef{Schema: "dbo", Name: t}
		out = append(out,
			sqlcheck.Column{Table: ref, Name: "_IDRRef", DataType: "binary", MaxLength: 16, Ordinal: 1},
			sqlcheck.Column{Table: ref, Name: "_Code", DataType: "nvarchar", MaxLength: 9, Ordinal: 2},
			sqlcheck.Column{Table: ref, Name: "_Description", DataType: "nvarchar", MaxLength: 50, Ordinal: 3},
			sqlcheck.Column{Table: ref, Name: "_SentNo", DataType: "numeric", Ordinal: 4},
			sqlcheck.Column{Table: ref, Name: "_ReceivedNo", DataType: "numeric", Ordinal: 5})
	}
	// a distractor: no exchange plan
	ref := sqlcheck.TableRef{Schema: "dbo", Name: "_Reference7"}
	out = append(out, sqlcheck.Column{Table: ref, Name: "_IDRRef", DataType: "binary", MaxLength: 16, Ordinal: 1},
		sqlcheck.Column{Table: ref, Name: "_Description", DataType: "nvarchar", MaxLength: 50, Ordinal: 2})
	return out, nil
}
func (s *store81) RowCounts(context.Context) (map[sqlcheck.TableRef]int64, error) {
	out := map[sqlcheck.TableRef]int64{}
	for t, n := range s.plans {
		out[sqlcheck.TableRef{Schema: "dbo", Name: t}] = int64(n)
	}
	return out, nil
}
func (s *store81) SampleColumns(context.Context, sqlcheck.TableRef, []string, int) ([][]interface{}, error) {
	return nil, nil
}
func (s *store81) LinkStats(context.Context, sqlcheck.LinkQuery) (sqlcheck.LinkStats, error) {
	return sqlcheck.LinkStats{}, nil
}
func (s *store81) SingleStats(context.Context, sqlcheck.SingleQuery) (sqlcheck.SingleStats, error) {
	return sqlcheck.SingleStats{}, nil
}
func (s *store81) Points(context.Context, model.Mapping, int) ([]sqlcheck.RawPoint, error) {
	return nil, nil
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
		key := make([]byte, 16)
		key[15] = byte(i + 1)
		out = append(out, sqlcheck.RawCounter{Key: key, Name: fmt.Sprintf("%s point %d", m.Table, i+1), ID: fmt.Sprint(i + 1),
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

func (e *chainEnv) editor(lang string) *traysettings.Editor {
	e.t.Helper()
	ed := traysettings.NewEditor(lang, traysettings.Admin)
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
	for _, lang := range []string{i18n.RU, i18n.EN} {
		ed := e.editor(lang)
		z := traysettings.NewWizard(lang)
		tm, m := e.wizardDiscover(ed, z, false)
		if tm.Type != "" {
			t.Fatalf("%s: unexpected test_profile %+v", lang, tm)
		}
		if m.Type != model.MsgDiscovery || m.AutoPick == nil || len(m.AutoPick.Plans) != 3 || !m.AutoPick.AutoApply ||
			m.AutoPick.ReasonKey != model.MsgSQLAutoPickCountersOK {
			t.Fatalf("%s: discover reply %+v", lang, m)
		}
		idx, text := z.DiscoveryDone(m)
		if idx < 0 || z.Problem != nil {
			t.Fatalf("%s: not found by itself: %d %q", lang, idx, text)
		}
		// no manual step: the wizard's own choice goes into the profile
		if !z.Choose(ed, idx) || ed.MapEdit == nil {
			t.Fatalf("%s: choose", lang)
		}
		want := []string{"dbo._node12", "dbo._node34", "dbo._node56"}
		if got := plansOf(*ed.MapEdit, ed.MapsEdit); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: applied plans %v, want %v", lang, got, want)
		}
		found := fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlan, lang), "_Node12") + fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlans, lang), 2)
		if !strings.Contains(text, found) || !strings.Contains(text, "_SentNo") ||
			!strings.Contains(text, i18n.Message(model.MsgSQLAutoPickCountersOK, lang)) {
			t.Errorf("%s: result text %q", lang, text)
		}
		sreq, pr := ed.SaveRequest("pw")
		if pr != nil {
			t.Fatalf("%s: save request %+v", lang, pr)
		}
		if r := e.do(sreq); r.Type == model.MsgError {
			t.Fatalf("%s: save_profile %+v", lang, r)
		}
		cfg := e.do(model.IPCRequest{Cmd: model.CmdGetConfig})
		var saved *model.Profile
		for i := range cfg.Config.Profiles {
			if cfg.Config.Profiles[i].ID == sreq.Profile.ID || (sreq.Profile.ID == "" && cfg.Config.Profiles[i].Name == sreq.Profile.Name) {
				saved = &cfg.Config.Profiles[i]
			}
		}
		if saved == nil {
			t.Fatalf("%s: saved profile missing: %+v", lang, cfg.Config.Profiles)
		}
		if got := plansOf(saved.Mapping, saved.Mappings); len(saved.Mappings) != 2 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: saved plans %v (mappings %d), want %v", lang, got, len(saved.Mappings), want)
		}
		for _, x := range append([]model.Mapping{saved.Mapping}, saved.Mappings...) {
			if x.Mode != model.MappingCounters || x.SentNoColumn != "_SentNo" || x.ReceivedNoColumn != "_ReceivedNo" || x.PointNameColumn != "_Description" {
				t.Errorf("%s: saved mapping %+v", lang, x)
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
	for _, st := range []*store81{newStore81("_Node12", 5, "_Node34", 3, "_Node56", 2), newStore81("_Node7", 1), newStore81("_Node40", 2, "_Node41", 9)} {
		e := newChain(t, st)
		ed := e.editor(i18n.RU)
		z := traysettings.NewWizard(i18n.RU)
		_, m := e.wizardDiscover(ed, z, false)
		idx, text := z.DiscoveryDone(m)
		if idx < 0 || !z.Choose(ed, idx) {
			t.Fatalf("%v: wizard %d %q", st.order, idx, text)
		}
		d := traysettings.NewDiscovery(i18n.RU, model.Mapping{})
		d.DiscoveryDone(m)
		main, _ := d.Result()
		expert := plansOf(main, d.PlanMappings(main))
		wizard := plansOf(*ed.MapEdit, ed.MapsEdit)
		if strings.Join(expert, ",") != strings.Join(wizard, ",") || d.Sel != idx {
			t.Errorf("%v: expert %v (sel %d) != wizard %v (%d)", st.order, expert, d.Sel, wizard, idx)
		}
		rm, rp, pr := traysettings.ResolveDiscovery(m.Candidates, i18n.RU)
		if pr != nil || rm != *ed.MapEdit || len(rp) != len(ed.MapsEdit) {
			t.Errorf("%v: ResolveDiscovery %+v %d %+v", st.order, rm, len(rp), pr)
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
	for _, lang := range []string{i18n.RU, i18n.EN} {
		texts[lang] = map[string]string{}
		// none: a database without exchange plans, through the chain
		e := newChain(t, newStore81())
		ed := e.editor(lang)
		z := traysettings.NewWizard(lang)
		_, m := e.wizardDiscover(ed, z, false)
		if idx, txt := z.DiscoveryDone(m); idx != -1 || z.Problem == nil || z.Problem.Code != traysettings.ProblemNone || txt != z.Problem.Text {
			t.Fatalf("%s: none %d %q", lang, idx, txt)
		}
		if !strings.Contains(z.Problem.Text, i18n.T(i18n.KeyWizGrant, lang)) {
			t.Errorf("%s: none does not offer the permissions script: %q", lang, z.Problem.Text)
		}
		texts[lang]["none"] = z.Problem.Text
		// truncated: nothing found and the search was cut short
		m.Truncated, m.TruncatedReason = true, model.TruncatedBudget
		if idx, _ := z.DiscoveryDone(m); idx != -1 || z.Problem == nil || z.Problem.Code != traysettings.ProblemTruncated {
			t.Fatalf("%s: truncated %+v", lang, z.Problem)
		}
		texts[lang]["truncated"] = z.Problem.Text
		// windows_unsaved: the real test_profile of a new Windows profile
		e2 := newChain(t, newStore81("_Node12", 5))
		ed2 := e2.editor(lang)
		z2 := traysettings.NewWizard(lang)
		tm, _ := e2.wizardDiscover(ed2, z2, true)
		pr := z2.TestProblem(tm)
		if pr == nil || pr.Code != traysettings.ProblemWindowsUnsaved {
			t.Fatalf("%s: windows_unsaved from %+v", lang, tm)
		}
		texts[lang]["windows_unsaved"] = pr.Text
		// ambiguous: two equally good candidates for the same table
		a := model.DiscoveryCandidate{Mode: model.CandidateSingle, Schema: "dbo", Table: "ExchLog", PointCol: "Node", SendCol: "Sent", RecvCol: "Recv", Score: 150, Points: 4}
		b := a
		b.PointCol = "NodeCode"
		_, _, apr := traysettings.ResolveDiscovery([]model.DiscoveryCandidate{a, b}, lang)
		if apr == nil || apr.Code != traysettings.ProblemAmbiguous {
			t.Fatalf("%s: ambiguous %+v", lang, apr)
		}
		if !strings.Contains(apr.Text, "(Node, Sent, Recv)") || !strings.Contains(apr.Text, "(NodeCode, Sent, Recv)") {
			t.Errorf("%s: ambiguous must name both candidates: %q", lang, apr.Text)
		}
		texts[lang]["ambiguous"] = apr.Text
		// a single good candidate is no error
		if _, _, p := traysettings.ResolveDiscovery([]model.DiscoveryCandidate{a}, lang); p != nil {
			t.Errorf("%s: single candidate refused: %+v", lang, p)
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
			if lang == i18n.RU && s == texts[i18n.EN][k] {
				t.Errorf("%s: RU == EN", k)
			}
		}
	}
	for _, key := range []string{model.MsgSQLAutoPickCountersOK} {
		ru, en := i18n.Message(key, i18n.RU), i18n.Message(key, i18n.EN)
		if ru == "" || en == "" || ru == en || strings.Contains(ru, key) {
			t.Errorf("%s: RU %q EN %q", key, ru, en)
		}
	}
}
