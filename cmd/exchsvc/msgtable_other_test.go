//go:build !windows

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
)

// TestCHK_SVC_02_MessageTable — static parse of rsrc_windows_amd64.syso:
// COFF .rsrc -> resource directory -> RT_MESSAGETABLE (11) ->
// MESSAGE_RESOURCE_DATA; every alert.EventIDs() entry has text "%1\r\n".
func TestCHK_SVC_02_MessageTable(t *testing.T) {
	b, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if len(b) < 20 || le.Uint16(b) != 0x8664 {
		t.Fatal("CHK-SVC-02: not an amd64 COFF object")
	}
	nsec, optSz := int(le.Uint16(b[2:])), int(le.Uint16(b[16:]))
	var rsrc []byte
	for i := 0; i < nsec; i++ {
		h := 20 + optSz + 40*i
		if h+40 > len(b) {
			t.Fatal("CHK-SVC-02: truncated section table")
		}
		name := string(bytes.TrimRight(b[h:h+8], "\x00"))
		size, off := int(le.Uint32(b[h+16:])), int(le.Uint32(b[h+20:]))
		if name == ".rsrc" || bytes.HasPrefix(b[h:h+8], []byte(".rsrc$")) {
			if off+size > len(b) {
				t.Fatal("CHK-SVC-02: truncated .rsrc")
			}
			rsrc = b[off : off+size]
			break
		}
	}
	if rsrc == nil {
		t.Fatal("CHK-SVC-02: no .rsrc section")
	}
	u32 := func(o int) uint32 {
		if o < 0 || o+4 > len(rsrc) {
			t.Fatalf("CHK-SVC-02: offset %d out of .rsrc", o)
		}
		return le.Uint32(rsrc[o:])
	}
	// entries of a resource directory at o: numeric id -> OffsetToData;
	// named entries (high bit of Name set) are skipped (Вика)
	dir := func(o int) map[uint32]uint32 {
		if o < 0 || o+16 > len(rsrc) {
			t.Fatalf("CHK-SVC-02: directory at %d out of .rsrc", o)
		}
		n := int(le.Uint16(rsrc[o+12:])) + int(le.Uint16(rsrc[o+14:]))
		if o+16+8*n > len(rsrc) {
			t.Fatalf("CHK-SVC-02: directory at %d: %d entries out of .rsrc", o, n)
		}
		m := map[uint32]uint32{}
		for i := 0; i < n; i++ {
			name := u32(o + 16 + 8*i)
			if name&0x80000000 != 0 {
				continue
			}
			m[name] = u32(o + 16 + 8*i + 4)
		}
		return m
	}
	const subdir = 0x80000000
	types := dir(0)
	mt, ok := types[11]
	if !ok || mt&subdir == 0 {
		t.Fatal("CHK-SVC-02: no RT_MESSAGETABLE directory")
	}
	found := map[uint32]string{}
	for _, nameOff := range dir(int(mt &^ subdir)) {
		if nameOff&subdir == 0 {
			t.Fatal("CHK-SVC-02: name level is not a directory")
		}
		for lang, de := range dir(int(nameOff &^ subdir)) {
			if de&subdir != 0 {
				t.Fatal("CHK-SVC-02: language level is not a data entry")
			}
			if lang != 0 {
				t.Errorf("CHK-SVC-02: message table language %#x, want LANG_NEUTRAL", lang)
			}
			doff, dsz := int(u32(int(de))), int(u32(int(de)+4))
			if doff < 0 || dsz < 4 || doff+dsz > len(rsrc) {
				t.Fatal("CHK-SVC-02: data out of .rsrc")
			}
			d := rsrc[doff : doff+dsz]
			nb := int(le.Uint32(d))
			if nb < 0 || 4+12*nb > len(d) {
				t.Fatalf("CHK-SVC-02: %d message blocks out of the resource", nb)
			}
			for i := 0; i < nb; i++ {
				lo, hi, eo := le.Uint32(d[4+12*i:]), le.Uint32(d[8+12*i:]), int(le.Uint32(d[12+12*i:]))
				if hi < lo || hi-lo > 0xFFFF {
					t.Fatalf("CHK-SVC-02: bad block %d..%d", lo, hi)
				}
				for id := lo; id <= hi; id++ {
					if eo < 0 || eo+4 > len(d) {
						t.Fatalf("CHK-SVC-02: entry of id %d out of the resource", id)
					}
					l, fl := int(le.Uint16(d[eo:])), le.Uint16(d[eo+2:])
					if l < 4 || eo+l > len(d) {
						t.Fatalf("CHK-SVC-02: bad entry for id %d", id)
					}
					txt := d[eo+4 : eo+l]
					var s string
					if fl&1 != 0 {
						u := make([]uint16, len(txt)/2)
						for k := range u {
							u[k] = le.Uint16(txt[2*k:])
						}
						s = string(utf16.Decode(u))
					} else {
						s = string(txt)
					}
					found[id] = string(bytes.TrimRight([]byte(s), "\x00"))
					eo += l
				}
			}
		}
	}
	for _, id := range alert.EventIDs() {
		s, ok := found[id]
		if !ok {
			t.Errorf("CHK-SVC-02: event ID %d missing from the message table", id)
		} else if s != "%1\r\n" {
			t.Errorf("CHK-SVC-02: event ID %d text %q, want %q", id, s, "%1\r\n")
		}
	}
}
