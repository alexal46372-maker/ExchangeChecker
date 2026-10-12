package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
)

// ParseSyso reads the resources of a COFF object written by BuildSyso (or
// a compatible generator): the .rsrc section, with every data entry's
// OffsetToData covered by exactly one ADDR32NB relocation against the
// section symbol.
func ParseSyso(obj []byte) ([]Resource, error) {
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		return nil, fmt.Errorf("winres: %w", err)
	}
	defer f.Close()
	if f.Machine != machineAMD64 {
		return nil, fmt.Errorf("winres: machine %#x, want amd64", f.Machine)
	}
	s := f.Section(".rsrc")
	if s == nil {
		return nil, errors.New("winres: no .rsrc section")
	}
	data, err := s.Data()
	if err != nil {
		return nil, fmt.Errorf("winres: %w", err)
	}
	res, entries, err := parseTree(data, 0)
	if err != nil {
		return nil, err
	}
	if len(s.Relocs) != len(entries) {
		return nil, fmt.Errorf("winres: %d relocations for %d data entries", len(s.Relocs), len(entries))
	}
	want := map[uint32]bool{}
	for _, e := range entries {
		want[e] = true
	}
	for _, r := range s.Relocs {
		if r.Type != relAMD64Addr32NB || !want[r.VirtualAddress] {
			return nil, fmt.Errorf("winres: relocation %+v does not point at a data entry", r)
		}
		delete(want, r.VirtualAddress)
	}
	return res, nil
}

// ParsePE reads the resources of a linked PE image (RVAs relative to the
// .rsrc section's virtual address).
func ParsePE(image []byte) ([]Resource, error) {
	f, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		return nil, fmt.Errorf("winres: %w", err)
	}
	defer f.Close()
	s := f.Section(".rsrc")
	if s == nil {
		return nil, errors.New("winres: no .rsrc section")
	}
	data, err := s.Data()
	if err != nil {
		return nil, fmt.Errorf("winres: %w", err)
	}
	if int(s.VirtualSize) < len(data) && s.VirtualSize != 0 {
		data = data[:s.VirtualSize]
	}
	res, _, err := parseTree(data, s.VirtualAddress)
	return res, err
}

// parseTree walks type → name → language; base is subtracted from each
// data entry's OffsetToData. It returns the resources and the offsets of
// the data entries' OffsetToData fields.
func parseTree(data []byte, base uint32) ([]Resource, []uint32, error) {
	le := binary.LittleEndian
	errShort := errors.New("winres: resource tree out of bounds")
	type ent struct {
		id     uint16
		target uint32
	}
	readDir := func(off uint32) ([]ent, error) {
		if uint64(off)+16 > uint64(len(data)) {
			return nil, errShort
		}
		n := int(le.Uint16(data[off+12:])) + int(le.Uint16(data[off+14:]))
		if uint64(off)+16+8*uint64(n) > uint64(len(data)) {
			return nil, errShort
		}
		out := make([]ent, n)
		for i := range out {
			p := off + 16 + uint32(8*i)
			name := le.Uint32(data[p:])
			if name&subdirFlag != 0 {
				return nil, errors.New("winres: string resource names are not supported")
			}
			out[i] = ent{uint16(name), le.Uint32(data[p+4:])}
		}
		return out, nil
	}
	var res []Resource
	var entries []uint32
	types, err := readDir(0)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range types {
		if t.target&subdirFlag == 0 {
			return nil, nil, errors.New("winres: type entry is not a directory")
		}
		names, err := readDir(t.target &^ subdirFlag)
		if err != nil {
			return nil, nil, err
		}
		for _, n := range names {
			if n.target&subdirFlag == 0 {
				return nil, nil, errors.New("winres: name entry is not a directory")
			}
			langs, err := readDir(n.target &^ subdirFlag)
			if err != nil {
				return nil, nil, err
			}
			for _, l := range langs {
				if l.target&subdirFlag != 0 || uint64(l.target)+16 > uint64(len(data)) {
					return nil, nil, errShort
				}
				rva, size := le.Uint32(data[l.target:]), le.Uint32(data[l.target+4:])
				if rva < base || uint64(rva-base)+uint64(size) > uint64(len(data)) {
					return nil, nil, errShort
				}
				d := append([]byte(nil), data[rva-base:rva-base+size]...)
				res = append(res, Resource{Type: t.id, ID: n.id, Lang: l.id, Data: d})
				entries = append(entries, l.target)
			}
		}
	}
	return res, entries, nil
}

// Find returns the first resource of type typ (any ID / language).
func Find(res []Resource, typ uint16) (Resource, bool) {
	for _, r := range res {
		if r.Type == typ {
			return r, true
		}
	}
	return Resource{}, false
}
