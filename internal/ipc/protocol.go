package ipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ErrTooLarge is returned for frames above model.IPCMaxMessage.
var ErrTooLarge = errors.New("ipc: message too large")

// WriteMessage writes v as one frame (length prefix + JSON) in a single
// Write call.
func WriteMessage(w io.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ipc: marshal: %w", err)
	}
	if len(b) > model.IPCMaxMessage {
		return ErrTooLarge
	}
	frame := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(frame, uint32(len(b)))
	copy(frame[4:], b)
	_, err = w.Write(frame)
	return err
}

// ReadMessage reads one frame into v.
func ReadMessage(r io.Reader, v interface{}) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > model.IPCMaxMessage {
		return ErrTooLarge
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("ipc: bad message: %w", err)
	}
	return nil
}
