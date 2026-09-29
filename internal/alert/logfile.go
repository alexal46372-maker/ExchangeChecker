package alert

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Log file defaults.
const (
	DefaultLogMaxBytes = 1 << 20 // rotate at 1 MiB
	DefaultLogKeep     = 5       // exchsvc.log.1 .. exchsvc.log.5
)

// RotatingFile is an append-only log file rotated by size:
// path -> path.1 -> ... -> path.Keep (the oldest is overwritten).
// Lines are prefixed with the time in UTC+3. Safe for concurrent use.
type RotatingFile struct {
	Path     string
	MaxBytes int64
	Keep     int
	// Now is the clock (tests); nil = time.Now.
	Now func() time.Time

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenRotatingFile creates the directory and opens (appends to) path.
func OpenRotatingFile(path string, maxBytes int64, keep int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultLogMaxBytes
	}
	if keep < 1 {
		keep = DefaultLogKeep
	}
	r := &RotatingFile{Path: path, MaxBytes: maxBytes, Keep: keep}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o755); err != nil {
		return fmt.Errorf("alert: log dir: %w", err)
	}
	f, err := os.OpenFile(r.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("alert: open log: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("alert: stat log: %w", err)
	}
	r.f, r.size = f, fi.Size()
	return nil
}

// rotate shifts the numbered files (os.Rename replaces the target; on
// Windows it is MoveFileEx with MOVEFILE_REPLACE_EXISTING).
func (r *RotatingFile) rotate() error {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	for i := r.Keep - 1; i >= 1; i-- {
		from := r.Path + "." + strconv.Itoa(i)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, r.Path+"."+strconv.Itoa(i+1))
		}
	}
	if err := os.Rename(r.Path, r.Path+".1"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("alert: rotate log: %w", err)
	}
	return r.open()
}

// Write appends p as is (rotating first when the file would exceed MaxBytes).
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.MaxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// Printf writes one timestamped line.
func (r *RotatingFile) Printf(format string, args ...interface{}) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	line := now().In(model.DBZone).Format("2006-01-02 15:04:05.000") + " " + fmt.Sprintf(format, args...)
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line += "\n"
	}
	_, _ = r.Write([]byte(line))
}

// Close closes the file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
