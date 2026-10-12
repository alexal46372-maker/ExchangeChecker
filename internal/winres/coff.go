package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Resource types (winuser.h).
const (
	TypeIcon         uint16 = 3
	TypeMessageTable uint16 = 11
	TypeGroupIcon    uint16 = 14
	TypeVersion      uint16 = 16
	TypeManifest     uint16 = 24
)

// LangNeutral is LANG_NEUTRAL: FormatMessage with language 0 tries it
// first (message tables).
const LangNeutral uint16 = 0x0000

// LangEnUS is the language of every resource the project generates
// (MAKELANGID(LANG_ENGLISH, SUBLANG_ENGLISH_US)); the loader falls back to it.
const LangEnUS uint16 = 0x0409

// Resource is one resource with a numeric type and name.
type Resource struct {
	Type uint16
	ID   uint16
	Lang uint16
	Data []byte
}

const (
	machineAMD64         = 0x8664
	relAMD64Addr32NB     = 3
	scnInitializedData   = 0x00000040
	scnMemRead           = 0x40000000
	fileLineNumsStripped = 0x0004
	file32BitMachine     = 0x0100
	symClassStatic       = 3
	subdirFlag           = 0x80000000
)

// BuildSyso returns a COFF object for amd64 holding res in one .rsrc
// section. Output is deterministic (TimeDateStamp 0, entries sorted by
// type, ID, language). Duplicate (type, ID, language) is an error.
func BuildSyso(res []Resource) ([]byte, error) {
	rsrc, relocs, err := buildRsrc(res)
	if err != nil {
		return nil, err
	}
	const fileHdr, secHdr, relocSize, symSize = 20, 40, 10, 18
	rawOff := uint32(fileHdr + secHdr)
	relOff := rawOff + uint32(len(rsrc))
	symOff := relOff + uint32(len(relocs)*relocSize)
	if len(relocs) > 0xFFFF {
		return nil, errors.New("winres: too many resources")
	}
	var b bytes.Buffer
	w := func(v interface{}) { binary.Write(&b, binary.LittleEndian, v) }
	// IMAGE_FILE_HEADER
	w(uint16(machineAMD64))
	w(uint16(1)) // NumberOfSections
	w(uint32(0)) // TimeDateStamp
	w(symOff)
	w(uint32(1)) // NumberOfSymbols
	w(uint16(0)) // SizeOfOptionalHeader
	w(uint16(fileLineNumsStripped | file32BitMachine))
	// IMAGE_SECTION_HEADER
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0)) // VirtualSize
	w(uint32(0)) // VirtualAddress
	w(uint32(len(rsrc)))
	w(rawOff)
	w(relOff)
	w(uint32(0)) // PointerToLinenumbers
	w(uint16(len(relocs)))
	w(uint16(0))
	w(uint32(scnInitializedData | scnMemRead))
	b.Write(rsrc)
	for _, off := range relocs {
		w(off)       // VirtualAddress (offset in the section)
		w(uint32(0)) // SymbolTableIndex: the .rsrc section symbol
		w(uint16(relAMD64Addr32NB))
	}
	// symbol 0: .rsrc, section 1, static
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0)) // Value
	w(int16(1))  // SectionNumber
	w(uint16(0)) // Type
	b.WriteByte(symClassStatic)
	b.WriteByte(0) // NumberOfAuxSymbols
	w(uint32(4))   // empty string table
	return b.Bytes(), nil
}

func align(n, a int) int { return (n + a - 1) &^ (a - 1) }

// buildRsrc lays out the three-level resource tree: all directories, then
// all data entries, then the data (8-byte aligned). OffsetToData of each
// data entry holds the section-relative offset; relocs lists the offsets
// of those fields.
func buildRsrc(in []Resource) ([]byte, []uint32, error) {
	res := append([]Resource(nil), in...)
	sort.SliceStable(res, func(i, j int) bool {
		a, b := res[i], res[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Lang < b.Lang
	})
	for i := 1; i < len(res); i++ {
		if res[i].Type == res[i-1].Type && res[i].ID == res[i-1].ID && res[i].Lang == res[i-1].Lang {
			return nil, nil, fmt.Errorf("winres: duplicate resource type %d id %d lang %#x", res[i].Type, res[i].ID, res[i].Lang)
		}
	}
	type nameDir struct {
		id    uint16
		langs []int // indexes into res
	}
	type typeDir struct {
		id    uint16
		names []*nameDir
	}
	var types []*typeDir
	for i, r := range res {
		if len(types) == 0 || types[len(types)-1].id != r.Type {
			types = append(types, &typeDir{id: r.Type})
		}
		t := types[len(types)-1]
		if len(t.names) == 0 || t.names[len(t.names)-1].id != r.ID {
			t.names = append(t.names, &nameDir{id: r.ID})
		}
		n := t.names[len(t.names)-1]
		n.langs = append(n.langs, i)
	}
	dirSize := func(entries int) int { return 16 + 8*entries }
	// offsets: root, type dirs, name dirs (breadth-first)
	off := dirSize(len(types))
	typeOff := make([]int, len(types))
	for i, t := range types {
		typeOff[i] = off
		off += dirSize(len(t.names))
	}
	nameOff := map[*nameDir]int{}
	for _, t := range types {
		for _, n := range t.names {
			nameOff[n] = off
			off += dirSize(len(n.langs))
		}
	}
	entryOff := off
	off += 16 * len(res)
	dataOff := make([]int, len(res))
	for i, r := range res {
		off = align(off, 8)
		dataOff[i] = off
		off += len(r.Data)
	}
	buf := make([]byte, align(off, 8))
	le := binary.LittleEndian
	putDir := func(at, entries int) {
		le.PutUint16(buf[at+14:], uint16(entries)) // NumberOfIdEntries
	}
	putEntry := func(at int, id uint16, target uint32) {
		le.PutUint32(buf[at:], uint32(id))
		le.PutUint32(buf[at+4:], target)
	}
	putDir(0, len(types))
	for i, t := range types {
		putEntry(16+8*i, t.id, subdirFlag|uint32(typeOff[i]))
		putDir(typeOff[i], len(t.names))
		for j, n := range t.names {
			putEntry(typeOff[i]+16+8*j, n.id, subdirFlag|uint32(nameOff[n]))
			putDir(nameOff[n], len(n.langs))
			for k, ri := range n.langs {
				putEntry(nameOff[n]+16+8*k, res[ri].Lang, uint32(entryOff+16*ri))
			}
		}
	}
	relocs := make([]uint32, len(res))
	for i, r := range res {
		e := entryOff + 16*i
		le.PutUint32(buf[e:], uint32(dataOff[i]))
		le.PutUint32(buf[e+4:], uint32(len(r.Data)))
		relocs[i] = uint32(e)
		copy(buf[dataOff[i]:], r.Data)
	}
	return buf, relocs, nil
}
