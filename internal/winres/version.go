package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// FileVersion is the four-part numeric version of VS_FIXEDFILEINFO.
type FileVersion [4]uint16

func (v FileVersion) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
}

// ParseVersion maps a product version (internal/version.Version, semver
// without a leading "v": "1.2.3", "1.2.3-rc.1", "0.0.0-ci.42",
// "0.0.0-dev") to FILEVERSION (PLAN §12, phase 7, decision 6):
// X.Y.Z → X,Y,Z,0; the pre-release "ci.N" → X,Y,Z,N; any pre-release sets
// VS_FF_PRERELEASE. "ci.N" with N not a number in 0..65535 is an error.
// Build metadata ("+…") is ignored.
func ParseVersion(s string) (v FileVersion, prerelease bool, err error) {
	bad := fmt.Errorf("winres: version %q is not X.Y.Z[-pre]", s)
	core := s
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	pre := ""
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core, pre = core[:i], core[i+1:]
		if pre == "" {
			return v, false, bad
		}
		prerelease = true
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false, bad
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || (len(p) > 1 && p[0] == '0') {
			return v, false, bad
		}
		v[i] = uint16(n)
	}
	for _, kind := range []string{"ci.", "rel."} {
		if !strings.HasPrefix(pre, kind) {
			continue
		}
		// CI builds 0.0.0-ci.<run_number>, manual release builds
		// (workflow_dispatch) 0.0.0-rel.<run_number>; N must fit the
		// fourth part
		n, err := strconv.ParseUint(pre[len(kind):], 10, 16)
		if err != nil {
			return FileVersion{}, false, fmt.Errorf("winres: version %q: %s build number must be 0..65535", s, strings.TrimSuffix(kind, "."))
		}
		v[3] = uint16(n)
	}
	return v, prerelease, nil
}

// VersionInfo describes an RT_VERSION resource. Empty strings are not
// written (decision 5: CompanyName / LegalCopyright stay unset).
type VersionInfo struct {
	FileVersion    FileVersion
	ProductVersion FileVersion
	Prerelease     bool
	// Strings: StringFileInfo\040904B0 (English, Unicode), written in this
	// key order.
	Strings []KV
}

// KV is one StringFileInfo entry.
type KV struct{ Key, Value string }

const (
	vsFFIsignature  = 0xFEEF04BD
	vsFFPrerelease  = 0x2
	vosNTWindows32  = 0x00040004
	vftApp          = 0x1
	translationLang = 0x0409
	translationCP   = 0x04B0
	stringTableKey  = "040904B0"
)

// node writes one version block: wLength, wValueLength, wType, key, pad,
// value, pad, children.
func node(key string, valueLen uint16, typ uint16, value []byte, children ...[]byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.Write([]byte{0, 0})
	binary.Write(&b, le, valueLen)
	binary.Write(&b, le, typ)
	for _, c := range utf16.Encode([]rune(key)) {
		binary.Write(&b, le, c)
	}
	b.Write([]byte{0, 0})
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	le.PutUint16(out, uint16(len(out)))
	return out
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func utf16z(s string) []byte {
	u := append(utf16.Encode([]rune(s)), 0)
	out := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(out[2*i:], c)
	}
	return out
}

// Bytes returns the VS_VERSIONINFO block.
func (vi VersionInfo) Bytes() ([]byte, error) {
	le := binary.LittleEndian
	fixed := make([]byte, 52)
	flags := uint32(0)
	if vi.Prerelease {
		flags = vsFFPrerelease
	}
	ms := func(v FileVersion) uint32 { return uint32(v[0])<<16 | uint32(v[1]) }
	ls := func(v FileVersion) uint32 { return uint32(v[2])<<16 | uint32(v[3]) }
	for i, x := range []uint32{vsFFIsignature, 0x00010000,
		ms(vi.FileVersion), ls(vi.FileVersion), ms(vi.ProductVersion), ls(vi.ProductVersion),
		0x3F, flags, vosNTWindows32, vftApp, 0, 0, 0} {
		le.PutUint32(fixed[4*i:], x)
	}
	var strs [][]byte
	seen := map[string]bool{}
	for _, kv := range vi.Strings {
		if kv.Key == "" || seen[kv.Key] {
			return nil, fmt.Errorf("winres: bad or duplicate version string key %q", kv.Key)
		}
		seen[kv.Key] = true
		if kv.Value == "" {
			continue
		}
		v := utf16z(kv.Value)
		strs = append(strs, node(kv.Key, uint16(len(v)/2), 1, v))
	}
	if len(strs) == 0 {
		return nil, errors.New("winres: no version strings")
	}
	table := node(stringTableKey, 0, 1, nil, strs...)
	sfi := node("StringFileInfo", 0, 1, nil, table)
	tr := make([]byte, 4)
	le.PutUint16(tr, translationLang)
	le.PutUint16(tr[2:], translationCP)
	vfi := node("VarFileInfo", 0, 1, nil, node("Translation", 4, 0, tr))
	return node("VS_VERSION_INFO", 52, 0, fixed, sfi, vfi), nil
}

