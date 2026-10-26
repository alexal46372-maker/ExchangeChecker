//go:build !windows

package sqlcheck

// systemLocal: the registry / SCM part of local discovery is Windows-only.
type systemLocal struct{}

func (systemLocal) supported() bool                                   { return false }
func (systemLocal) multiString(int, string, string) ([]string, error) { return nil, nil }
func (systemLocal) str(int, string, string) (string, bool, error)     { return "", false, nil }
func (systemLocal) browserState() string                              { return "" }
