// Package version holds the product version (PLAN §12, phase 7a,
// CHK-VER-01).
package version

// Version is the product version, semver without a leading "v": a release
// build sets it from the tag vX.Y.Z, CI to 0.0.0-ci.<run_number>, through
//
//	-ldflags "-X github.com/alexal46372-maker/ExchangeChecker/internal/version.Version=$V"
//
// It is a var (not a const) so the linker can override it; the default is
// for local/dev builds. The VERSIONINFO resources (tools/mkres) carry the
// same string.
var Version = "0.0.0-dev"

// ProductName is the product name (VERSIONINFO ProductName, Uninstall
// DisplayName).
const ProductName = "Exchange Checker"
