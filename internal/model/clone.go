package model

import "strings"

// CloneNameSuffix returns the localized " (copy)" / " (копия)" suffix for
// clone_profile (CHK-CFG-08). lang is "en" or anything else (RU default).
func CloneNameSuffix(lang string) string {
	if lang == LangEN {
		return " (copy)"
	}
	return " (копия)"
}

// DefaultCloneName builds "<old> (копия)" / "<old> (copy)". Empty old ->
// just the trimmed suffix without a leading space artifact.
func DefaultCloneName(oldName, lang string) string {
	old := strings.TrimSpace(oldName)
	suf := CloneNameSuffix(lang)
	if old == "" {
		return strings.TrimSpace(suf)
	}
	return old + suf
}
