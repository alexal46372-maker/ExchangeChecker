package sqlcheck

import (
	"errors"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9 (EC Tray request, CHK-SQL-09): ErrorInfo adds one stable code
// (model.SQLCode*), the diagnosis key and the SQL error number to an
// error of this package. ErrorKind and ConnCategory are unchanged.

var permissionErrorNumbers = map[int32]bool{229: true, 297: true, 300: true, 916: true}

// ErrNot1CDatabase marks a database without 1C service tables (wizard).
var ErrNot1CDatabase = errors.New("sqlcheck: not a 1C database")

// ErrorInfo classifies err (nil → zero value). Mapping: SQL 18456/18452/
// 18486-18488 → login_failed; 4060/4063/4064 → database_unavailable;
// 229/297/300/916 → permission_denied; TLS keys → tls; driver "no instance
// matching" → instance_not_found; SQL Browser (UDP 1434) failure →
// browser_unreachable; then ConnCategory (timeout, network, config) and
// ErrNot1CDatabase → not_1c_database; everything else other.
func ErrorInfo(err error) model.SQLErrorInfo {
	if err == nil {
		return model.SQLErrorInfo{}
	}
	cat := ConnCategory(err)
	info := model.SQLErrorInfo{Category: cat, SQLNumber: int(sqlNumber(err))}
	var e *Error
	if errors.As(err, &e) && isTLSKey(e.Key) {
		info.TLSKey = e.Key
	}
	msg := strings.ToLower(err.Error())
	n := int32(info.SQLNumber)
	switch {
	case errors.Is(err, ErrNot1CDatabase):
		info.Code = model.SQLCodeNot1CDatabase
	case permissionErrorNumbers[n]:
		info.Code = model.SQLCodePermissionDenied
	case cat == ConnCategoryLogin:
		info.Code = model.SQLCodeLoginFailed
	case cat == ConnCategoryDatabase:
		info.Code = model.SQLCodeDatabaseUnavailable
	case cat == ConnCategoryTLS:
		info.Code = model.SQLCodeTLS
	case strings.Contains(msg, "no instance matching"):
		info.Code = model.SQLCodeInstanceNotFound
	case strings.Contains(msg, ":1434") || strings.Contains(msg, "sql server browser"):
		info.Code = model.SQLCodeBrowserUnreachable
	case cat == ConnCategoryTimeout:
		info.Code = model.SQLCodeTimeout
	case cat == ConnCategoryNetwork:
		info.Code = model.SQLCodeNetwork
	case cat == ConnCategoryConfig:
		info.Code = model.SQLCodeConfig
	default:
		info.Code = model.SQLCodeOther
	}
	info.ErrorKey = model.SQLDiagKeyForCode(info.Code, info.TLSKey)
	return info
}

// sqlNumber returns the first SQL Server error number in err (0: none);
// a login / database / permission number in any token wins.
func sqlNumber(err error) int32 {
	var nums []int32
	var me mssql.Error
	if errors.As(err, &me) {
		for _, x := range me.All {
			nums = append(nums, x.Number)
		}
		nums = append(nums, me.Number)
	}
	var num sqlErrorNumber
	if errors.As(err, &num) {
		nums = append(nums, num.SQLErrorNumber())
	}
	for _, x := range nums {
		if loginErrorNumbers[x] || databaseErrorNumbers[x] || permissionErrorNumbers[x] {
			return x
		}
	}
	if len(nums) > 0 {
		return nums[0]
	}
	return 0
}
