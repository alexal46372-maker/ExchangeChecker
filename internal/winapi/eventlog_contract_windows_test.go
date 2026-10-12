//go:build windows

package winapi

import (
	"syscall"
	"testing"
	"unsafe"
)

// TestCHK_SVC_02_ReportEventContract — ReportEventW is called with
// category 0, the bare event ID, no SID, exactly one insertion string,
// dwDataSize 0 and lpRawData NULL (PLAN-phase7 §3.3 S4).
func TestCHK_SVC_02_ReportEventContract(t *testing.T) {
	var got []uintptr
	var str string
	old := reportEventHook
	defer func() { reportEventHook = old }()
	reportEventHook = func(args ...uintptr) (uintptr, uintptr, error) {
		got = append([]uintptr(nil), args...)
		if len(args) == 9 && args[7] != 0 {
			p := *(**uint16)(unsafe.Pointer(args[7]))
			n := 0
			for *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(n)*2)) != 0 {
				n++
			}
			str = syscall.UTF16ToString(unsafe.Slice(p, n))
		}
		return 1, 0, nil
	}
	l := &EventLog{h: 0x1234}
	for _, id := range []uint32{100, 101, 102, 103, 104, 105, 110} {
		got, str = nil, ""
		if err := l.Report(0x0002, id, "msg"); err != nil {
			t.Fatalf("Report: %v", err)
		}
		if len(got) != 9 {
			t.Fatalf("ReportEventW args = %d, want 9", len(got))
		}
		want := map[int]uintptr{0: 0x1234, 1: 0x0002, 2: 0, 3: uintptr(id), 4: 0, 5: 1, 6: 0, 8: 0}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("id %d: arg %d = %#x, want %#x", id, i, got[i], w)
			}
		}
		if got[3]&0xFFFF0000 != 0 {
			t.Errorf("event ID %#x carries high bits", got[3])
		}
		if got[7] == 0 || str != "msg" {
			t.Errorf("insertion string = %q, want %q", str, "msg")
		}
	}
}
