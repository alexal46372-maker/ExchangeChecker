package wininstall

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func gz(t *testing.T, n int) []byte {
	var b bytes.Buffer
	t.Helper()
	w := gzip.NewWriter(&b)
	if _, err := w.Write(bytes.Repeat([]byte{'x'}, n)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestCHK_SETUP_02_PayloadLimits — per-file and total decompressed caps.
func TestCHK_SETUP_02_PayloadLimits(t *testing.T) {
	in := map[string][]byte{"a": gz(t, 600), "b": gz(t, 600)}
	if out, err := gunzipPayload(in, 1000, 2000); err != nil || len(out["a"]) != 600 || len(out["b"]) != 600 {
		t.Fatalf("within limits: %v", err)
	}
	if _, err := gunzipPayload(in, 1000, 1000); err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("total cap: %v", err)
	}
	if _, err := gunzipPayload(map[string][]byte{"a": gz(t, 1001)}, 1000, 5000); err == nil || !strings.Contains(err.Error(), "payload a: larger") {
		t.Fatalf("file cap: %v", err)
	}
	if _, err := gunzipPayload(map[string][]byte{"a": gz(t, 1000)}, 1000, 1000); err != nil {
		t.Fatalf("exactly at cap: %v", err)
	}
	if MaxPayloadTotal < 3*20<<20 || MaxPayloadTotal < MaxPayloadFile {
		t.Fatal("caps too small for the real payload")
	}
}
