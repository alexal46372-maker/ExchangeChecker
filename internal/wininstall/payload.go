package wininstall

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// MaxPayloadFile bounds one decompressed payload file (a corrupt or hostile
// stream cannot exhaust memory).
const MaxPayloadFile = 256 << 20

// GunzipPayload decompresses the embedded payload (7e, coordinator decision:
// gzip, decompression only via stdlib compress/gzip). name → gzip bytes in,
// name → exe bytes out. Any error means the setup is broken: nothing is
// installed (CHK-SETUP-02 refuses a nil payload).
func GunzipPayload(gz map[string][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(gz))
	for name, b := range gz {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("payload %s: %w", name, err)
		}
		zr.Multistream(false)
		data, err := io.ReadAll(io.LimitReader(zr, MaxPayloadFile+1))
		if err == nil {
			err = zr.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("payload %s: %w", name, err)
		}
		if len(data) > MaxPayloadFile {
			return nil, fmt.Errorf("payload %s: larger than %d bytes", name, MaxPayloadFile)
		}
		out[name] = data
	}
	return out, nil
}
