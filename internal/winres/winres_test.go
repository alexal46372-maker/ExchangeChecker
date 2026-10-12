package winres

import (
	"bytes"
	"debug/pe"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sample(t *testing.T) []Resource {
	t.Helper()
	ico, err := os.ReadFile(filepath.Join("..", "..", "cmd", "exchtray", "exchtray.ico"))
	if err != nil {
		t.Fatal(err)
	}
	icons, err := IconResources(ico, 1, 1, LangEnUS)
	if err != nil {
		t.Fatal(err)
	}
	vi, err := VersionInfo{FileVersion: FileVersion{1, 2, 3, 0}, ProductVersion: FileVersion{1, 2, 3, 0},
		Strings: []KV{{"FileDescription", "Тест"}, {"CompanyName", ""}, {"ProductVersion", "1.2.3"}}}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	mt, err := MessageTable(map[uint32]string{100: "%1\r\n", 101: "%1\r\n", 110: "x %1\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	res := append(icons,
		Resource{Type: TypeManifest, ID: 1, Lang: LangEnUS, Data: []byte("<assembly/>")},
		Resource{Type: TypeVersion, ID: 1, Lang: LangEnUS, Data: vi},
		Resource{Type: TypeMessageTable, ID: 1, Lang: LangEnUS, Data: mt})
	return res
}

// TestCHK_RES_01_RoundTrip — write → parse returns every resource byte for byte.
func TestCHK_RES_01_RoundTrip(t *testing.T) {
	res := sample(t)
	obj, err := BuildSyso(res)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSyso(obj)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(res) {
		t.Fatalf("%d resources, want %d", len(got), len(res))
	}
	key := func(r Resource) [3]uint16 { return [3]uint16{r.Type, r.ID, r.Lang} }
	want := map[[3]uint16][]byte{}
	for _, r := range res {
		want[key(r)] = r.Data
	}
	for _, r := range got {
		if !bytes.Equal(want[key(r)], r.Data) {
			t.Errorf("resource %v differs", key(r))
		}
	}
	// deterministic
	obj2, _ := BuildSyso(append([]Resource(nil), res[len(res)-1], res[0]))
	obj3, _ := BuildSyso(append([]Resource(nil), res[0], res[len(res)-1]))
	if !bytes.Equal(obj2, obj3) {
		t.Error("output depends on input order")
	}
}

// TestCHK_RES_01_COFF — debug/pe accepts the object: amd64, one .rsrc
// section (initialized data, read), one ADDR32NB relocation per data entry
// against symbol 0 (.rsrc, static).
func TestCHK_RES_01_COFF(t *testing.T) {
	obj, err := BuildSyso(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		t.Fatal(err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(f.Sections) != 1 || f.OptionalHeader != nil {
		t.Fatalf("header %+v", f.FileHeader)
	}
	s := f.Sections[0]
	if s.Name != ".rsrc" || s.Characteristics != 0x40000040 || len(s.Relocs) != len(sample(t)) {
		t.Fatalf("section %+v relocs %d", s.SectionHeader, len(s.Relocs))
	}
	for _, r := range s.Relocs {
		if r.Type != 3 || r.SymbolTableIndex != 0 {
			t.Errorf("relocation %+v", r)
		}
	}
	if len(f.Symbols) != 1 || f.Symbols[0].Name != ".rsrc" || f.Symbols[0].SectionNumber != 1 || f.Symbols[0].StorageClass != 3 {
		t.Fatalf("symbols %+v", f.Symbols)
	}
}

// TestCHK_RES_01_ParsesRsrcOutput — the parser reads the tray .syso made by
// akavel/rsrc (independent writer): manifest text and all three icons.
func TestCHK_RES_01_ParsesRsrcOutput(t *testing.T) {
	dir := filepath.Join("..", "..", "cmd", "exchtray")
	obj, err := os.ReadFile(filepath.Join(dir, "rsrc_windows_amd64.syso"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseSyso(obj)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := Find(res, TypeManifest)
	man, _ := os.ReadFile(filepath.Join(dir, "exchtray.manifest"))
	if !ok || !bytes.Contains(bytes.ReplaceAll(man, []byte("\r\n"), []byte("\n")), bytes.TrimSpace(m.Data)) {
		t.Error("manifest not found in the tray .syso")
	}
	g, ok := Find(res, TypeGroupIcon)
	if !ok {
		t.Fatal("no group icon")
	}
	ids, err := GroupIconIDs(g.Data)
	if err != nil || len(ids) != 3 {
		t.Fatalf("group icon ids %v %v", ids, err)
	}
}

func TestCHK_RES_01_Icon(t *testing.T) {
	ico, _ := os.ReadFile(filepath.Join("..", "..", "cmd", "exchtray", "exchtray.ico"))
	res, err := IconResources(ico, 7, 10, LangEnUS)
	if err != nil {
		t.Fatal(err)
	}
	g := res[len(res)-1]
	if g.Type != TypeGroupIcon || g.ID != 7 {
		t.Fatalf("group %+v", g)
	}
	ids, _ := GroupIconIDs(g.Data)
	if !reflect.DeepEqual(ids, []uint16{10, 11, 12}) {
		t.Fatalf("ids %v", ids)
	}
	for i, r := range res[:3] {
		if r.Type != TypeIcon || r.ID != ids[i] || !bytes.Contains(ico, r.Data) {
			t.Errorf("icon %d %+v", i, r.ID)
		}
	}
	if _, err := IconResources([]byte("nope"), 1, 1, 0); err == nil {
		t.Error("garbage accepted as .ico")
	}
}

func TestCHK_RES_01_Version(t *testing.T) {
	cases := []struct {
		in   string
		want FileVersion
		pre  bool
	}{
		{"1.2.3", FileVersion{1, 2, 3, 0}, false},
		{"0.0.0-dev", FileVersion{0, 0, 0, 0}, true},
		{"0.0.0-ci.42", FileVersion{0, 0, 0, 42}, true},
		{"0.0.0-ci.65535", FileVersion{0, 0, 0, 65535}, true},
		{"2.0.1-rc.1+abc", FileVersion{2, 0, 1, 0}, true},
	}
	for _, c := range cases {
		v, pre, err := ParseVersion(c.in)
		if err != nil || v != c.want || pre != c.pre {
			t.Errorf("ParseVersion(%q) = %v %v %v", c.in, v, pre, err)
		}
	}
	for _, bad := range []string{"", "v1.2.3", "1.2", "1.2.3.4", "1.02.3", "1.2.3-", "70000.0.0", "a.b.c", "0.0.0-ci.", "0.0.0-ci.x", "0.0.0-ci.65536", "0.0.0-ci.-1", "0.0.0-ci.1.2"} {
		if _, _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted", bad)
		}
	}
	vi := VersionInfo{FileVersion: FileVersion{0, 0, 0, 42}, ProductVersion: FileVersion{0, 0, 0, 42}, Prerelease: true,
		Strings: []KV{{"CompanyName", ""}, {"FileDescription", "Exchange Checker Setup"}, {"LegalCopyright", ""}, {"ProductVersion", "0.0.0-ci.42"}}}
	b, err := vi.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pv, err := ParseVersionInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if pv.FileVersion != vi.FileVersion || pv.ProductVersion != vi.ProductVersion || pv.FileFlags != 2 {
		t.Errorf("fixed info %+v", pv)
	}
	if !reflect.DeepEqual(pv.Strings, map[string]string{"FileDescription": "Exchange Checker Setup", "ProductVersion": "0.0.0-ci.42"}) {
		t.Errorf("strings %v (empty values must not be written)", pv.Strings)
	}
	if !reflect.DeepEqual(pv.Translation, []uint16{0x0409, 0x04B0}) {
		t.Errorf("translation %x", pv.Translation)
	}
	if bytes.Contains(b, utf16z("CompanyName")[:20]) {
		t.Error("empty CompanyName key written")
	}
	if int(b[0])|int(b[1])<<8 != len(b) {
		t.Errorf("wLength %d, len %d", int(b[0])|int(b[1])<<8, len(b))
	}
}

func TestCHK_RES_01_MessageTable(t *testing.T) {
	in := map[uint32]string{100: "%1\r\n", 101: "%1\r\n", 102: "%1\r\n", 103: "%1\r\n", 104: "%1\r\n", 105: "%1\r\n", 110: "%1\r\n"}
	b, err := MessageTable(in)
	if err != nil {
		t.Fatal(err)
	}
	if n := int(b[0]); n != 2 {
		t.Errorf("%d blocks, want 2 (100..105, 110)", n)
	}
	got, err := ParseMessageTable(b)
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Fatalf("parse %v %v", got, err)
	}
	if _, err := MessageTable(map[uint32]string{0x40000064: "x"}); err == nil {
		t.Error("severity bits accepted")
	}
}

func TestCHK_RES_01_Duplicate(t *testing.T) {
	r := Resource{Type: TypeManifest, ID: 1, Lang: LangEnUS, Data: []byte("x")}
	if _, err := BuildSyso([]Resource{r, r}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := ParseSyso([]byte("not coff")); err == nil {
		t.Fatal("garbage parsed")
	}
}
