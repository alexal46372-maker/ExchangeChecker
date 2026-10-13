package traysettings

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func label(key, lang string) string { return strings.TrimSuffix(i18n.T(key, lang), ":") }

// TestCHK_TRAY_12_ProfileLimits — thresholds, timeouts and the server
// preset are form fields: round trip, own values saved, values equal to
// the inherited / default ones stored as 0, range and pair validation
// with the focus on the profile's own boxes.
func TestCHK_TRAY_12_ProfileLimits(t *testing.T) {
	base := savedSQL() // own 10 / 20 min, connect 30 s
	f := FromProfile(base)
	if f.WarnMin != "10" || f.ErrorMin != "20" || f.ConnTimeout != "30" || f.QueryTimeout != "30" || f.Preset != base.ServerPreset {
		t.Fatalf("form %+v", f)
	}
	f.WarnMin, f.ErrorMin, f.ConnTimeout, f.QueryTimeout, f.Preset = "45", "90", "20", "120", ""
	p, fe := f.Apply(base)
	if fe != nil || p.WarnMinutes != 45 || p.ErrorMinutes != 90 || p.ConnectTimeoutSec != 20 || p.QueryTimeoutSec != 120 || p.ServerPreset != "" {
		t.Fatalf("apply %+v %v", p, fe)
	}
	// inherited (general 30 / 60) and default timeouts are not overrides
	f.WarnMin, f.ErrorMin, f.ConnTimeout, f.QueryTimeout = "30", "60", "15", ""
	if p, fe = f.Apply(base); fe != nil || p.WarnMinutes != 0 || p.ErrorMinutes != 0 || p.ConnectTimeoutSec != 0 || p.QueryTimeoutSec != 0 {
		t.Fatalf("inherited %+v %v", p, fe)
	}
	bad := []struct {
		mod   func(*Form)
		key   string
		focus Field
	}{
		{func(f *Form) { f.WarnMin = "0" }, model.ValRange, FieldProfWarn},
		{func(f *Form) { f.ErrorMin = "x" }, model.ValRange, FieldProfError},
		{func(f *Form) { f.WarnMin, f.ErrorMin = "50", "40" }, model.ValGreaterThan, FieldProfError},
		{func(f *Form) { f.WarnMin = "70" }, model.ValGreaterThan, FieldProfError}, // above the inherited error 60
		{func(f *Form) { f.ConnTimeout = "601" }, model.ValRange, FieldConnTimeout},
		{func(f *Form) { f.QueryTimeout = "0" }, model.ValRange, FieldQueryTimeout},
		{func(f *Form) { f.Encrypt = model.EncryptStrict; f.Preset = model.ServerPresetMSSQL2008 }, model.ValPresetConflict, FieldEncrypt},
	}
	for i, c := range bad {
		g := FromProfile(base)
		g.WarnMin, g.ErrorMin = "30", "60"
		c.mod(&g)
		_, fe := g.Apply(base)
		if fe == nil || fe.Key != c.key || FocusProfileField(fe.Field, fe.Args) != c.focus {
			t.Errorf("case %d: %+v", i, fe)
		}
	}
	// the editor shows the inherited general thresholds and follows them
	e := NewEditor(i18n.RU, Admin)
	np := model.NewProfile("p2")
	np.Server, np.Database, np.Login = "h", "d", "l"
	e.LoadConfig(model.ConfigView{Settings: model.Settings{PollIntervalSec: 60, WarnMinutes: 40, ErrorMinutes: 80}, Profiles: []model.Profile{np}}, "r")
	if e.Form.WarnMin != "40" || e.Form.ErrorMin != "80" || e.ProfileUnsaved() {
		t.Fatalf("inherited form %+v", e.Form)
	}
	e.LoadSettings(model.Settings{PollIntervalSec: 60, WarnMinutes: 35, ErrorMinutes: 70})
	if e.Form.WarnMin != "35" || e.ProfileUnsaved() {
		t.Errorf("after settings save %+v", e.Form)
	}
	e.Form.WarnMin = "10"
	if _, pr := e.SaveRequest(""); pr != nil && (pr.Field == FieldProfWarn || pr.Field == FieldProfError) {
		t.Errorf("save %+v", pr) // only the missing password may be reported
	}
	e.Form.WarnMin = "100"
	if _, pr := e.SaveRequest(""); pr == nil || pr.Field != FieldProfError {
		t.Errorf("pair problem %+v", pr)
	}
}

