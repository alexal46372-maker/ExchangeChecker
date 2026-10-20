//go:build windows

package winapi

import (
	"path/filepath"
	"testing"
	"unsafe"
)

// addTestFwRule adds a minimal inbound UDP rule with the given name
// (duplicates allowed: INetFwRules.Add does not dedupe by name).
func addTestFwRule(t *testing.T, rules *comObject, name, app string) {
	t.Helper()
	rule, err := newFwRule()
	if err != nil {
		t.Fatal(err)
	}
	defer comCall(rule, vtRelease)
	if err := putBSTR(rule, FwRulePutName, "put_Name", name); err != nil {
		t.Fatal(err)
	}
	if err := putBSTR(rule, FwRulePutApplicationName, "put_ApplicationName", app); err != nil {
		t.Fatal(err)
	}
	if err := hrErr("put_Protocol", comCall(rule, FwRulePutProtocol, uintptr(FirewallProtocolUDP))); err != nil {
		t.Fatal(err)
	}
	if err := putBSTR(rule, FwRulePutRemotePorts, "put_RemotePorts", "1434"); err != nil {
		t.Fatal(err)
	}
	if err := hrErr("put_Direction", comCall(rule, FwRulePutDirection, uintptr(netFwRuleDirIn))); err != nil {
		t.Fatal(err)
	}
	if err := hrErr("put_Action", comCall(rule, FwRulePutAction, uintptr(netFwActionAllow))); err != nil {
		t.Fatal(err)
	}
	if err := hrErr("put_Enabled", comCall(rule, FwRulePutEnabled, uintptr(variantFalse))); err != nil {
		t.Fatal(err)
	}
	if err := hrErr("INetFwRules.Add", comCall(rules, FwRulesAdd, uintptr(unsafe.Pointer(rule)))); err != nil {
		t.Fatal(err)
	}
}

func countNamed(t *testing.T, rules *comObject, name string) int {
	t.Helper()
	all, err := listFwRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range all {
		if r.Name == name {
			n++
		}
	}
	return n
}

// TestCHK_SETUP_13_RemoveDuplicates — two rules with the same name are
// both removed by fwRemoveByName (Remove loops while Item finds one), and
// Item on a missing name is "not found" (0x80070002), not an error.
func TestCHK_SETUP_13_RemoveDuplicates(t *testing.T) {
	running, err := FirewallServiceRunning()
	if err != nil {
		t.Fatalf("query MpsSvc: %v", err)
	}
	if !running {
		t.Skip("MpsSvc (Windows Firewall) is not SERVICE_RUNNING; cannot exercise INetFwPolicy2")
	}
	win, err := WindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(win, "notepad.exe")
	const name = "Exchange Checker (CHK-SETUP-13 duplicate test)"
	err = WithCOM(func() error {
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
		defer fwRemoveByName(rules, name)

		if err := fwRemoveByName(rules, name); err != nil {
			return err
		}
		addTestFwRule(t, rules, name, app)
		addTestFwRule(t, rules, name, app)
		if n := countNamed(t, rules, name); n != 2 {
			t.Fatalf("after two Adds: %d rules named %q, want 2", n, name)
		}
		if err := fwRemoveByName(rules, name); err != nil {
			return err
		}
		if n := countNamed(t, rules, name); n != 0 {
			t.Fatalf("after remove: %d rules named %q, want 0", n, name)
		}
		if _, ok, err := itemByName(rules, name); err != nil || ok {
			t.Fatalf("Item(missing): ok=%v err=%v, want not found", ok, err)
		}
		t.Logf("firewall ok: duplicate rules named %q: 2 added, 0 left after remove", name)
		return nil
	})
	if err != nil {
		t.Fatalf("firewall COM: %v", err)
	}
}
