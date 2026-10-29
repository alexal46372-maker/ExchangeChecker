package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// CHK-SQL-06: browse request validation and limits.
func TestCHK_SQL_06_BrowseRequest(t *testing.T) {
	ok := []SQLBrowseRequest{{}, {Host: "srv-1c.corp.local"}, {Host: "10.0.0.5", Instance: "SQL2008"}, {TimeoutMs: 500}}
	for _, r := range ok {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := map[string]SQLBrowseRequest{
		ValSQLBrowseHost:   {Host: `\\evil\pipe\sql\query`},
		ValRequired:        {Instance: "X"},
		ValSQLInstanceName: {Host: "h", Instance: "1abc"},
		ValRange:           {TimeoutMs: 50},
	}
	for key, r := range bad {
		var fe *FieldError
		if err := r.Validate(); !errors.As(err, &fe) || fe.Key != key {
			t.Errorf("%+v: %v, want %s", r, err, key)
		}
	}
	for _, h := range []string{"np:host", "lpc:host", "admin:host", `\host`, "host\\inst", "a b", "-x", ""} {
		if ValidSQLBrowseHost(h) {
			t.Errorf("host %q accepted", h)
		}
	}
	if (SQLServerInstance{Host: "H", Instance: "MSSQLSERVER"}).Server() != "H" || (SQLServerInstance{Host: "H", Instance: "I"}).Server() != `H\I` {
		t.Error("Server()")
	}
	// The worst-case response stays well under the 64 KiB IPC frame? Values
	// are capped at 255 bytes; 256 instances of realistic size must fit.
	res := SQLBrowseResult{Truncated: true}
	for i := 0; i < MaxSQLBrowseInstances; i++ {
		res.Instances = append(res.Instances, SQLServerInstance{Host: strings.Repeat("h", 15), Address: "255.255.255.255", Instance: strings.Repeat("i", 16), Version: "10.50.6000.34", TCPPort: 65535, NamedPipe: `\\HHHHHHHHHHHHHHH\pipe\MSSQL$IIIIIIIIIIIIIIII\sql\query`})
	}
	res.Instances, _ = CapSQLList(res.Instances, MaxSQLBrowseInstances)
	b, _ := json.Marshal(res)
	if len(b) > 60*1024 {
		t.Errorf("browse JSON %d bytes", len(b))
	}
	db := SQLDatabasesResult{}
	for i := 0; i < MaxSQLDatabases; i++ {
		db.Databases = append(db.Databases, SQLDatabaseInfo{Name: strings.Repeat("я", 128), CompatLevel: 100, State: "ONLINE"})
	}
	db.Databases, db.Truncated = CapSQLList(db.Databases, MaxSQLDatabases)
	if !db.Truncated || len(db.Databases) == 0 {
		t.Error("db cap")
	}
	if l, tr := CapSQLList(make([]int, 10), 3); len(l) != 3 || !tr {
		t.Error("count cap")
	}
	if l, tr := CapSQLList(make([]int, 10), 0); len(l) != 10 || tr {
		t.Error("no cap")
	}
	if b, _ = json.Marshal(db); len(b) > 60*1024 {
		t.Errorf("databases JSON %d bytes", len(b))
	}
}

func cand(score int, plan string) DiscoveryCandidate {
	return DiscoveryCandidate{Mode: CandidateSingle, Table: "_T" + plan, SendCol: "s", RecvCol: "r", Points: 3, Score: score, PlanTable: plan}
}

// CHK-SQL-08: auto-pick only for one confident candidate.
func TestCHK_SQL_08_AutoPick(t *testing.T) {
	cases := []struct {
		name  string
		cands []DiscoveryCandidate
		key   string
	}{
		{"none", nil, MsgSQLAutoPickNone},
		{"one", []DiscoveryCandidate{cand(148, "_Node1")}, MsgSQLAutoPickOK},
		{"margin", []DiscoveryCandidate{cand(102, "_Node1"), cand(148, "_Node1")}, MsgSQLAutoPickOK},
		{"plans", []DiscoveryCandidate{cand(155, "_Node1"), cand(148, "_Node2")}, MsgSQLAutoPickOK}, // CHK-SQL-16: merged
		{"low", []DiscoveryCandidate{cand(117, ""), cand(50, "")}, MsgSQLAutoPickLowScore},
		{"ambiguous", []DiscoveryCandidate{cand(140, ""), cand(120, "")}, MsgSQLAutoPickAmbiguous},
		{"tie", []DiscoveryCandidate{cand(150, ""), cand(150, "")}, MsgSQLAutoPickAmbiguous},
	}
	for _, c := range cases {
		p := SQLAutoPickFor(c.cands)
		if p.ReasonKey != c.key || p.AutoApply != (c.key == MsgSQLAutoPickOK) {
			t.Errorf("%s: %+v", c.name, p)
		}
	}
	if p := SQLAutoPickFor([]DiscoveryCandidate{cand(102, "_N"), cand(148, "_N")}); p.Index != 1 || p.Margin != 46 || p.CandidateKey == "" {
		t.Errorf("%+v", p)
	}
	rk := SQLAutoPickFor([]DiscoveryCandidate{cand(102, "_N"), cand(148, "_N"), cand(102, "_N")})
	if len(rk.Ranks) != 3 || rk.Ranks[1].Rank != 1 || rk.Ranks[0].Rank != 2 || rk.Ranks[2].Rank != 3 || rk.AutoPickMin != 0.6 ||
		rk.Confidence != 0.74 || rk.Ranks[0].Confidence != 0.51 {
		t.Errorf("ranks: %+v", rk)
	}
	if r := rk.Ranks[0].Reasons; len(r) != 3 || r[0] != MsgSQLCandExchangePlan || r[1] != MsgSQLCandSendRecv || r[2] != MsgSQLCandLowScore {
		t.Errorf("reasons: %v", r)
	}
	if SQLConfidence(-5) != 0 || SQLConfidence(500) != 1 {
		t.Error("clamp")
	}
	inc := cand(200, "")
	inc.RecvCol = ""
	cnt := cand(200, "")
	cnt.Mode = CandidateCounters
	if SQLAutoPickFor([]DiscoveryCandidate{inc}).ReasonKey != MsgSQLAutoPickIncomplete || SQLAutoPickFor([]DiscoveryCandidate{cnt}).ReasonKey != MsgSQLAutoPickCounters {
		t.Error("incomplete/counters")
	}
}

// CHK-SQL-09: every new key has RU/EN text and is therefore a known error
// key for history (monitor.KnownErrorKey uses model.SQLMessage).
func TestCHK_SQL_09_WizardKeysKnown(t *testing.T) {
	for _, k := range append(SQLWizardMessageKeys(), SQLWizardValidationKeys()...) {
		if SQLMessage(k, LangRU) == "" || SQLMessage(k, LangEN) == "" {
			t.Errorf("%s: no text", k)
		}
	}
	fe := &FieldError{Field: "principal", Key: ValSQLPrincipal, Args: []string{SQLPrincipalControl}}
	if s := FormatMessage(fe.Key, []string{"principal", "", SQLPrincipalControl}, LangEN); !strings.Contains(s, "control_char") {
		t.Errorf("format: %q", s)
	}
}

// CHK-SQL-09: diagnosis texts.
func TestCHK_SQL_09_Diagnose(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range SQLDiagnoses() {
		if seen[d.Key] || len(d.Steps) == 0 || d.Title.RU == "" || d.Title.EN == "" {
			t.Errorf("%+v", d)
		}
		seen[d.Key] = true
		for _, s := range d.Steps {
			if s.RU == "" || s.EN == "" {
				t.Errorf("%s: empty step", d.Key)
			}
		}
	}
	for _, c := range SQLConnCategories() {
		if d := SQLDiagnose(c, ""); d.Category != c || d.Key != "sql.diag_"+c {
			t.Errorf("%s: %+v", c, d.Key)
		}
	}
	if SQLDiagnose(SQLConnTLS, MsgSQLTLSCertificate).Key != "sql.diag_tls_certificate" || SQLDiagnose("weird", "").Key != "sql.diag_other" {
		t.Error("tls/unknown")
	}
	d := SQLDiagnose(SQLConnLogin, "")
	d.Steps[0].RU = "x"
	if SQLDiagnose(SQLConnLogin, "").Steps[0].RU == "x" {
		t.Error("not a copy")
	}
}

const goldenWindowsGrant = `-- Exchange Checker: read-only access (role db_datareader) in one database for a Windows login.
-- SQL Server 2005 / 2008 and newer. Safe to run again. Run as a member of sysadmin
-- (or securityadmin + db_owner of the database).
SET NOCOUNT ON;
DECLARE @login sysname, @db sysname, @password nvarchar(max), @sql nvarchar(max);
SET @login = N'CORP\EC-HOST$';
SET @db = N'buh_2008';
IF DB_ID(@db) IS NULL
BEGIN
    RAISERROR(N'Exchange Checker: database %s not found.', 16, 1, @db);
    RETURN;
END;
IF NOT EXISTS (SELECT 1 FROM sys.server_principals WHERE name = @login)
BEGIN
    SET @sql = N'CREATE LOGIN ' + QUOTENAME(@login) + N' FROM WINDOWS WITH DEFAULT_DATABASE = ' + QUOTENAME(@db) + N';';
    EXEC (@sql);
END;
SET @sql = N'USE ' + QUOTENAME(@db) + N';
IF NOT EXISTS (SELECT 1 FROM sys.database_principals WHERE name = @login)
BEGIN
    DECLARE @create nvarchar(max);
    SET @create = N''CREATE USER '' + QUOTENAME(@login) + N'' FOR LOGIN '' + QUOTENAME(@login) + N'';'';
    EXEC (@create);
END;
EXEC sp_addrolemember N''db_datareader'', @login;';
EXEC sp_executesql @sql, N'@login sysname', @login = @login;
PRINT N'Exchange Checker: access granted (role db_datareader).';
`

// CHK-SQL-09: grant script golden text, validation, injection.
func TestCHK_SQL_09_GrantScript(t *testing.T) {
	s, err := SQLGrantScriptFor(SQLGrantRequest{Principal: `CORP\EC-HOST$`, Windows: true, Database: "buh_2008"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(s.SQL, "\r\n", "\n"); got != goldenWindowsGrant {
		t.Errorf("golden mismatch:\n%s", got)
	}
	if s.PasswordPlaceholder != "" || len(s.Notes) == 0 {
		t.Error("windows notes")
	}
	sq, err := SQLGrantScriptFor(SQLGrantRequest{Principal: "ec_reader", Database: "buh"})
	if err != nil || sq.PasswordPlaceholder != SQLGrantPasswordPlaceholder || strings.Count(sq.SQL, SQLGrantPasswordPlaceholder) < 1 || !strings.Contains(sq.SQL, "CHECK_POLICY = ON") {
		t.Fatalf("%v %q", err, sq.SQL)
	}
	// Password helper (local display only): apostrophes doubled, length.
	filled, err := SQLGrantFillPassword(sq, "pa'ss")
	if err != nil || !strings.Contains(filled, "SET @password = N'pa''ss';") || strings.Contains(filled, SQLGrantPasswordPlaceholder+"';") {
		t.Errorf("fill: %v", err)
	}
	if !strings.Contains(sq.SQL, "LEN(@password) > 128") {
		t.Error("no length guard in the script")
	}
	var pe *FieldError
	if _, err := SQLGrantFillPassword(sq, strings.Repeat("x", 129)); !errors.As(err, &pe) || pe.Key != ValSQLGrantPassword || pe.Args[0] != SQLPrincipalTooLong {
		t.Errorf("long: %v", err)
	}
	if _, err := SQLGrantFillPassword(sq, "a\nb"); !errors.As(err, &pe) || pe.Args[0] != SQLPrincipalControl {
		t.Errorf("control: %v", err)
	}
	if _, err := SQLGrantFillPassword(s, "x"); err == nil {
		t.Error("windows script has no password")
	}
	if ValidateSQLGrantPassword(strings.Repeat("я", 128)) != nil || ValidateSQLGrantPassword("") == nil {
		t.Error("validate")
	}
	// SQL 2008 compatibility of both variants: sqlcheck TestCHK_SQL_09_GrantCompat.
	// Injection: names only appear inside N'' literals with ' doubled.
	inj := []string{"x'; DROP LOGIN sa; --", "a]; DROP DATABASE buh; --", "a'+N'b", "[x]"}
	for _, n := range inj {
		sc, err := SQLGrantScriptFor(SQLGrantRequest{Principal: n, Database: n})
		if err != nil {
			t.Fatalf("%q: %v", n, err)
		}
		if !strings.Contains(sc.SQL, "SET @login = "+sqlNLiteral(n)+";") || strings.Contains(sc.SQL, "DROP") && !strings.Contains(sc.SQL, "N'x''; DROP") && !strings.Contains(sc.SQL, "N'a]; DROP") {
			t.Errorf("%q not quoted:\n%s", n, sc.SQL)
		}
		if strings.Count(sc.SQL, n) != 2 && !strings.Contains(n, "'") {
			t.Errorf("%q appears outside literals", n)
		}
	}
	bad := map[string]SQLGrantRequest{
		SQLPrincipalControl:       {Principal: "a\nb", Database: "d"},
		SQLPrincipalSpace:         {Principal: " a", Database: "d"},
		SQLPrincipalTooLong:       {Principal: strings.Repeat("я", 129), Database: "d"},
		SQLPrincipalBackslash:     {Principal: `D\u`, Database: "d"},
		SQLPrincipalWindowsFormat: {Principal: `a\b\c`, Windows: true, Database: "d"},
		SQLPrincipalEmptyPart:     {Principal: `\u`, Windows: true, Database: "d"},
		SQLPrincipalInvalidChar:   {Principal: `D\u]x`, Windows: true, Database: "d"},
	}
	for reason, r := range bad {
		var fe *FieldError
		if _, err := SQLGrantScriptFor(r); !errors.As(err, &fe) || fe.Key != ValSQLPrincipal || len(fe.Args) != 1 || fe.Args[0] != reason {
			t.Errorf("%s: %v", reason, err)
		}
	}
	var fe *FieldError
	if _, err := SQLGrantScriptFor(SQLGrantRequest{Principal: "a", Database: "d\x00"}); !errors.As(err, &fe) || fe.Field != "database" {
		t.Errorf("database: %v", err)
	}
	// Tables mode: GRANT SELECT per table, names quoted twice.
	ts, err := SQLGrantScriptFor(SQLGrantRequest{Principal: `CORP\u`, Auth: "windows", Database: "d", Tables: []string{"_Node123", "acc.x'y]"}})
	if err != nil || strings.Contains(ts.SQL, "db_datareader'', @login") || !strings.Contains(ts.SQL, "QUOTENAME(N''_Node123'')") ||
		!strings.Contains(ts.SQL, "QUOTENAME(N''dbo'')") || !strings.Contains(ts.SQL, "QUOTENAME(N''x''''y]'')") || !strings.Contains(ts.SQL, "FROM WINDOWS") {
		t.Errorf("tables: %v\n%s", err, ts.SQL)
	}
	for _, r := range []SQLGrantRequest{
		{Principal: "a", Database: "d", Auth: "kerberos"},
		{Principal: "a", Database: "d", Auth: "sql", Windows: true},
		{Principal: "a", Database: "d", Tables: []string{"s.\x00"}},
		{Principal: "a", Database: "d", Tables: []string{".t"}},
		{Principal: "a", Database: "d", Tables: make([]string, MaxSQLGrantTables+1)},
	} {
		if _, err := SQLGrantScriptFor(r); err == nil {
			t.Errorf("%+v accepted", r)
		}
	}
	if _, err := SQLGrantScriptFor(SQLGrantRequest{Principal: `NT AUTHORITY\SYSTEM`, Windows: true, Database: "d"}); err != nil {
		t.Error(err)
	}
}
