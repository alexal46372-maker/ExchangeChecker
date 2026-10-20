//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows Firewall (INetFwPolicy2 / INetFwRule) for the SQL Browser reply
// rule (phase 11, CHK-SETUP-13). Same COM pattern as the Start-menu
// shortcut (com_windows.go): CoCreateInstance + callProc, interface
// methods through the vtable (comCall). oleaut32 SysAllocString /
// SysFreeString for BSTRs. INetFwPolicy2 exists on Windows 7 (NT 6.1).

var (
	modoleaut32         = newSystemDLL("oleaut32.dll")
	procSysAllocString  = modoleaut32.NewProc("SysAllocString")
	procSysFreeString   = modoleaut32.NewProc("SysFreeString")
	procVariantClear    = modoleaut32.NewProc("VariantClear")
)

// Firewall rule constants (one place for port/protocol tuning; matches
// internal/sqlcheck.SQLBrowserPort = 1434 and UDP SSRP replies).
const (
	// FirewallRuleName is the exact display name of the inbound rule.
	FirewallRuleName = "Exchange Checker (SQL Browser)"
	// FirewallProtocolUDP is IPPROTO_UDP / NET_FW_IP_PROTOCOL_UDP.
	FirewallProtocolUDP = 17
	// FirewallRemotePorts is the SQL Browser source port of SSRP replies.
	FirewallRemotePorts = "1434"
	// FirewallLocalPorts is "*" (any / ephemeral ListenPacket :0).
	FirewallLocalPorts = "*"
	// FirewallRemoteAddresses is "*" (any IPv4 peer, including broadcast replies).
	FirewallRemoteAddresses = "*"
	// FirewallGrouping tags the rule for Remove-by-group on upgrade/uninstall.
	FirewallGrouping = "Exchange Checker"
	// FirewallProfiles is NET_FW_PROFILE2_DOMAIN|PRIVATE (Public excluded —
	// EC Service advice: discovery is for LAN/domain, not public networks).
	FirewallProfiles = 0x3

	netFwActionAllow = 1
	netFwRuleDirIn   = 1
	variantTrue      = 0xFFFF // VARIANT_TRUE
	variantFalse     = 0
	vtI4             = 3
)

var (
	// CLSID_NetFwPolicy2 {E2B3C97F-6AE1-41AC-817A-F6F92166D7DD}
	CLSID_NetFwPolicy2 = GUID{0xE2B3C97F, 0x6AE1, 0x41AC, [8]byte{0x81, 0x7A, 0xF6, 0xF9, 0x21, 0x66, 0xD7, 0xDD}}
	// IID_INetFwPolicy2 {98325047-C671-4174-8E67-CFC62AF2E0DB}
	IID_INetFwPolicy2 = GUID{0x98325047, 0xC671, 0x4174, [8]byte{0x8E, 0x67, 0xCF, 0xC6, 0x2A, 0xF2, 0xE0, 0xDB}}
	// CLSID_NetFwRule {2C5BC43E-3369-4C33-AB0C-BE9469677AF4}
	CLSID_NetFwRule = GUID{0x2C5BC43E, 0x3369, 0x4C33, [8]byte{0xAB, 0x0C, 0xBE, 0x94, 0x69, 0x67, 0x7A, 0xF4}}
	// IID_INetFwRule {AF230D27-BABA-4E42-ACED-F524F22CFCE2}
	IID_INetFwRule = GUID{0xAF230D27, 0xBABA, 0x4E42, [8]byte{0xAC, 0xED, 0xF5, 0x24, 0xF2, 0x2C, 0xFC, 0xE2}}
)