// ParsedVersion is a decoded RT_VERSION resource.
type ParsedVersion struct {
	FileVersion, ProductVersion FileVersion
	FileFlags                   uint32
	Strings                     map[string]string // StringFileInfo\040904B0
	Translation                 []uint16
}

type block struct {
	key      string
	typ      uint16
	value    []byte
	children []block
}

func parseBlock(b []byte) (block, int, error) {
	le := binary.LittleEndian
	bad := errors.New("winres: malformed version block")
	if len(b) < 6 {
		return block{}, 0, bad
	}
	total, vlen, typ := int(le.Uint16(b)), int(le.Uint16(b[2:])), le.Uint16(b[4:])
	if total < 6 || total > len(b) {
		return block{}, 0, bad
	}
	b = b[:total]
	p := 6
	var key []uint16
	for {
		if p+2 > total {
			return block{}, 0, bad
		}
		c := le.Uint16(b[p:])
		p += 2
		if c == 0 {
			break
		}
		key = append(key, c)
	}
	p = align(p, 4)
	n := vlen
	if typ == 1 {
		n = 2 * vlen
	}
	if p+n > total {
		return block{}, 0, bad
	}
	out := block{key: string(utf16.Decode(key)), typ: typ, value: b[p : p+n]}
	p += n
	for {
		p = align(p, 4)
		if p >= total {
			break
		}
		c, used, err := parseBlock(b[p:])
		if err != nil {
			return block{}, 0, err
		}
		out.children = append(out.children, c)
		p += used
	}
	return out, total, nil
}

// ParseVersionInfo decodes VS_VERSIONINFO.
func ParseVersionInfo(data []byte) (ParsedVersion, error) {
	le := binary.LittleEndian
	root, _, err := parseBlock(data)
	if err != nil {
		return ParsedVersion{}, err
	}
	if root.key != "VS_VERSION_INFO" || len(root.value) != 52 || le.Uint32(root.value) != vsFFIsignature {
		return ParsedVersion{}, errors.New("winres: not a VS_VERSIONINFO")
	}
	f := root.value
	unpack := func(ms, ls uint32) FileVersion {
		return FileVersion{uint16(ms >> 16), uint16(ms), uint16(ls >> 16), uint16(ls)}
	}
	pv := ParsedVersion{
		FileVersion:    unpack(le.Uint32(f[8:]), le.Uint32(f[12:])),
		ProductVersion: unpack(le.Uint32(f[16:]), le.Uint32(f[20:])),
		FileFlags:      le.Uint32(f[28:]),
		Strings:        map[string]string{},
	}
	for _, c := range root.children {
		switch c.key {
		case "StringFileInfo":
			for _, t := range c.children {
				if t.key != stringTableKey {
					continue
				}
				for _, s := range t.children {
					u := make([]uint16, len(s.value)/2)
					for i := range u {
						u[i] = le.Uint16(s.value[2*i:])
					}
					for len(u) > 0 && u[len(u)-1] == 0 {
						u = u[:len(u)-1]
					}
					pv.Strings[s.key] = string(utf16.Decode(u))
				}
			}
		case "VarFileInfo":
			for _, v := range c.children {
				if v.key == "Translation" {
					for i := 0; i+1 < len(v.value); i += 2 {
						pv.Translation = append(pv.Translation, le.Uint16(v.value[i:]))
					}
				}
			}
		}
	}
	return pv, nil
}
