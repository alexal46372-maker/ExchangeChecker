package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_91_ToggleItemByProfileID — every database item carries the
// ProfileID it was built for and its shown state (Checked); ToggleItem
// finds the item of a check mark command in the shown items, so a list
// that changed after the menu was built (a profile inserted in front,
// the order changed) does not move the command to another database.
func TestCHK_TRAY_91_ToggleItemByProfileID(t *testing.T) {
	yes := true
	mk := func(ps ...model.ProfileSnapshot) []Item {
		snap := model.StatusSnapshot{Profiles: ps}
		st := connected(&snap)
		st.Capabilities = append(st.Capabilities, model.CapProfileEnabled)
		st.ClientElevated = &yes
		return BuildLeftMenu(st)
	}
	a := model.ProfileSnapshot{ProfileID: "a", Name: "A", Enabled: true, Polled: true}
	b := model.ProfileSnapshot{ProfileID: "b", Name: "B", Enabled: false, Polled: true}
	z := model.ProfileSnapshot{ProfileID: "z", Name: "Z", Enabled: true, Polled: true}
	shown := mk(a, b)
	dbs := databases(shown)
	if len(dbs) != 2 || dbs[0].ProfileID != "a" || dbs[1].ProfileID != "b" || !dbs[0].Checked || dbs[1].Checked {
		t.Fatalf("items %+v", dbs)
	}
	later := databases(mk(z, b, a)) // the list changed while the menu was open
	if later[1].ID != dbs[1].ID || later[1].ProfileID != "b" || later[0].ProfileID != "z" {
		t.Fatalf("later %+v", later)
	}
	it, ok := ToggleItem(shown, dbs[1].ID)
	if !ok || it.ProfileID != "b" || it.Checked {
		t.Fatalf("ToggleItem(b) = %+v %v", it, ok)
	}
	it, ok = ToggleItem(shown, dbs[0].ID)
	if !ok || it.ProfileID != "a" || !it.Checked {
		t.Fatalf("ToggleItem(a) = %+v %v", it, ok)
	}
	if _, ok := ToggleItem(shown, ToggleCmd(5)); ok {
		t.Error("a command that is not in the shown menu")
	}
	if _, ok := ToggleItem(shown, CmdSettings); ok {
		t.Error("not a check mark command")
	}
}
