package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Phase 9 (PLAN §12 p. 99, CHK-SQL-09): a T-SQL script that a SQL Server
// administrator runs (SSMS, sqlcmd) to give Exchange Checker read-only
// access to one database: login (SQL or Windows) if missing, database
// user if missing, role db_datareader. SQL Server 2005/2008 compatible
// (no 2012+ syntax, CHK-SQL-05), idempotent (IF NOT EXISTS against
// sys.server_principals / sys.database_principals), one batch (no GO).
// The names reach the server only as N'...' literals ('' escaped) and are
// put into the DDL by QUOTENAME on the server; a SQL login's password is
// never part of the script: SQLGrantPasswordPlaceholder must be replaced
// by the administrator, the script refuses to run otherwise.

// SQLGrantPasswordPlaceholder is replaced by the administrator.
const SQLGrantPasswordPlaceholder = "{{PASSWORD}}"

// MaxSQLPrincipalLen: logins, users and databases are sysname (128).
const MaxSQLPrincipalLen = 128

// SQLGrantRequest asks for the grant script (IPC sql_grant_script).
type SQLGrantRequest struct {
	// Principal: a SQL login name, or for Windows = true a Windows account
	// DOMAIN\NAME, a computer account DOMAIN\HOST$ (the service running as
	// LocalSystem / NetworkService on another computer) or
	// NT AUTHORITY\SYSTEM (same computer).
	Principal string `json:"principal"`
	Windows   bool   `json:"windows"`
	Database  string `json:"database"`
}

// SQLGrantScript is the generated script with notes for the wizard.
type SQLGrantScript struct {
	SQL string `json:"sql"`
	// PasswordPlaceholder is set for a SQL login: the text to replace.
	PasswordPlaceholder string          `json:"passwordPlaceholder,omitempty"`
	Notes               []LocalizedText `json:"notes"`
}

// Reasons of ValSQLPrincipal ({2}).
const (
	SQLPrincipalEmptyPart     = "empty_part"     // DOMAIN\ or \NAME
	SQLPrincipalControl       = "control_char"   // control / invalid UTF-8
	SQLPrincipalSpace         = "space"          // leading / trailing white space
	SQLPrincipalTooLong       = "too_long"       // > 128 characters
	SQLPrincipalBackslash     = "backslash"      // SQL login with '\'
	SQLPrincipalWindowsFormat = "windows_format" // Windows account not DOMAIN\NAME
	SQLPrincipalInvalidChar   = "invalid_char"   // character not allowed in a Windows account
)

func checkSysname(field, s string) error {
	switch {
	case s == "":
		return fieldErr(field, ValRequired, field+" is required")
	case !utf8.ValidString(s) || strings.IndexFunc(s, unicode.IsControl) >= 0:
		return fieldErr(field, ValSQLPrincipal, field+": control character", SQLPrincipalControl)
	case strings.TrimSpace(s) != s:
		return fieldErr(field, ValSQLPrincipal, field+": leading or trailing space", SQLPrincipalSpace)
	case utf8.RuneCountInString(s) > MaxSQLPrincipalLen:
		return fieldErr(field, ValSQLPrincipal, field+": longer than 128 characters", SQLPrincipalTooLong)
	}
	return nil
}