// INetFwPolicy2 / INetFwRules / INetFwRule vtable slots (IDispatch-based;
// IUnknown 0–2, IDispatch 3–6, then interface methods). Verified by
// TestCHK_SETUP_13_FirewallRoundTrip on Windows.
const (
	FwPolicy2GetRules = 18

	FwRulesGetCount = 7
	FwRulesAdd      = 8
	FwRulesRemove   = 9
	FwRulesItem     = 10

	FwRuleGetName            = 7
	FwRulePutName            = 8
	FwRuleGetApplicationName = 11
	FwRulePutApplicationName = 12
	FwRuleGetProtocol        = 15
	FwRulePutProtocol        = 16
	FwRuleGetLocalPorts      = 17
	FwRulePutLocalPorts      = 18
	FwRuleGetRemotePorts      = 19
	FwRulePutRemotePorts      = 20
	FwRuleGetRemoteAddresses  = 23
	FwRulePutRemoteAddresses  = 24
	FwRuleGetDirection        = 27
	FwRulePutDirection        = 28
	FwRulePutInterfaceTypes   = 32
	FwRuleGetEnabled          = 33
	FwRulePutEnabled          = 34
	FwRuleGetGrouping         = 35
	FwRulePutGrouping         = 36
	FwRuleGetProfiles         = 37
	FwRulePutProfiles         = 38
	FwRuleGetEdgeTraversal    = 39
	FwRulePutEdgeTraversal    = 40
	FwRuleGetAction           = 41
	FwRulePutAction           = 42
)

// variant is the Win32 VARIANT (24 bytes on amd64).
type variant struct {
	VT         uint16
	Reserved1  uint16
	Reserved2  uint16
	Reserved3  uint16
	Val        [16]byte
}

func allocBSTR(s string) (*uint16, error) {
	r, _, e := callProc(procSysAllocString, uintptr(unsafe.Pointer(UTF16Ptr(s))))
	if r == 0 {
		if e != nil {
			return nil, fmt.Errorf("SysAllocString: %v", e)
		}
		return nil, fmt.Errorf("SysAllocString: out of memory")
	}
	// Reinterpret the syscall uintptr as a pointer (vet-safe; same pattern
	// as converting a SysAllocString / CoTaskMemAlloc result).
	return *(**uint16)(unsafe.Pointer(&r)), nil
}

func freeBSTR(b *uint16) {
	if b != nil {
		callProc(procSysFreeString, uintptr(unsafe.Pointer(b)))
	}
}

func putBSTR(obj *comObject, idx int, what, s string) error {
	b, err := allocBSTR(s)
	if err != nil {
		return err
	}
	defer freeBSTR(b)
	return hrErr(what, comCall(obj, idx, uintptr(unsafe.Pointer(b))))
}

func getBSTR(obj *comObject, idx int, what string) (string, error) {
	var b *uint16
	if err := hrErr(what, comCall(obj, idx, uintptr(unsafe.Pointer(&b)))); err != nil {
		return "", err
	}
	defer freeBSTR(b)
	if b == nil {
		return "", nil
	}
	return syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(b))[:]), nil
}

func newFwPolicy2() (*comObject, error) {
	var p *comObject
	hr, _, _ := callProc(procCoCreateInstance,
		uintptr(unsafe.Pointer(&CLSID_NetFwPolicy2)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&IID_INetFwPolicy2)), uintptr(unsafe.Pointer(&p)))
	if err := hrErr("CoCreateInstance(NetFwPolicy2)", hr); err != nil {
		return nil, err
	}
	return p, nil
}

func newFwRule() (*comObject, error) {
	var r *comObject
	hr, _, _ := callProc(procCoCreateInstance,
		uintptr(unsafe.Pointer(&CLSID_NetFwRule)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&IID_INetFwRule)), uintptr(unsafe.Pointer(&r)))
	if err := hrErr("CoCreateInstance(NetFwRule)", hr); err != nil {
		return nil, err
	}
	return r, nil
}

func fwRules(policy *comObject) (*comObject, error) {
	var rules *comObject
	if err := hrErr("INetFwPolicy2.get_Rules", comCall(policy, FwPolicy2GetRules, uintptr(unsafe.Pointer(&rules)))); err != nil {
		return nil, err
	}
	return rules, nil
}

func fwRemoveByName(rules *comObject, name string) error {
	b, err := allocBSTR(name)
	if err != nil {
		return err
	}
	defer freeBSTR(b)
	// Remove deletes every rule with that name; missing = S_OK.
	return hrErr("INetFwRules.Remove", comCall(rules, FwRulesRemove, uintptr(unsafe.Pointer(b))))
}

