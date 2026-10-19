package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-09: stable error codes, diagnosis keys and SQL numbers.
func TestCHK_SQL_09_ErrorInfo(t *testing.T) {
	sqlErr := func(n int32) error {
		e := mssql.Error{Number: n, Message: "x"}
		e.All = []mssql.Error{e}
		return e
	}
	cases := []struct {
		err  error
		code string
		num  int
	}{
		{classify("connect", sqlErr(18456), true), model.SQLCodeLoginFailed, 18456},
		{classify("connect", sqlErr(4060), true), model.SQLCodeDatabaseUnavailable, 4060},
		{classify("catalog", sqlErr(229), false), model.SQLCodePermissionDenied, 229},
		{classify("catalog", sqlErr(297), false), model.SQLCodePermissionDenied, 297},
		{&Error{Kind: KindConn, Op: "connect", Err: errors.New("handshake"), Key: MsgTLSCertificate}, model.SQLCodeTLS, 0},
		{classify("connect", errors.New("no instance matching 'X' returned from host 'h'"), true), model.SQLCodeInstanceNotFound, 0},
		{classify("connect", &net.OpError{Op: "read", Net: "udp", Err: errors.New("i/o timeout on 10.0.0.1:1434")}, true), model.SQLCodeBrowserUnreachable, 0},
		{ctxErr("x", context.DeadlineExceeded), model.SQLCodeTimeout, 0},
		{classify("connect", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, true), model.SQLCodeNetwork, 0},
		{&Error{Kind: KindConfig, Op: "open", Err: errors.New("bad")}, model.SQLCodeConfig, 0},
		{fmt.Errorf("wizard: %w", ErrNot1CDatabase), model.SQLCodeNot1CDatabase, 0},
		{classify("query", sqlErr(208), false), model.SQLCodeOther, 208},
	}
	for i, c := range cases {
		info := ErrorInfo(c.err)
		if info.Code != c.code || info.SQLNumber != c.num || info.ErrorKey == "" || model.SQLMessage(info.ErrorKey, model.LangEN) == "" {
			t.Errorf("%d %v: %+v", i, c.err, info)
		}
	}
	if ErrorInfo(cases[4].err).ErrorKey != "sql.diag_tls_certificate" || ErrorInfo(nil).Code != "" {
		t.Error("tls refinement / nil")
	}
	for _, code := range model.SQLErrorCodes() {
		if d := model.SQLDiagnoseCode(code, ""); d.Key != model.SQLDiagKeyForCode(code, "") || len(d.Steps) == 0 {
			t.Errorf("%s: %+v", code, d.Key)
		}
	}
}