// Validate checks the request; errors are *FieldError (principal,
// database; key validation.sql_principal with a reason, or
// validation.required).
func (r SQLGrantRequest) Validate() error {
	if err := checkSysname("principal", r.Principal); err != nil {
		return err
	}
	if r.Windows {
		i := strings.IndexByte(r.Principal, '\\')
		if i < 0 || strings.Count(r.Principal, `\`) != 1 {
			return fieldErr("principal", ValSQLPrincipal, `principal: DOMAIN\NAME expected`, SQLPrincipalWindowsFormat)
		}
		dom, name := r.Principal[:i], r.Principal[i+1:]
		if dom == "" || name == "" || name == "$" {
			return fieldErr("principal", ValSQLPrincipal, "principal: empty domain or name", SQLPrincipalEmptyPart)
		}
		if strings.ContainsAny(dom, `/:*?"<>|[]`) || strings.ContainsAny(strings.TrimSuffix(name, "$"), `/[]:;|=,+*?<>"$`) {
			return fieldErr("principal", ValSQLPrincipal, "principal: character not allowed in a Windows account", SQLPrincipalInvalidChar)
		}
	} else if strings.ContainsRune(r.Principal, '\\') {
		return fieldErr("principal", ValSQLPrincipal, `principal: a SQL login cannot contain '\'`, SQLPrincipalBackslash)
	}
	return checkSysname("database", r.Database)
}

// sqlNLiteral returns s as a T-SQL N'...' literal.
func sqlNLiteral(s string) string { return "N'" + strings.ReplaceAll(s, "'", "''") + "'" }

// SQLGrantScriptFor validates r and returns the script.
func SQLGrantScriptFor(r SQLGrantRequest) (SQLGrantScript, error) {
	if err := r.Validate(); err != nil {
		return SQLGrantScript{}, err
	}
	var b strings.Builder
	w := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l)
			b.WriteString("\r\n")
		}
	}
	kind := "SQL Server login"
	if r.Windows {
		kind = "Windows login"
	}
	w("-- Exchange Checker: read-only access (role db_datareader) to one database for a "+kind+".",
		"-- SQL Server 2005 / 2008 and newer. Safe to run again. Run as a member of sysadmin",
		"-- (or securityadmin + db_owner of the database).")
	if !r.Windows {
		w("-- Replace " + SQLGrantPasswordPlaceholder + " below with the password of the login first.")
	}
	w("SET NOCOUNT ON;",
		"DECLARE @login sysname, @db sysname, @password nvarchar(128), @sql nvarchar(max);",
		"SET @login = "+sqlNLiteral(r.Principal)+";",
		"SET @db = "+sqlNLiteral(r.Database)+";")
	if !r.Windows {
		w("SET @password = N'"+SQLGrantPasswordPlaceholder+"';",
			"IF @password IS NULL OR LEN(@password) = 0 OR @password = N'{{' + N'PASSWORD}}'",
			"BEGIN",
			"    RAISERROR(N'Exchange Checker: replace "+SQLGrantPasswordPlaceholder+" with the password of the login.', 16, 1);",
			"    RETURN;",
			"END;")
	}
	w("IF DB_ID(@db) IS NULL",
		"BEGIN",
		"    RAISERROR(N'Exchange Checker: database %s not found.', 16, 1, @db);",
		"    RETURN;",
		"END;",
		"IF NOT EXISTS (SELECT 1 FROM sys.server_principals WHERE name = @login)",
		"BEGIN")
	if r.Windows {
		w("    SET @sql = N'CREATE LOGIN ' + QUOTENAME(@login) + N' FROM WINDOWS WITH DEFAULT_DATABASE = ' + QUOTENAME(@db) + N';';")
	} else {
		w("    SET @sql = N'CREATE LOGIN ' + QUOTENAME(@login) + N' WITH PASSWORD = ' + QUOTENAME(@password, N'''')",
			"        + N', CHECK_POLICY = ON, CHECK_EXPIRATION = OFF, DEFAULT_DATABASE = ' + QUOTENAME(@db) + N';';")
	}
	w("    EXEC (@sql);",
		"END;",
		"SET @sql = N'USE ' + QUOTENAME(@db) + N';",
		"IF NOT EXISTS (SELECT 1 FROM sys.database_principals WHERE name = @login)",
		"BEGIN",
		"    DECLARE @create nvarchar(max);",
		"    SET @create = N''CREATE USER '' + QUOTENAME(@login) + N'' FOR LOGIN '' + QUOTENAME(@login) + N'';'';",
		"    EXEC (@create);",
		"END;",
		"EXEC sp_addrolemember N''db_datareader'', @login;';",
		"EXEC sp_executesql @sql, N'@login sysname', @login = @login;",
		"PRINT N'Exchange Checker: access granted (db_datareader).';")
	res := SQLGrantScript{SQL: b.String(), Notes: []LocalizedText{
		{"Скрипт выдаёт только чтение (роль db_datareader) в одной базе; его можно запускать повторно.", "The script grants read access only (db_datareader role) in one database; it can be run again."},
		{"Запускает администратор SQL Server (sysadmin, либо securityadmin + db_owner базы) в SQL Server Management Studio или sqlcmd.", "A SQL Server administrator runs it (sysadmin, or securityadmin + db_owner of the database) in SQL Server Management Studio or sqlcmd."},
	}}
	if r.Windows {
		res.Notes = append(res.Notes, LocalizedText{
			"Служба Exchange Checker работает как LocalSystem: на удалённом SQL Server выдайте права учётной записи компьютера ДОМЕН\\ИМЯ$, на этом же компьютере — NT AUTHORITY\\SYSTEM.",
			"The Exchange Checker service runs as LocalSystem: on a remote SQL Server grant the computer account DOMAIN\\NAME$, on the same computer NT AUTHORITY\\SYSTEM."})
	} else {
		res.PasswordPlaceholder = SQLGrantPasswordPlaceholder
		res.Notes = append(res.Notes,
			LocalizedText{"Замените " + SQLGrantPasswordPlaceholder + " надёжным паролем (без этого скрипт остановится с ошибкой). Пароль в скрипт программой не подставляется и нигде не сохраняется; после запуска службы введите его в трее.",
				"Replace " + SQLGrantPasswordPlaceholder + " with a strong password (the script stops with an error otherwise). The program never puts the password into the script or stores it; enter it in the tray after the service starts."},
			LocalizedText{"CHECK_POLICY = ON применяет политику паролей Windows (сложность, блокировка); CHECK_EXPIRATION = OFF — пароль не истекает, иначе опрос остановится.",
				"CHECK_POLICY = ON applies the Windows password policy (complexity, lockout); CHECK_EXPIRATION = OFF — the password does not expire, otherwise polling would stop."},
			LocalizedText{"На сервере должен быть включён смешанный режим проверки подлинности (SQL Server and Windows Authentication mode).",
				"The server must use mixed mode authentication (SQL Server and Windows Authentication mode)."})
	}
	return res, nil
}
