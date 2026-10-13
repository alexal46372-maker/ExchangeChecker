package wininstall

import "testing"

func TestCHK_SETUP_09_SplitTrayStop(t *testing.T) {
	post, term := SplitTrayStop(1, []TrayProc{
		{PID: 10, Session: 1},
		{PID: 11, Session: 2},
		{PID: 12, Session: 1},
		{PID: 13, Session: 0xffffffff},
	})
	if len(post) != 2 || post[0] != 10 || post[1] != 12 {
		t.Fatalf("post=%v", post)
	}
	if len(term) != 2 || term[0] != 11 || term[1] != 13 {
		t.Fatalf("term=%v", term)
	}
}
