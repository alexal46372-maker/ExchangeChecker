package wininstall

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver is a parsed X.Y.Z[-pre] version (build metadata ignored).
type Semver struct {
	Major, Minor, Patch int
	Pre                 string
}

// ParseSemver parses version.Version / DisplayVersion.
func ParseSemver(s string) (Semver, error) {
	var v Semver
	core := s
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core, v.Pre = core[:i], core[i+1:]
		if v.Pre == "" {
			return v, fmt.Errorf("version %q: empty pre-release", s)
		}
	}
	p := strings.Split(core, ".")
	if len(p) != 3 {
		return v, fmt.Errorf("version %q is not X.Y.Z", s)
	}
	n := [3]*int{&v.Major, &v.Minor, &v.Patch}
	for i, x := range p {
		k, err := strconv.Atoi(x)
		if err != nil || k < 0 || (len(x) > 1 && x[0] == '0') {
			return v, fmt.Errorf("version %q is not X.Y.Z", s)
		}
		*n[i] = k
	}
	return v, nil
}

// Compare orders by X.Y.Z (pre-release ignored; see Decide).
func (a Semver) Compare(b Semver) int {
	for _, d := range [3]int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Mode is what the setup does on this machine.
type Mode string

const (
	ModeFresh     Mode = "fresh"     // nothing installed
	ModeUpgrade   Mode = "upgrade"   // newer than installed
	ModeReinstall Mode = "reinstall" // same version, or pre-release / dev on either side
	ModeDowngrade Mode = "downgrade" // older than installed (refused without /ALLOWDOWNGRADE)
)

// Decide applies the version policy (PLAN §12 п. 90.6): newer → upgrade,
// equal → reinstall, older → downgrade (refused, exit 3, unless
// /ALLOWDOWNGRADE); a pre-release / dev version on either side, or an
// unreadable installed version, is a reinstall with a warning.
func Decide(installed, newVersion string, present bool) (mode Mode, warning string) {
	if !present {
		return ModeFresh, ""
	}
	a, errA := ParseSemver(installed)
	b, errB := ParseSemver(newVersion)
	switch {
	case errA != nil || errB != nil:
		return ModeReinstall, fmt.Sprintf("version %q over %q not comparable: reinstall", newVersion, installed)
	case a.Pre != "" || b.Pre != "":
		return ModeReinstall, fmt.Sprintf("pre-release / dev version (%s over %s): treated as reinstall", newVersion, installed)
	}
	switch b.Compare(a) {
	case 1:
		return ModeUpgrade, ""
	case 0:
		return ModeReinstall, ""
	}
	return ModeDowngrade, ""
}
