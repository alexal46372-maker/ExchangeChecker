package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// Phase 6 G1/G4: SQL texts, warnings, templates, sample limits and field
// errors in package model (EC Service sends them over IPC without the
// driver). The sqlcheck side is covered by its g1g4_test.go.

func TestCHK_SQL_04_SQLMessagesModel(t *testing.T) {
	keys := append(SQLMessageKeys(), SQLValidationKeys()...)
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key %s", k)
		}
		seen[k] = true
		if !strings.HasPrefix(k, "sql.") && !strings.HasPrefix(k, "validation.sql_") {
			t.Errorf("key %s", k)
		}
		if MonitorMessage(k, LangEN) != "" {
			t.Errorf("%s must not be a monitor message (tray i18n)", k)
		}
		for _, lang := range []string{LangRU, LangEN} {
			if SQLMessage(k, lang) == "" {
				t.Errorf("%s/%s empty", k, lang)
			}
		}
		if strings.HasPrefix(k, "validation.") && !strings.Contains(SQLMessage(k, LangRU), "{0}") {
			t.Errorf("%s: no {0} (field)", k)
		}
	}
	if len(SQLMessages()) != len(keys) {
		t.Errorf("SQLMessages %d, keys %d", len(SQLMessages()), len(keys))
	}
	if SQLMessage("sql.unknown", LangEN) != "" || SQLMessage(ValRequired, LangEN) != "" {
		t.Error("unknown key")
	}
	if SQLMessage(MsgSQLTLSCertificate, "de") != SQLMessage(MsgSQLTLSCertificate, LangRU) {
		t.Error("unknown lang -> RU (as MonitorMessage)")
	}
	lt := LocalizedText{RU: "р", EN: "e"}
	if lt.In(LangRU) != "р" || lt.In(LangEN) != "e" || lt.In("") != "р" {
		t.Error("LocalizedText.In")
	}
	// FormatMessage: monitor keys unchanged, SQL keys through the fallback
	if FormatMessage(ValRequired, []string{"server", ValRequired}, LangEN) != MonitorMessage(ValRequired, LangEN) &&
		!strings.Contains(FormatMessage(ValRequired, []string{"server", ValRequired}, LangEN), "server") {
		t.Error("monitor key")
	}
	got := FormatMessage(ValSQLColumnType, []string{"mapping.lastSendColumn", ValSQLColumnType, "dbo.t", "c", "int", SQLTypesDateTime}, LangRU)
	for _, want := range []string{"mapping.lastSendColumn", "dbo.t", "int", SQLTypesDateTime} {
		if !strings.Contains(got, want) || strings.Contains(got, "{") {
			t.Errorf("format %q lacks %s", got, want)
		}
	}
	if FormatMessage(MsgSQLTLSEnableTLS10, nil, LangRU) != SQLMessage(MsgSQLTLSEnableTLS10, LangRU) {
		t.Error("sql.* through FormatMessage")
	}
}

func TestCHK_SQL_01_SQLWarningsModel(t *testing.T) {
	p := sqlTestProfile()
	for _, c := range []struct {
		auth AuthMode
		enc  EncryptMode
		n    int
	}{
		{AuthSQL, EncryptDefault, 1}, {AuthSQL, EncryptDisable, 1}, {AuthSQL, EncryptOptional, 0},
		{AuthSQL, EncryptOn, 0}, {AuthWindows, EncryptDisable, 0},
	} {
		p.AuthMode, p.Encrypt = c.auth, c.enc
		w := SQLWarnings(p)
		if len(w) != c.n || (c.n == 1 && w[0] != MsgSQLWarnPlaintextLogin) {
			t.Errorf("%s/%s: %v", c.auth, c.enc, w)
		}
	}
}

func TestCHK_SQL_04_SQLManualTemplatesModel(t *testing.T) {
	tpls := SQLManualTemplates()
	ids := map[string]bool{}
	for _, tp := range tpls {
		if ids[tp.ID] || tp.ID == "" || tp.Title.RU == "" || tp.Title.EN == "" || tp.Description.RU == "" || tp.Description.EN == "" ||
			!(strings.HasPrefix(tp.SQL, "SELECT ") || strings.HasPrefix(tp.SQL, "WITH ")) || strings.Contains(tp.SQL, ";") {
			t.Errorf("template %+v", tp)
		}
		ids[tp.ID] = true
	}
	if len(tpls) != 4 {
		t.Errorf("%d templates", len(tpls))
	}
}

func TestCHK_SQL_02_TruncateSampleModel(t *testing.T) {
	long := strings.Repeat("я", MaxSampleCellRunes+10)
	rows := []SampleRow{{Point: long}, {Point: "ok"}, {Point: strings.Repeat("a", MaxSampleCellRunes)}}
	TruncateSampleRows(rows)
	if !rows[0].PointTruncated || utf8.RuneCountInString(rows[0].Point) != MaxSampleCellRunes || len(rows[0].Point) != 2*MaxSampleCellRunes ||
		rows[1].PointTruncated || rows[2].PointTruncated {
		t.Errorf("rows %+v", rows[1:])
	}
	TruncateSampleRows(rows) // idempotent: the flag stays
	if !rows[0].PointTruncated {
		t.Error("flag lost")
	}
	TruncateSampleRows(nil)
	TruncateCandidates(nil)
	if s, cut := TruncateSampleCell(""); s != "" || cut {
		t.Error("empty")
	}
	// worst case of one IPC sample: MaxSampleRows x 256 runes x 4 bytes
	if MaxSampleRows*MaxSampleCellRunes*utf8.UTFMax > 128<<10 {
		t.Errorf("sample worst case %d bytes", MaxSampleRows*MaxSampleCellRunes*utf8.UTFMax)
	}
}

type multiErr []error

func (m multiErr) Error() string   { return "multi" }
func (m multiErr) Unwrap() []error { return m }

func TestCHK_SQL_04_FieldErrorsModel(t *testing.T) {
	a := fieldErr("table", ValSQLTableNotFound, "table x not found", "x")
	b := fieldErr("lastSendColumn", ValSQLColumnNotFound, "column", "dbo.t", "c")
	err := fmt.Errorf("check: %w", multiErr{a, errors.New("plain"), fmt.Errorf("wrap: %w", b)})
	fes := FieldErrors(err)
	if len(fes) != 2 || fes[0] != a || fes[1] != b {
		t.Errorf("FieldErrors %v", fes)
	}
	if args := FieldErrorArgs(err, "mapping."); strings.Join(args, "|") != "mapping.table|"+ValSQLTableNotFound+"|x" {
		t.Errorf("FieldErrorArgs %v", args)
	}
	if FieldErrorArgs(errors.New("x"), "") != nil || FieldErrors(nil) != nil {
		t.Error("no field error")
	}
}
