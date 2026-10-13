package wininstall

import (
	"errors"
	"fmt"
	"strings"
)

// Exit codes of the setup / uninstaller (docs/PHASE7.md §1.3).
const (
	ExitOK             = 0
	ExitError          = 1
	ExitCancelled      = 2
	ExitDowngrade      = 3
	ExitRebootRequired = 3010 // ERROR_SUCCESS_REBOOT_REQUIRED
)

// Options is the parsed command line.
//
// Install: [/S] [/DIR=<folder>] [/NOSTART] [/NOSHORTCUT] [/LOG=<file>]
// [/ALLOWDOWNGRADE] [/LANG=ru|en]. Uninstall: /uninstall [/S] [/PURGEDATA]
// [/LOG=<file>] [/LANG=ru|en]. Switches are case-insensitive; "-" works
// like "/".
type Options struct {
	Silent         bool
	Uninstall      bool
	Dir            string
	NoStart        bool // the checkbox "start the service and start it automatically" off
	NoShortcut     bool
	Log            string
	AllowDowngrade bool
	PurgeData      bool
	Lang           string // "", "ru", "en"
}

// ErrUsage: an unknown or malformed switch.
var ErrUsage = errors.New("setup: usage")

// ParseArgs parses os.Args[1:]. The flag package is not used: it writes
// usage text to stderr, which a GUI process does not have.
func ParseArgs(args []string) (Options, error) {
	var o Options
	for _, a := range args {
		if a == "" {
			continue
		}
		if a[0] != '/' && a[0] != '-' {
			return o, fmt.Errorf("%w: unexpected argument %q", ErrUsage, a)
		}
		name, val, hasVal := strings.Cut(a[1:], "=")
		name = strings.ToUpper(name)
		needVal := func() (string, error) {
			if !hasVal || strings.TrimSpace(val) == "" {
				return "", fmt.Errorf("%w: /%s needs a value", ErrUsage, name)
			}
			return strings.Trim(strings.TrimSpace(val), `"`), nil
		}
		noVal := func() error {
			if hasVal {
				return fmt.Errorf("%w: /%s takes no value", ErrUsage, name)
			}
			return nil
		}
		var err error
		switch name {
		case "S", "SILENT":
			err, o.Silent = noVal(), true
		case "UNINSTALL":
			err, o.Uninstall = noVal(), true
		case "NOSTART":
			err, o.NoStart = noVal(), true
		case "NOSHORTCUT":
			err, o.NoShortcut = noVal(), true
		case "ALLOWDOWNGRADE":
			err, o.AllowDowngrade = noVal(), true
		case "PURGEDATA":
			err, o.PurgeData = noVal(), true
		case "DIR":
			o.Dir, err = needVal()
		case "LOG":
			o.Log, err = needVal()
		case "LANG":
			o.Lang, err = needVal()
			o.Lang = strings.ToLower(o.Lang)
			if err == nil && o.Lang != "ru" && o.Lang != "en" {
				err = fmt.Errorf("%w: /LANG=%s (ru or en)", ErrUsage, o.Lang)
			}
		default:
			err = fmt.Errorf("%w: unknown switch /%s", ErrUsage, name)
		}
		if err != nil {
			return o, err
		}
	}
	if o.Uninstall && (o.Dir != "" || o.NoStart || o.NoShortcut || o.AllowDowngrade) {
		return o, fmt.Errorf("%w: /uninstall takes only /S, /PURGEDATA, /LOG, /LANG", ErrUsage)
	}
	if !o.Uninstall && o.PurgeData {
		return o, fmt.Errorf("%w: /PURGEDATA needs /uninstall", ErrUsage)
	}
	return o, nil
}