// fwRemoveOurs deletes every rule whose Name is FirewallRuleName or whose
// Grouping is FirewallGrouping (only those). Collect names first so indices
// stay stable while enumerating.
func fwRemoveOurs(rules *comObject) error {
	list, err := listFwRules(rules)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var names []string
	for _, info := range list {
		if info.Name == FirewallRuleName || info.Grouping == FirewallGrouping {
			if !seen[info.Name] && info.Name != "" {
				seen[info.Name] = true
				names = append(names, info.Name)
			}
		}
	}
	for _, n := range names {
		if err := fwRemoveByName(rules, n); err != nil {
			return err
		}
	}
	return nil
}

// AddSQLBrowserFirewallRule installs (or replaces) the inbound UDP rule
// that lets ExchangeCheckerSvc.exe receive SQL Browser (SSRP) replies.
// Call inside WithCOM. Idempotent: removes every rule with our Name or
// Grouping, then adds one. appExe is the full path of the service exe
// (same path as the SCM ImagePath). ServiceName is not set (SID type NONE).
func AddSQLBrowserFirewallRule(appExe string) error {
	policy, err := newFwPolicy2()
	if err != nil {
		return err
	}
	defer comCall(policy, vtRelease)
	rules, err := fwRules(policy)
	if err != nil {
		return err
	}
	defer comCall(rules, vtRelease)
	if err := fwRemoveOurs(rules); err != nil {
		return err
	}
	rule, err := newFwRule()
	if err != nil {
		return err
	}
	defer comCall(rule, vtRelease)
	set := []struct {
		idx  int
		what string
		val  string
	}{
		{FwRulePutName, "put_Name", FirewallRuleName},
		{FwRulePutApplicationName, "put_ApplicationName", appExe},
		{FwRulePutLocalPorts, "put_LocalPorts", FirewallLocalPorts},
		{FwRulePutRemotePorts, "put_RemotePorts", FirewallRemotePorts},
		{FwRulePutRemoteAddresses, "put_RemoteAddresses", FirewallRemoteAddresses},
		{FwRulePutGrouping, "put_Grouping", FirewallGrouping},
		{FwRulePutInterfaceTypes, "put_InterfaceTypes", "All"},
	}
	for _, s := range set {
		if err := putBSTR(rule, s.idx, s.what, s.val); err != nil {
			return err
		}
	}
	if err := hrErr("put_Protocol", comCall(rule, FwRulePutProtocol, uintptr(FirewallProtocolUDP))); err != nil {
		return err
	}
	if err := hrErr("put_Direction", comCall(rule, FwRulePutDirection, uintptr(netFwRuleDirIn))); err != nil {
		return err
	}
	if err := hrErr("put_Action", comCall(rule, FwRulePutAction, uintptr(netFwActionAllow))); err != nil {
		return err
	}
	if err := hrErr("put_Profiles", comCall(rule, FwRulePutProfiles, uintptr(FirewallProfiles))); err != nil {
		return err
	}
	if err := hrErr("put_EdgeTraversal", comCall(rule, FwRulePutEdgeTraversal, uintptr(variantFalse))); err != nil {
		return err
	}
	if err := hrErr("put_Enabled", comCall(rule, FwRulePutEnabled, uintptr(variantTrue))); err != nil {
		return err
	}
	return hrErr("INetFwRules.Add", comCall(rules, FwRulesAdd, uintptr(unsafe.Pointer(rule))))
}

// RemoveSQLBrowserFirewallRule deletes every firewall rule with our Name
// or Grouping (missing = success). Call inside WithCOM.
func RemoveSQLBrowserFirewallRule() error {
	policy, err := newFwPolicy2()
	if err != nil {
		return err
	}
	defer comCall(policy, vtRelease)
	rules, err := fwRules(policy)
	if err != nil {
		return err
	}
	defer comCall(rules, vtRelease)
	return fwRemoveOurs(rules)
}

