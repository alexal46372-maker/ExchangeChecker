package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// TestCHK_SQL_04_ServiceSQLKeysInModel — G1/G4 on the service side: every
// sql.* key the service can put into errorKey (sqlcheck.Error.Key of a
// poll or of test_profile) and into ProfileSnapshot.warnings has its RU/EN
// text in model (model.SQLMessage, SQLMessageKeys); every
// validation.sql_* reason (errorArgs[1] of the G3 check) is formatted by
// model.FormatMessage.
func TestCHK_SQL_04_ServiceSQLKeysInModel(t *testing.T) {
	known := map[string]bool{}
	for _, k := range model.SQLMessageKeys() {
		known[k] = true
	}
	for _, k := range []string{sqlcheck.MsgTLSEnableTLS10, sqlcheck.MsgTLSLegacyFailed, sqlcheck.MsgTLSCertificate, sqlcheck.MsgWarnPlaintextLogin} {
		ru, en := model.SQLMessage(k, model.LangRU), model.SQLMessage(k, model.LangEN)
		if !known[k] || !KnownErrorKey(k) || ru == "" || en == "" || ru == en {
			t.Errorf("%s: known %v, ru %q, en %q", k, known[k], ru, en)
		}
	}
	for _, k := range model.SQLValidationKeys() {
		if !strings.HasPrefix(k, "validation.sql_") {
			t.Errorf("validation key %q", k)
		}
		ru, en := model.FormatMessage(k, nil, model.LangRU), model.FormatMessage(k, nil, model.LangEN)
		if ru == "" || en == "" || ru == k || en == k {
			t.Errorf("%s: ru %q en %q", k, ru, en)
		}
	}
	for _, k := range model.MonitorMessageKeys() {
		if !KnownErrorKey(k) {
			t.Errorf("monitor key %s not known", k)
		}
	}
	if KnownErrorKey("") || KnownErrorKey("sql.no_such_key") {
		t.Error("unknown keys reported as known")
	}
}

// TestCHK_SQL_04_ServiceErrorKeys — through the monitor: a poll error
// with a known sql.* key keeps it in errorKey, an unknown key falls back
// to the monitor.* key of the error kind; warnings come from
// model.SQLWarnings (SQL authentication without encryption); test_profile
// keeps a known sql.* key and drops an unknown one.
func TestCHK_SQL_04_ServiceErrorKeys(t *testing.T) {
	f := newFactory()
	f.set("tls", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("TLS"), Key: model.MsgSQLTLSCertificate}})
	f.set("odd", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("x"), Key: "sql.no_such_key"}})
	plain := sqlProf("plain")
	plain.Encrypt = model.EncryptDisable
	f.set("plain", behavior{points: lagPoints(map[string]int{"A": 1})})
	m := New(baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("tls"), prof("odd"), plain}}), &fakeClock{t: t0}))
	m.reloadConfig(true)
	m.pollAll(context.Background())
	s := m.Snapshot()
	if p := profileByID(s, "tls"); p.ErrorKey != model.MsgSQLTLSCertificate {
		t.Errorf("tls: %q", p.ErrorKey)
	}
	if p := profileByID(s, "odd"); p.ErrorKey != model.MsgMonitorConnError {
		t.Errorf("unknown sql key: %q", p.ErrorKey)
	}
	if p := profileByID(s, "plain"); len(p.Warnings) != 1 || p.Warnings[0] != model.MsgSQLWarnPlaintextLogin {
		t.Errorf("warnings %v", p.Warnings)
	}
	if p := profileByID(s, "tls"); len(p.Warnings) != 0 {
		t.Errorf("windows auth warnings %v", p.Warnings)
	}

	e := newTestEnv(t, []model.Profile{sqlProf("db1")}, -1)
	for key, want := range map[string]string{
		model.MsgSQLTLSEnableTLS10: model.MsgSQLTLSEnableTLS10,
		"sql.no_such_key":          "",
	} {
		k := key
		e.ft.mu.Lock()
		e.ft.err = func(string) error {
			return &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "connect", Err: errors.New("handshake"), Key: k}
		}
		e.ft.mu.Unlock()
		r, err := e.run(model.IPCRequest{ProfileID: "db1"})
		if err != nil || r.OK {
			t.Fatalf("%s: %+v %v", key, r, err)
		}
		if want != "" && r.ErrorKey != want {
			t.Errorf("%s: errorKey %q", key, r.ErrorKey)
		}
		if want == "" && (r.ErrorKey == key || !KnownErrorKey(r.ErrorKey)) {
			t.Errorf("unknown key passed through: %q", r.ErrorKey)
		}
	}
}
