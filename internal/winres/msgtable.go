package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"unicode/utf16"
)

// MessageTable returns an RT_MESSAGETABLE (MESSAGE_RESOURCE_DATA) with one
// Unicode entry per ID; consecutive IDs share a block. IDs must not carry
// severity/customer/facility bits the Event Log does not pass (ReportEventW
// gets the bare ID, internal/alert).
func MessageTable(msgs map[uint32]string) ([]byte, error) {
	if len(msgs) == 0 {
		return nil, errors.New("winres: empty message table")
	}
	ids := make([]uint32, 0, len(msgs))
	for id := range msgs {
		if id > 0xFFFF {
			return nil, fmt.Errorf("winres: message id %d carries high bits", id)
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	type blk struct{ lo, hi uint32 }
	var blocks []blk
	for _, id := range ids {
		if n := len(blocks); n > 0 && blocks[n-1].hi+1 == id {
			blocks[n-1].hi = id
		} else {
			blocks = append(blocks, blk{id, id})
		}
	}
	le := binary.LittleEndian
	var entries bytes.Buffer
	head := make([]byte, 4+12*len(blocks))
	le.PutUint32(head, uint32(len(blocks)))
	i := 0
	for bi, b := range blocks {
		le.PutUint32(head[4+12*bi:], b.lo)
		le.PutUint32(head[8+12*bi:], b.hi)
		le.PutUint32(head[12+12*bi:], uint32(len(head)+entries.Len()))
		for ; i < len(ids) && ids[i] <= b.hi; i++ {
			text := utf16z(msgs[ids[i]])
			n := align(4+len(text), 4)
			if n > 0xFFFF {
				return nil, fmt.Errorf("winres: message %d too long", ids[i])
			}
			binary.Write(&entries, le, uint16(n))
			binary.Write(&entries, le, uint16(1)) // MESSAGE_RESOURCE_UNICODE
			entries.Write(text)
			for entries.Len()%4 != 0 {
				entries.WriteByte(0)
			}
		}
	}
	return append(head, entries.Bytes()...), nil
}

// ParseMessageTable decodes MESSAGE_RESOURCE_DATA (Unicode and ANSI entries).
func ParseMessageTable(data []byte) (map[uint32]string, error) {
	le := binary.LittleEndian
	bad := errors.New("winres: malformed message table")
	if len(data) < 4 {
		return nil, bad
	}
	nb := int(le.Uint32(data))
	if 4+12*nb > len(data) {
		return nil, bad
	}
	out := map[uint32]string{}
	for b := 0; b < nb; b++ {
		lo, hi, off := le.Uint32(data[4+12*b:]), le.Uint32(data[8+12*b:]), int(le.Uint32(data[12+12*b:]))
		if hi < lo || hi-lo > 0xFFFF {
			return nil, bad
		}
		for id := lo; id <= hi; id++ {
			if off+4 > len(data) {
				return nil, bad
			}
			n, flags := int(le.Uint16(data[off:])), le.Uint16(data[off+2:])
			if n < 4 || off+n > len(data) {
				return nil, bad
			}
			text := data[off+4 : off+n]
			if flags&1 != 0 {
				u := make([]uint16, len(text)/2)
				for i := range u {
					u[i] = le.Uint16(text[2*i:])
				}
				for len(u) > 0 && u[len(u)-1] == 0 {
					u = u[:len(u)-1]
				}
				out[id] = string(utf16.Decode(u))
			} else {
				out[id] = string(bytes.TrimRight(text, "\x00"))
			}
			off += n
		}
	}
	return out, nil
}
