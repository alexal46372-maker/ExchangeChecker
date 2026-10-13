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

// MaxPayloadTotal bounds all decompressed payload files together (the
// three exe are ~17 MiB today).
const MaxPayloadTotal = 512 << 20

// GunzipPayload decompresses the embedded payload (7e, coordinator decision:
// gzip, decompression only via stdlib compress/gzip). name → gzip bytes in,
// name → exe bytes out. Any error means the setup is broken: nothing is
// installed (CHK-SETUP-02 refuses a nil payload).
func GunzipPayload(gz map[string][]byte) (map[string][]byte, error) {
	return gunzipPayload(gz, MaxPayloadFile, MaxPayloadTotal)
}

func gunzipPayload(gz map[string][]byte, maxFile, maxTotal int64) (map[string][]byte, error) {
	out := make(map[string][]byte, len(gz))
	var total int64
	for name, b := range gz {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("payload %s: %w", name, err)
		}
		zr.Multistream(false)
		limit := maxFile
		if rest := maxTotal - total; rest < limit {
			limit = rest
		}
		data, err := io.ReadAll(io.LimitReader(zr, limit+1))
		if err == nil {
			err = zr.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("payload %s: %w", name, err)
		}
		if int64(len(data)) > limit {
			if limit < maxFile {
				return nil, fmt.Errorf("payload: decompressed total larger than %d bytes (at %s)", maxTotal, name)
			}
			return nil, fmt.Errorf("payload %s: larger than %d bytes", name, maxFile)
		}
		total += int64(len(data))
		out[name] = data
	}
	return out, nil
}
