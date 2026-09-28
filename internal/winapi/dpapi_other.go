//go:build !windows

package winapi

// ProtectData is not available outside Windows.
func ProtectData(plain, entropy []byte, flags uint32) ([]byte, error) {
	return nil, ErrNotSupported
}

// UnprotectData is not available outside Windows.
func UnprotectData(blob, entropy []byte, flags uint32) ([]byte, error) {
	return nil, ErrNotSupported
}