// TestCHK_TRAY_17_Defaults — a new profile shows the default values at
// once: port empty (cue banner in the window), thresholds 30 / 60,
// timeouts of model (15 / 30 s), preset SQL Server 2008.
func TestCHK_TRAY_17_Defaults(t *testing.T) {
	e := adminEditor(savedSQL())
	e.New()
	f := e.Form
	if f.Port != "" || f.WarnMin != "30" || f.ErrorMin != "60" || f.ConnTimeout != "15" || f.QueryTimeout != "30" ||
		f.Preset != model.ServerPresetMSSQL2008 {
		t.Fatalf("new form %+v", f)
	}
	f.Host, f.Database, f.Login = "h", "d", "l"
	p, fe := f.Apply(e.Base)
	if fe != nil || p.Port != 0 || p.WarnMinutes != 0 || p.ErrorMinutes != 0 || p.ConnectTimeoutSec != 0 || p.QueryTimeoutSec != 0 {
		t.Fatalf("defaults stored as overrides: %+v %v", p, fe)
	}
	if model.DefaultConnectTimeoutSec != 15 || model.DefaultQueryTimeoutSec != 30 || model.DefaultWarnMinutes != 30 || model.DefaultErrorMinutes != 60 {
		t.Error("defaults of model changed: update the texts / tests")
	}
	if i18n.T(i18n.KeySetPortCue, i18n.RU) != "1433 или пусто для SQL Browser" {
		t.Errorf("port cue %q", i18n.T(i18n.KeySetPortCue, i18n.RU))
	}
}

// TestCHK_TRAY_13_TrustOnlyWithOn — «Доверять сертификату» is stored only
// with «Весь сеанс (TLS)»: optional never validates, strict always does.
func TestCHK_TRAY_13_TrustOnlyWithOn(t *testing.T) {
	base := savedSQL()
	base.ServerPreset = ""
	for _, c := range []struct {
		enc   model.EncryptMode
		trust bool
	}{{model.EncryptOn, true}, {model.EncryptOptional, false}, {model.EncryptStrict, false}, {model.EncryptDisable, false}} {
		f := FromProfile(base)
		f.Encrypt, f.TrustServerCert = c.enc, true
		p, fe := f.Apply(base)
		if fe != nil || p.TrustServerCert != c.trust || TrustApplies(c.enc) != c.trust {
			t.Errorf("%s: trust %v %v", c.enc, p.TrustServerCert, fe)
		}
		// the stored value matches what the driver does
		if model.VerifiesServerCert(p) != (c.enc == model.EncryptStrict) {
			t.Errorf("%s: verifies %v", c.enc, model.VerifiesServerCert(p))
		}
	}
}

// TestCHK_TRAY_14_ErrorTexts — validation texts name the window's labels,
// never JSON names; the TLS texts of the service name existing controls.
func TestCHK_TRAY_14_ErrorTexts(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		e := adminEditor(savedSQL())
		e.Lang = lang
		e.Form.WarnMin, e.Form.ErrorMin = "50", "40"
		_, pr := e.SaveRequest("")
		if pr == nil || strings.Contains(pr.Text, "warnMinutes") || strings.Contains(pr.Text, "errorMinutes") ||
			!strings.Contains(pr.Text, label(i18n.KeySetWarnMin, lang)) || !strings.Contains(pr.Text, label(i18n.KeySetErrorMin, lang)) {
			t.Errorf("%s: pair text %+v", lang, pr)
		}
		// the same reply of the service (general page) uses the general labels
		e.settingsCtx = true
		if txt := e.validationText([]string{"errorMinutes", model.ValGreaterThan, "warnMinutes", "30"}); strings.Contains(txt, "warnMinutes") ||
			!strings.Contains(txt, label(i18n.KeyGenWarn, lang)) {
			t.Errorf("%s: general text %q", lang, txt)
		}
		e.settingsCtx = false
		txt := e.validationText([]string{"encrypt", model.ValPresetConflict, "strict", "mssql2008"})
		if strings.Contains(txt, "mssql2008") || !strings.Contains(txt, i18n.T(i18n.KeySetPreset2008, lang)) || !strings.Contains(txt, i18n.T(i18n.KeySetEncStrict, lang)) {
			t.Errorf("%s: preset text %q", lang, txt)
		}
		if txt := e.validationText([]string{"connectTimeoutSec", model.ValRange, "1", "600", "0"}); !strings.Contains(txt, label(i18n.KeySetConnTimeout, lang)) {
			t.Errorf("%s: timeout text %q", lang, txt)
		}
		// TLS messages name real labels of the window
		want := map[string][]string{
			model.MsgSQLTLSEnableTLS10:  {i18n.T(i18n.KeySetTLS10, lang)},
			model.MsgSQLTLSLegacyFailed: {label(i18n.KeySetEncrypt, lang), i18n.T(i18n.KeySetEncDisable, lang)},
			model.MsgSQLTLSCertificate: {i18n.T(i18n.KeySetTrust, lang), i18n.T(i18n.KeySetEncOn, lang),
				i18n.T(i18n.KeySetEncOptional, lang)},
		}
		for k, labels := range want {
			msg := model.SQLMessage(k, lang)
			for _, l := range labels {
				if !strings.Contains(msg, l) {
					t.Errorf("%s %s: %q does not name %q", lang, k, msg, l)
				}
			}
		}
	}
}
