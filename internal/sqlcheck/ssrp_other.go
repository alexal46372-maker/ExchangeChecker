//go:build !windows

package sqlcheck

// ssrpDecode: off Windows (tests, CI) SSRP bytes are taken as they are;
// invalid UTF-8 is dropped by ssrpValue.
func ssrpDecode(b []byte) string { return string(b) }
