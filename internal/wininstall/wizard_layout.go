package wininstall

// Wizard chrome geometry and palette (CHK-SETUP-14, modern Win32 UI).
// Client size ~640×420; left brand panel; content + button bar on the right.
// Values are logical pixels (setup is not dpiAware — PHASE7).

const (
	WizardClientW = 640
	WizardClientH = 420

	WizardBrandW    = 210
	WizardPad       = 24
	WizardGrid      = 8
	WizardBtnW      = 110
	WizardBtnH      = 32
	WizardBtnGap    = 8
	WizardBtnBarH   = 56
	WizardLineH     = 1
	WizardTitleH    = 28
	WizardEditH     = 26
	WizardProgH     = 18
	WizardUninstW   = 520
	WizardUninstH   = 260
	WizardBrandIcon = 32
)

// ContentLeft is the first x of the right-hand content column.
func ContentLeft() int32 { return WizardBrandW + WizardPad }

// ContentWidth is the usable width of the content column.
func ContentWidth() int32 {
	return WizardClientW - WizardBrandW - WizardPad*2
}

// ContentTop is the y of the first content line (below top padding).
func ContentTop() int32 { return WizardPad }

// ButtonBarTop is the y where the bottom button strip begins.
func ButtonBarTop() int32 { return WizardClientH - WizardBtnBarH }

// ContentBottom is the last y reserved for page body (above the button bar).
func ContentBottom() int32 { return ButtonBarTop() - WizardPad }

// COLORREF (0x00BBGGRR) for the modern setup chrome.
const (
	ColorBrandDark   uint32 = 0x0044270F // #0F2744
	ColorBrandMid    uint32 = 0x005C3A1B // #1B3A5C
	ColorContentBg   uint32 = 0x00FCFBFA // #FAFBFC
	ColorBtnBarBg    uint32 = 0x00F9F5F1 // #F1F5F9
	ColorBtnBarLine  uint32 = 0x00F0E8E2 // #E2E8F0
	ColorTextPrimary uint32 = 0x001E293B // #1E293B
	ColorTextMuted   uint32 = 0x0064748B // #64748B
	ColorBrandText   uint32 = 0x00F8FAFC // #F8FAFC
	ColorBrandMuted  uint32 = 0x00CBD5E1 // #CBD5E1
	ColorStepActive  uint32 = 0x00FFFFFF // #FFFFFF
	ColorAccent      uint32 = 0x00D97706 // #0677D9 progress bar (brand accent)
)

// StepIndex maps a wizard Page to the brand-panel step list (0..4).
func StepIndex(p Page) int {
	switch p {
	case PageWelcome:
		return 0
	case PageFolder:
		return 1
	case PageOptions:
		return 2
	case PageProgress:
		return 3
	case PageFinish:
		return 4
	default:
		return 0
	}
}

// StepKeys are catalog keys for the brand-panel step list (RU/EN).
var StepKeys = []string{
	"setup.step.welcome",
	"setup.step.folder",
	"setup.step.options",
	"setup.step.progress",
	"setup.step.finish",
}

// Control IDs (tests / E2E drive them with SendMessage). Shared with the
// Windows window; do not renumber — CHK-SETUP-10/12/14 contract.
const (
	IDBack      = 101
	IDNext      = 102
	IDCancel    = 103
	IDBrowse    = 104
	IDDir       = 105
	IDShortcut  = 106
	IDService   = 107
	IDTray      = 108
	IDText      = 109
	IDProgress  = 110
	IDStatus    = 111
	IDError     = 112
	IDPurge     = 113
	IDUninstall = 114
)
