//go:build !windows

package sqlcheck

// Non-Windows builds (CI/tests only) use the driver's pure-Go NTLM provider,
// which needs an explicit DOMAIN\user + password.
const integratedAuthProvider = "ntlm"
