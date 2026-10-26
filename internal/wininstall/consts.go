package wininstall

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

// Security descriptors (PLAN §12 п. 90.4, 90.17; docs/PHASE7.md §2.3, §4).
const (
	// DataDirSDDL: %ProgramData%\ExchangeChecker, logs included — SYSTEM
	// and Administrators only, protected, inherited by files and folders.
	DataDirSDDL = "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// InstallDirSDDL: the install folder, always protected: Users read and
	// execute only (a LocalSystem service exe must not be replaceable).
	InstallDirSDDL = "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)"
	// ServiceSDDL: the service DACL — SY/BA as by default, Interactive and
	// Service users query, Authenticated Users QUERY_CONFIG / QUERY_STATUS
	// / ENUMERATE_DEPENDENTS / INTERROGATE / READ_CONTROL (the tray's G7
	// check needs SERVICE_QUERY_STATUS); start/stop only SY and BA.
	ServiceSDDL = "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)(A;;CCLCSWLORC;;;AU)"
)

// Registry locations (64-bit view).
const (
	UninstallKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\` + model.ServiceName
	EventLogKeyPath  = `SYSTEM\CurrentControlSet\Services\EventLog\Application\` + model.EventSource
	// UserKeyPath is the tray's per-user key (HKCU, language); removed for
	// the current user only on /PURGEDATA.
	UserKeyPath = `Software\ExchangeChecker`
	// EventTypesSupported: ERROR | WARNING | INFORMATION.
	EventTypesSupported = 7
)

// Service settings (PLAN §12 п. 90.1–90.3).
var (
	FailureResetPeriod = 24 * time.Hour
	FailureDelays      = []time.Duration{60 * time.Second, 120 * time.Second, 300 * time.Second}
	StartTimeout       = 60 * time.Second
	ForceStopTimeout   = 15 * time.Second
	StopTimeout        = 90 * time.Second
)

// SetupMutexName keeps one setup / uninstaller per machine.
const SetupMutexName = `Global\ExchangeCheckerSetup`

// FirewallRuleName must match winapi.FirewallRuleName (inbound UDP
// SQL Browser / SSRP replies for the service exe, CHK-SETUP-13).
const FirewallRuleName = "Exchange Checker (SQL Browser)"

// FirewallProfilePublic is NET_FW_PROFILE2_PUBLIC (must match winapi).
const FirewallProfilePublic = 0x4

// ServiceDescription is the SCM description in the installer language.
func ServiceDescription(lang string) string {
	if lang == "en" {
		return "Exchange Checker: monitors 1C exchange times in MS SQL databases; alerts in the tray and the event log."
	}
	return "Exchange Checker: контроль времени обмена 1С по базам MS SQL; тревоги в трее и журнале событий."
}

// RegValue is one value of the Uninstall key.
type RegValue struct {
	Name  string
	Str   string // REG_SZ when DWord is false
	DWord bool
	Num   uint32
}

// UninstallValues: the Uninstall key (docs/PHASE7.md §6.1); no Publisher
// (decision 5). uninstallExe is the full path of the uninstaller.
func UninstallValues(dir, uninstallExe, trayExe, ver string, sizeKB uint32, now time.Time) []RegValue {
	q := `"` + uninstallExe + `"`
	return []RegValue{
		{Name: "DisplayName", Str: version.ProductName},
		{Name: "DisplayVersion", Str: ver},
		{Name: "InstallLocation", Str: dir},
		{Name: "UninstallString", Str: q + " /uninstall"},
		{Name: "QuietUninstallString", Str: q + " /uninstall /S"},
		{Name: "DisplayIcon", Str: trayExe + ",0"},
		{Name: "InstallDate", Str: now.Format("20060102")},
		{Name: "EstimatedSize", DWord: true, Num: sizeKB},
		{Name: "NoModify", DWord: true, Num: 1},
		{Name: "NoRepair", DWord: true, Num: 1},
	}
}

// ServiceImagePath quotes the exe path (no unquoted-service-path hole).
func ServiceImagePath(exe string) string { return `"` + exe + `"` }

// ExePathFromImagePath returns the exe of an ImagePath ("…" or bare).
func ExePathFromImagePath(img string) string {
	img = strings.TrimSpace(img)
	if strings.HasPrefix(img, `"`) {
		if i := strings.IndexByte(img[1:], '"'); i >= 0 {
			return img[1 : i+1]
		}
	}
	if i := strings.Index(strings.ToLower(img), ".exe"); i >= 0 {
		return img[:i+4]
	}
	return img
}

func joinDir(dir, name string) string {
	return strings.TrimRight(dir, `\`) + `\` + name
}

// ParentDir is filepath.Dir for Windows paths (backslash), portable on linux tests.
func ParentDir(p string) string {
	p = CleanDir(p)
	if i := strings.LastIndex(p, `\`); i >= 0 {
		if i == 2 && len(p) >= 3 && p[1] == ':' {
			return p[:3] // "C:\"
		}
		return p[:i]
	}
	return p
}

func sizeKB(n int64) uint32 {
	k := (n + 1023) / 1024
	if k > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(k)
}

func errf(step string, err error) error { return fmt.Errorf("%s: %w", step, err) }
