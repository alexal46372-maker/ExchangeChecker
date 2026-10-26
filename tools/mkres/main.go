// Command mkres generates the committed Windows resource objects
// (rsrc_windows_amd64.syso) of ExchangeCheckerSetup.exe (cmd/exchsetup),
// ExchangeCheckerSvc.exe (cmd/exchsvc) and ExchangeCheckerTray.exe
// (cmd/exchtray, phase 7e T3) with internal/winres — standard
// library only, no rc.exe / mc.exe / third-party generator (PLAN §12 п. 51,
// phase 7a, decision 10). It is a developer / CI tool, not part of the
// product graph.
//
//	go run ./tools/mkres                 # rewrite every .syso for version.Version (0.0.0-dev)
//	go run ./tools/mkres -version 0.0.0-ci.42   # release-like build in CI (not committed)
//	go run ./tools/mkres -check          # exit 1 if a committed .syso is stale
//
// The tray replaced its akavel/rsrc .syso of phase 6a in phase 7e (T3):
// the same manifest and icon, plus VERSIONINFO.
//
// Check ID: CHK-RES-01, CHK-VER-01.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

// IconDir holds the generated .ico files of the tray (phase 11, PLAN §12
// p.144, CHK-TRAY-49), embedded by internal/wintray.
const IconDir = "internal/wintray/icons"

// icons returns the generated .ico files by path relative to the root.
func icons() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, n := range trayui.IconNames() {
		b, err := trayui.IconICO(n)
		if err != nil {
			return nil, err
		}
		out[IconDir+"/"+n+".ico"] = b
	}
	return out, nil
}

// SysoName is the object the Go linker picks up for windows/amd64 only.
const SysoName = "rsrc_windows_amd64.syso"

// target describes the resources of one executable.
type target struct {
	Dir         string // package directory, relative to the module root
	Manifest    string // manifest file in Dir
	Icon        string // .ico relative to the module root ("" = none)
	Description string // FileDescription
	Internal    string // InternalName
	Original    string // OriginalFilename
	Messages    bool   // RT_MESSAGETABLE with the Event Log IDs
}

func targets() []target {
	return []target{
		{
			Dir: "cmd/exchsetup", Manifest: "exchsetup.manifest",
			Icon:        "cmd/exchtray/exchtray.ico", // decision 5: setup uses the tray icon
			Description: version.ProductName + " Setup", Internal: "exchsetup", Original: config.SetupExeName,
		},
		{
			Dir: "cmd/exchsvc", Manifest: "exchsvc.manifest",
			Description: version.ProductName + " Service", Internal: "exchsvc", Original: config.ServiceExeName,
			Messages: true,
		},
		{
			Dir: "cmd/exchtray", Manifest: "exchtray.manifest",
			Icon:        "cmd/exchtray/exchtray.ico",
			Description: version.ProductName + " Tray", Internal: "exchtray", Original: config.TrayExeName,
		},
	}
}

// messageText: the service writes the whole localized text as the single
// insertion string, so every message is just %1 (PLAN-phase7 §3.2, S4).
const messageText = "%1\r\n"

// build returns the .syso bytes of t for product version ver.
func build(root string, t target, ver string) ([]byte, error) {
	fv, pre, err := winres.ParseVersion(ver)
	if err != nil {
		return nil, err
	}
	man, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(t.Dir), t.Manifest))
	if err != nil {
		return nil, err
	}
	// a Windows checkout may have CRLF; the resource is always the LF text
	man = bytes.ReplaceAll(bytes.TrimSpace(man), []byte("\r\n"), []byte("\n"))
	man = append(man, '\n')
	res := []winres.Resource{{Type: winres.TypeManifest, ID: 1, Lang: winres.LangEnUS, Data: man}}
	if t.Icon != "" {
		ico, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(t.Icon)))
		if err != nil {
			return nil, err
		}
		icons, err := winres.IconResources(ico, 1, 1, winres.LangEnUS)
		if err != nil {
			return nil, err
		}
		res = append(res, icons...)
	}
	vi, err := winres.VersionInfo{
		FileVersion: fv, ProductVersion: fv, Prerelease: pre,
		Strings: []winres.KV{
			// decision 5: CompanyName and LegalCopyright are empty and not written
			{Key: "CompanyName", Value: ""},
			{Key: "FileDescription", Value: t.Description},
			{Key: "FileVersion", Value: ver},
			{Key: "InternalName", Value: t.Internal},
			{Key: "LegalCopyright", Value: ""},
			{Key: "OriginalFilename", Value: t.Original},
			{Key: "ProductName", Value: version.ProductName},
			{Key: "ProductVersion", Value: ver},
		},
	}.Bytes()
	if err != nil {
		return nil, err
	}
	res = append(res, winres.Resource{Type: winres.TypeVersion, ID: 1, Lang: winres.LangEnUS, Data: vi})
	if t.Messages {
		msgs := map[uint32]string{}
		for _, id := range alert.EventIDs() { // S1, EC Service: the single list of Event Log IDs
			msgs[id] = messageText
		}
		mt, err := winres.MessageTable(msgs)
		if err != nil {
			return nil, err
		}
		// LANG_NEUTRAL: FormatMessage(…, language 0) finds it first
		res = append(res, winres.Resource{Type: winres.TypeMessageTable, ID: 1, Lang: winres.LangNeutral, Data: mt})
	}
	return winres.BuildSyso(res)
}

func main() {
	root := flag.String("root", ".", "module root")
	ver := flag.String("version", version.Version, "product version written to VERSIONINFO")
	check := flag.Bool("check", false, "compare with the committed .syso instead of writing")
	flag.Parse()
	stale := false
	for _, t := range targets() {
		out, err := build(*root, t, *ver)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mkres: %s: %v\n", t.Dir, err)
			os.Exit(2)
		}
		path := filepath.Join(*root, filepath.FromSlash(t.Dir), SysoName)
		if *check {
			old, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(old, out) {
				fmt.Fprintf(os.Stderr, "mkres: %s is stale (run go run ./tools/mkres)\n", path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "mkres: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("mkres: %s (%d bytes, version %s)\n", path, len(out), *ver)
	}
	ics, err := icons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkres: icons: %v\n", err)
		os.Exit(2)
	}
	if !*check {
		if err := os.MkdirAll(filepath.Join(*root, filepath.FromSlash(IconDir)), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkres: %v\n", err)
			os.Exit(2)
		}
	}
	for _, rel := range sortedKeys(ics) {
		path := filepath.Join(*root, filepath.FromSlash(rel))
		if *check {
			if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, ics[rel]) {
				fmt.Fprintf(os.Stderr, "mkres: %s is stale (run go run ./tools/mkres)\n", path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, ics[rel], 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "mkres: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("mkres: %s (%d bytes)\n", path, len(ics[rel]))
	}
	if stale {
		os.Exit(1)
	}
}

func sortedKeys(m map[string][]byte) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
