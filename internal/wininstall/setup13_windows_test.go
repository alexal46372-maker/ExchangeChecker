//go:build windows

package wininstall

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_13_FirewallRoundTrip — COM INetFwPolicy2/INetFwRule with
// the documented GUIDs and vtable indices: add a rule for notepad.exe,
// read it back (name, app, protocol UDP, remote 1434, inbound allow,
// enabled, all profiles), add again (still exactly one), remove (zero).
func TestCHK_SETUP_13_FirewallRoundTrip(t *testing.T) {
	running, err := winapi.FirewallServiceRunning()
	if err != nil {
		t.Fatalf("query MpsSvc: %v", err)
	}
	if !running {
		t.Skip("MpsSvc (Windows Firewall) is not SERVICE_RUNNING; cannot exercise INetFwPolicy2")
	}
	win, err := winapi.WindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(win, "notepad.exe")
	const name = winapi.FirewallRuleName

	err = winapi.WithCOM(func() error {
		_ = winapi.RemoveSQLBrowserFirewallRule()
		if err := winapi.AddSQLBrowserFirewallRule(app); err != nil {
			return err
		}
		prof, err := winapi.FirewallCurrentProfiles()
		if err != nil {
			return err
		}
		if prof == 0 || prof&^0x7 != 0 {
			return fmt.Errorf("current profiles %#x", uint32(prof))
		}
		t.Logf("firewall current profiles %#x", uint32(prof))
		list, err := winapi.ListFirewallRulesNamed(name)
		if err != nil {
			return err
		}
		if len(list) != 1 {
			return fmt.Errorf("after add: %d rules", len(list))
		}
		r := list[0]
		if r.Name != name {
			return fmt.Errorf("name %q", r.Name)
		}
		if !strings.EqualFold(r.Application, app) {
			return fmt.Errorf("application %q want %q", r.Application, app)
		}
		if r.Protocol != winapi.FirewallProtocolUDP {
			return fmt.Errorf("protocol %d", r.Protocol)
		}
		if r.RemotePorts != winapi.FirewallRemotePorts {
			return fmt.Errorf("remotePorts %q", r.RemotePorts)
		}
		if r.LocalPorts != "" && r.LocalPorts != "*" {
			return fmt.Errorf("localPorts %q (want empty/any)", r.LocalPorts)
		}
		if !strings.EqualFold(r.RemoteAddresses, winapi.FirewallRemoteAddresses) {
			return fmt.Errorf("remoteAddresses %q", r.RemoteAddresses)
		}
		if r.Grouping != winapi.FirewallGrouping {
			return fmt.Errorf("grouping %q", r.Grouping)
		}
		if r.Direction != 1 {
			return fmt.Errorf("direction %d", r.Direction)
		}
		if r.Action != 1 {
			return fmt.Errorf("action %d", r.Action)
		}
		if !r.Enabled {
			return fmt.Errorf("enabled=false")
		}
		if r.EdgeTraversal {
			return fmt.Errorf("edgeTraversal=true")
		}
		if r.Profiles != int32(winapi.FirewallProfiles) {
			return fmt.Errorf("profiles %#x want Domain|Private (%#x), Public excluded", uint32(r.Profiles), winapi.FirewallProfiles)
		}
		if err := winapi.AddSQLBrowserFirewallRule(app); err != nil {
			return err
		}
		list, err = winapi.ListFirewallRulesNamed(name)
		if err != nil {
			return err
		}
		if len(list) != 1 {
			return fmt.Errorf("after second add: %d rules", len(list))
		}
		if err := winapi.RemoveSQLBrowserFirewallRule(); err != nil {
			return err
		}
		list, err = winapi.ListFirewallRulesNamed(name)
		if err != nil {
			return err
		}
		if len(list) != 0 {
			return fmt.Errorf("after remove: %d rules", len(list))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("firewall COM: %v", err)
	}
	if winapi.FwPolicy2GetCurrentProfileTypes != 7 || winapi.FwPolicy2GetRules != 18 || winapi.FwRulesAdd != 8 || winapi.FwRulesRemove != 9 ||
		winapi.FwRulePutName != 8 || winapi.FwRulePutApplicationName != 12 ||
		winapi.FwRulePutProtocol != 16 || winapi.FwRulePutRemotePorts != 20 ||
		winapi.FwRulePutDirection != 28 || winapi.FwRulePutEnabled != 34 ||
		winapi.FwRulePutProfiles != 38 || winapi.FwRulePutAction != 42 {
		t.Fatal("vtable indices changed")
	}
	// Official netfw.h GUIDs (IID_INetFwPolicy2 must be 8D81-DEFCD3F03186).
	if winapi.CLSID_NetFwPolicy2.Data1 != 0xE2B3C97F ||
		winapi.IID_INetFwPolicy2.Data4 != [8]byte{0x8D, 0x81, 0xDE, 0xFC, 0xD3, 0xF0, 0x31, 0x86} ||
		winapi.IID_INetFwRule.Data1 != 0xAF230D27 ||
		winapi.IID_INetFwRules.Data1 != 0x9C4C6277 {
		t.Fatal("GUIDs changed")
	}
	if winapi.FirewallProtocolUDP != 17 || winapi.FirewallRemotePorts != "1434" ||
		winapi.FirewallLocalPorts != "" || winapi.FirewallRemoteAddresses != "LocalSubnet" ||
		winapi.FirewallGrouping != "Exchange Checker" || winapi.FirewallProfiles != 0x3 ||
		winapi.FirewallRuleName != "Exchange Checker (SQL Browser)" {
		t.Fatal("firewall constants changed")
	}
	if FirewallProfilePublic != winapi.FirewallProfilePublic {
		t.Fatal("wininstall.FirewallProfilePublic != winapi")
	}
	if FirewallRuleName != winapi.FirewallRuleName {
		t.Fatalf("wininstall.FirewallRuleName %q != winapi", FirewallRuleName)
	}
}
