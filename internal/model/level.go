package model

import (
	"fmt"
	"strings"
)

// AlertLevel is the severity of an exchange point or a profile.
// Greater value = worse; Worst relies on this ordering.
type AlertLevel int

const (
	LevelOK AlertLevel = iota
	LevelWarning
	LevelError
)

// String returns a non-localized name: "OK", "Warning", "Error".
func (l AlertLevel) String() string {
	switch l {
	case LevelOK:
		return "OK"
	case LevelWarning:
		return "Warning"
	case LevelError:
		return "Error"
	}
	return fmt.Sprintf("AlertLevel(%d)", int(l))
}

// Valid reports whether l is one of the defined levels.
func (l AlertLevel) Valid() bool { return l >= LevelOK && l <= LevelError }

// MarshalText encodes the level as "ok" / "warning" / "error".
func (l AlertLevel) MarshalText() ([]byte, error) {
	if !l.Valid() {
		return nil, fmt.Errorf("model: invalid alert level %d", int(l))
	}
	return []byte(strings.ToLower(l.String())), nil
}

// UnmarshalText decodes "ok" / "warning" / "error" (case-insensitive).
func (l *AlertLevel) UnmarshalText(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "ok":
		*l = LevelOK
	case "warning":
		*l = LevelWarning
	case "error":
		*l = LevelError
	default:
		return fmt.Errorf("model: unknown alert level %q", string(b))
	}
	return nil
}

// Worst returns the most severe of the given levels (LevelOK when none).
func Worst(levels ...AlertLevel) AlertLevel {
	w := LevelOK
	for _, l := range levels {
		if l > w {
			w = l
		}
	}
	return w
}
