package ipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ErrTooLarge is returned for frames above the limit of their direction:
// model.IPCMaxRequest for tray -> service requests, model.IPCMaxMessage
// for service -> tray messages (CHK-MON-11).
var ErrTooLarge = errors.New("ipc: message too large")

// WriteMessage writes a service -> tray message (limit IPCMaxMessage).
func WriteMessage(w io.Writer, v interface{}) error {
	return WriteFrame(w, v, model.IPCMaxMessage)
}

// WriteRequest writes a tray -> service request (limit IPCMaxRequest).
func WriteRequest(w io.Writer, req model.IPCRequest) error {
	return WriteFrame(w, req, model.IPCMaxRequest)
}

// WriteFrame writes v as one frame (length prefix + JSON) in a single
// Write call; frames above max are refused with ErrTooLarge.
func WriteFrame(w io.Writer, v interface{}, max int) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ipc: marshal: %w", err)
	}
	if len(b) > max {
		return ErrTooLarge
	}
	frame := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(frame, uint32(len(b)))
	copy(frame[4:], b)
	_, err = w.Write(frame)
	return err
}

// ReadMessage reads one service -> tray frame (limit IPCMaxMessage).
func ReadMessage(r io.Reader, v interface{}) error {
	return ReadFrame(r, v, model.IPCMaxMessage)
}

// ReadFrame reads one frame into v. A length above max is rejected before
// anything is allocated.
func ReadFrame(r io.Reader, v interface{}, max int) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if uint64(n) > uint64(max) {
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
