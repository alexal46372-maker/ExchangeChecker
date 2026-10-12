package winres

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// IconResources converts an .ico file into RT_ICON resources with IDs
// firstID, firstID+1, … and one RT_GROUP_ICON resource groupID that
// references them.
func IconResources(ico []byte, groupID, firstID uint16, lang uint16) ([]Resource, error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil, errors.New("winres: not an .ico file")
	}
	n := int(le.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return nil, errors.New("winres: .ico directory truncated")
	}
	if int(firstID)+n-1 > 0xFFFF {
		return nil, errors.New("winres: icon IDs overflow")
	}
	group := make([]byte, 6+14*n)
	le.PutUint16(group[2:], 1)
	le.PutUint16(group[4:], uint16(n))
	var out []Resource
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if uint64(off)+uint64(size) > uint64(len(ico)) || size == 0 {
			return nil, fmt.Errorf("winres: .ico image %d out of bounds", i)
		}
		id := firstID + uint16(i)
		g := group[6+14*i:]
		copy(g[:12], e[:12]) // width, height, colors, reserved, planes, bit count, bytes in res
		le.PutUint16(g[12:], id)
		out = append(out, Resource{Type: TypeIcon, ID: id, Lang: lang, Data: append([]byte(nil), ico[off:off+size]...)})
	}
	out = append(out, Resource{Type: TypeGroupIcon, ID: groupID, Lang: lang, Data: group})
	return out, nil
}

// GroupIconIDs returns the RT_ICON IDs referenced by an RT_GROUP_ICON.
func GroupIconIDs(group []byte) ([]uint16, error) {
	le := binary.LittleEndian
	if len(group) < 6 || le.Uint16(group[2:]) != 1 {
		return nil, errors.New("winres: not a group icon")
	}
	n := int(le.Uint16(group[4:]))
	if len(group) < 6+14*n {
		return nil, errors.New("winres: group icon truncated")
	}
	ids := make([]uint16, n)
	for i := range ids {
		ids[i] = le.Uint16(group[6+14*i+12:])
	}
	return ids, nil
}
