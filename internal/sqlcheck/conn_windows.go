//go:build windows

package sqlcheck

// Windows authentication = SSPI via secur32.dll (driver package winsspi,
// registered by the driver on Windows; imported here to make it explicit).
import _ "github.com/microsoft/go-mssqldb/integratedauth/winsspi"

const integratedAuthProvider = "winsspi"