// FirewallRuleInfo is a read-back of one INetFwRule (CHK-SETUP-13 e2e).
type FirewallRuleInfo struct {
	Name, Application, LocalPorts, RemotePorts string
	RemoteAddresses, Grouping                  string
	Protocol, Direction, Action, Profiles      int32
	Enabled, EdgeTraversal                     bool
}

func listFwRules(rules *comObject) ([]FirewallRuleInfo, error) {
	var count int32
	if err := hrErr("get_Count", comCall(rules, FwRulesGetCount, uintptr(unsafe.Pointer(&count)))); err != nil {
		return nil, err
	}
	var out []FirewallRuleInfo
	for i := int32(1); i <= count; i++ {
		var v variant
		v.VT = vtI4
		*(*int32)(unsafe.Pointer(&v.Val[0])) = i
		var rule *comObject
		hr := comCall(rules, FwRulesItem, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&rule)))
		callProc(procVariantClear, uintptr(unsafe.Pointer(&v)))
		if err := hrErr("Item", hr); err != nil {
			continue
		}
		info, err := readFwRule(rule)
		comCall(rule, vtRelease)
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// ListFirewallRulesNamed returns every rule whose Name equals name
// (case-sensitive, as stored). Call inside WithCOM.
func ListFirewallRulesNamed(name string) ([]FirewallRuleInfo, error) {
	policy, err := newFwPolicy2()
	if err != nil {
		return nil, err
	}
	defer comCall(policy, vtRelease)
	rules, err := fwRules(policy)
	if err != nil {
		return nil, err
	}
	defer comCall(rules, vtRelease)
	all, err := listFwRules(rules)
	if err != nil {
		return nil, err
	}
	var out []FirewallRuleInfo
	for _, info := range all {
		if info.Name == name {
			out = append(out, info)
		}
	}
	return out, nil
}

func readFwRule(rule *comObject) (FirewallRuleInfo, error) {
	var info FirewallRuleInfo
	var err error
	if info.Name, err = getBSTR(rule, FwRuleGetName, "get_Name"); err != nil {
		return info, err
	}
	if info.Application, err = getBSTR(rule, FwRuleGetApplicationName, "get_ApplicationName"); err != nil {
		return info, err
	}
	if info.LocalPorts, err = getBSTR(rule, FwRuleGetLocalPorts, "get_LocalPorts"); err != nil {
		return info, err
	}
	if info.RemotePorts, err = getBSTR(rule, FwRuleGetRemotePorts, "get_RemotePorts"); err != nil {
		return info, err
	}
	if info.RemoteAddresses, err = getBSTR(rule, FwRuleGetRemoteAddresses, "get_RemoteAddresses"); err != nil {
		return info, err
	}
	if info.Grouping, err = getBSTR(rule, FwRuleGetGrouping, "get_Grouping"); err != nil {
		return info, err
	}
	if err := hrErr("get_Protocol", comCall(rule, FwRuleGetProtocol, uintptr(unsafe.Pointer(&info.Protocol)))); err != nil {
		return info, err
	}
	if err := hrErr("get_Direction", comCall(rule, FwRuleGetDirection, uintptr(unsafe.Pointer(&info.Direction)))); err != nil {
		return info, err
	}
	if err := hrErr("get_Action", comCall(rule, FwRuleGetAction, uintptr(unsafe.Pointer(&info.Action)))); err != nil {
		return info, err
	}
	if err := hrErr("get_Profiles", comCall(rule, FwRuleGetProfiles, uintptr(unsafe.Pointer(&info.Profiles)))); err != nil {
		return info, err
	}
	var en, edge int16
	if err := hrErr("get_Enabled", comCall(rule, FwRuleGetEnabled, uintptr(unsafe.Pointer(&en)))); err != nil {
		return info, err
	}
	info.Enabled = en != 0
	if err := hrErr("get_EdgeTraversal", comCall(rule, FwRuleGetEdgeTraversal, uintptr(unsafe.Pointer(&edge)))); err != nil {
		return info, err
	}
	info.EdgeTraversal = edge != 0
	return info, nil
}
