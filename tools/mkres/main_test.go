package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

const root = "../.."

// TestCHK_RES_01_CommittedSysoFresh — the committed .syso files equal the
// generator output for the default version 0.0.0-dev (rebuild with
// `go run ./tools/mkres` after editing a manifest, the icon or this tool).
func TestCHK_RES_01_CommittedSysoFresh(t *testing.T) {
	for _, tg := range targets() {
		out, err := build(root, tg, "0.0.0-dev")
		if err != nil {
			t.Fatal(err)
		}
		old, err := os.ReadFile(filepath.Join(root, tg.Dir, SysoName))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(old, out) {
			t.Errorf("%s/%s is stale: run go run ./tools/mkres", tg.Dir, SysoName)
		}
	}
}

// TestCHK_RES_01_Targets — what each target carries (manifest, icon,
// VERSIONINFO strings without empty CompanyName / LegalCopyright, message
// table of the service).
func TestCHK_RES_01_Targets(t *testing.T) {
	for _, tg := range targets() {
		out, err := build(root, tg, "1.4.2")
		if err != nil {
			t.Fatal(err)
		}
		res, err := winres.ParseSyso(out)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := winres.Find(res, winres.TypeManifest); !ok {
			t.Errorf("%s: no manifest", tg.Dir)
		}
		_, icon := winres.Find(res, winres.TypeGroupIcon)
		if icon != (tg.Icon != "") {
			t.Errorf("%s: group icon %v", tg.Dir, icon)
		}
		v, _ := winres.Find(res, winres.TypeVersion)
		pv, err := winres.ParseVersionInfo(v.Data)
		if err != nil {
			t.Fatal(err)
		}
		if pv.FileVersion != (winres.FileVersion{1, 4, 2, 0}) || pv.Strings["ProductVersion"] != "1.4.2" ||
			pv.Strings["OriginalFilename"] != tg.Original || pv.Strings["ProductName"] != "Exchange Checker" || pv.FileFlags != 0 {
			t.Errorf("%s: version %+v", tg.Dir, pv)
		}
		for _, k := range []string{"CompanyName", "LegalCopyright"} {
			if _, ok := pv.Strings[k]; ok {
				t.Errorf("%s: %s must not be written (decision 5)", tg.Dir, k)
			}
		}
		m, ok := winres.Find(res, winres.TypeMessageTable)
		if ok != tg.Messages {
			t.Errorf("%s: message table %v", tg.Dir, ok)
		}
		if ok {
			msgs, err := winres.ParseMessageTable(m.Data)
			if err != nil || len(msgs) != len(alert.EventIDs()) {
				t.Fatalf("message table %v %v", msgs, err)
			}
			for _, id := range alert.EventIDs() {
				if msgs[id] != "%1\r\n" {
					t.Errorf("message %d = %q", id, msgs[id])
				}
			}
		}
	}
	if _, err := build(root, targets()[0], "v1.0.0"); err == nil {
		t.Error("a tag with v accepted as version")
	}
}

// TestCHK_RES_01_MessageTableIDs — the service message table holds exactly
// alert.EventIDs() (S1; CHK-SVC-02 keeps that list complete).
func TestCHK_RES_01_MessageTableIDs(t *testing.T) {
	for _, tg := range targets() {
		if !tg.Messages {
			continue
		}
		out, err := build(root, tg, "0.0.0-dev")
		if err != nil {
			t.Fatal(err)
		}
		res, _ := winres.ParseSyso(out)
		m, _ := winres.Find(res, winres.TypeMessageTable)
		if m.Lang != winres.LangNeutral {
			t.Errorf("message table language %#x, want LANG_NEUTRAL", m.Lang)
		}
		msgs, err := winres.ParseMessageTable(m.Data)
		if err != nil {
			t.Fatal(err)
		}
		var got []uint32
		for id := range msgs {
			got = append(got, id)
		}
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		if want := alert.EventIDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("message table IDs %v, want alert.EventIDs() %v", got, want)
		}
	}
}

// TestCHK_RES_01_TrayTarget — phase 7e T3: the tray .syso keeps the 6a
// manifest (the LF text of exchtray.manifest) and every image of
// exchtray.ico, and adds VERSIONINFO for config.TrayExeName.
func TestCHK_RES_01_TrayTarget(t *testing.T) {
	var tg *target
	for _, x := range targets() {
		if x.Dir == "cmd/exchtray" {
			x := x
			tg = &x
		}
	}
	if tg == nil || tg.Original != "ExchangeCheckerTray.exe" || tg.Messages {
		t.Fatalf("tray target %+v", tg)
	}
	out, err := build(root, *tg, "0.0.0-dev")
	if err != nil {
		t.Fatal(err)
	}
	res, err := winres.ParseSyso(out)
	if err != nil {
		t.Fatal(err)
	}
	man, _ := os.ReadFile(filepath.Join(root, "cmd", "exchtray", "exchtray.manifest"))
	man = append(bytes.ReplaceAll(bytes.TrimSpace(man), []byte("\r\n"), []byte("\n")), '\n')
	if m, ok := winres.Find(res, winres.TypeManifest); !ok || !bytes.Equal(m.Data, man) {
		t.Error("tray manifest differs from exchtray.manifest")
	}
	ico, _ := os.ReadFile(filepath.Join(root, "cmd", "exchtray", "exchtray.ico"))
	want, err := winres.IconResources(ico, 1, 1, winres.LangEnUS)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range want {
		for _, r := range res {
			if r.Type == w.Type && r.ID == w.ID && bytes.Equal(r.Data, w.Data) {
				n++
			}
		}
	}
	if n != len(want) || len(want) != 4 {
		t.Errorf("tray icon resources: %d of %d", n, len(want))
	}
	v, ok := winres.Find(res, winres.TypeVersion)
	if !ok {
		t.Fatal("no VERSIONINFO")
	}
	pv, err := winres.ParseVersionInfo(v.Data)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Strings["FileDescription"] != "Exchange Checker Tray" || pv.Strings["InternalName"] != "exchtray" ||
		pv.Strings["OriginalFilename"] != "ExchangeCheckerTray.exe" || pv.FileFlags&2 == 0 {
		t.Errorf("tray VERSIONINFO %+v", pv)
	}
}
