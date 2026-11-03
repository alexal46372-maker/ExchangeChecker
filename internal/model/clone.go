package model

import "strings"

// CloneNameSuffix returns the " (копия)" suffix for clone_profile
// (CHK-CFG-08).
func CloneNameSuffix() string {
	return " (копия)"
}

// DefaultCloneName builds "<old> (копия)". Empty old ->
// just the trimmed suffix without a leading space artifact.
func DefaultCloneName(oldName string) string {
	old := strings.TrimSpace(oldName)
	suf := CloneNameSuffix()
	if old == "" {
		return strings.TrimSpace(suf)
	}
	return old + suf
}
