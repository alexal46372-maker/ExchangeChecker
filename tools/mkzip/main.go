// Command mkzip packages a release (phase 7e / 10 / 13, CHK-DELIV-01). Std-only.
//
//	mkzip gz  -o <dir> <file>...                 deterministic <dir>/<name>.gz
//	mkzip zip -o <out.zip> [-entry <name.exe>] <Setup.exe>
//	        deterministic zip with exactly one entry. Default entry is derived
//	        from out.zip: ExchangeChecker-<V>.zip → "Exchange Checker <V>.exe"
//	        (zip name stays technical; entry has spaces — phase 13).
//	        -entry overrides when zip and entry names must differ explicitly.
//	mkzip version                               prints X.Y.Z / 0.0.0-rel.N / 0.0.0-ci.N
//
// Nothing is uploaded from ci.yml (CHK-CI-01); release.yml uploads the zip.
package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// zipTime: fixed entry time, so equal inputs give equal zips.
var zipTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var tagRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ZipEntryName derives the delivery installer entry from the technical zip
// path: ExchangeChecker-<V>.zip → "Exchange Checker <V>.exe" (CHK-DELIV-01,
// phase 13). The input setup exe may have any name.
func ZipEntryName(outZipPath string) (string, error) {
	base := filepath.Base(outZipPath)
	if !strings.HasSuffix(strings.ToLower(base), ".zip") || len(base) < 5 {
		return "", fmt.Errorf("zip output %q must end in .zip", outZipPath)
	}
	stem := base[:len(base)-len(filepath.Ext(base))]
	const prefix = "ExchangeChecker-"
	if !strings.HasPrefix(stem, prefix) {
		return "", fmt.Errorf("zip output %q must be named ExchangeChecker-<V>.zip", outZipPath)
	}
	ver := stem[len(prefix):]
	if ver == "" {
		return "", fmt.Errorf("zip output %q missing version", outZipPath)
	}
	return config.DeliveryInstallerName(ver), nil
}

// validateEntry checks an explicit -entry value (basename .exe, no path).
func validateEntry(entry string) error {
	if entry == "" || strings.ContainsAny(entry, `/\`) || entry != filepath.Base(entry) {
		return fmt.Errorf("bad zip entry %q", entry)
	}
	if !strings.HasSuffix(strings.ToLower(entry), ".exe") {
		return fmt.Errorf("zip entry %q must end in .exe", entry)
	}
	return nil
}

// Version resolves the build version from the CI environment.
func Version(getenv func(string) string) (string, error) {
	if getenv("GITHUB_REF_TYPE") == "tag" {
		tag := getenv("GITHUB_REF_NAME")
		if !tagRe.MatchString(tag) {
			return "", fmt.Errorf("tag %q is not vX.Y.Z", tag)
		}
		return tag[1:], nil
	}
	n := getenv("GITHUB_RUN_NUMBER")
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(n) {
		return "", errors.New("GITHUB_RUN_NUMBER not set")
	}
	if getenv("GITHUB_EVENT_NAME") == "workflow_dispatch" {
		return "0.0.0-rel." + n, nil
	}
	return "0.0.0-ci." + n, nil
}

// Gzip compresses deterministically (empty header name, zero mtime).
func Gzip(data []byte) ([]byte, error) {
	var b bytes.Buffer
	zw, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zw.ModTime = time.Time{}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Zip builds the delivery zip: one entry named entry, fixed time, deflate.
func Zip(setup []byte, entry string) ([]byte, error) {
	if err := validateEntry(entry); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	h := &zip.FileHeader{Name: entry, Method: zip.Deflate, Modified: zipTime}
	h.SetMode(0o644)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(setup); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mkzip gz|zip|version")
	}
	switch args[0] {
	case "version":
		v, err := Version(os.Getenv)
		if err == nil {
			fmt.Println(v)
		}
		return err
	case "gz", "zip":
		if len(args) < 4 || args[1] != "-o" {
			return fmt.Errorf("usage: mkzip %s -o <out> [-entry <name.exe>] <file>...", args[0])
		}
		out := args[2]
		rest := args[3:]
		entryFlag := ""
		var files []string
		for i := 0; i < len(rest); i++ {
			if rest[i] == "-entry" {
				if i+1 >= len(rest) {
					return errors.New("zip: -entry needs a value")
				}
				entryFlag = rest[i+1]
				i++
				continue
			}
			files = append(files, rest[i])
		}
		if args[0] == "zip" {
			if len(files) != 1 {
				return errors.New("zip takes exactly one setup exe (any file name)")
			}
			entry := entryFlag
			if entry == "" {
				var err error
				entry, err = ZipEntryName(out)
				if err != nil {
					return err
				}
			} else if err := validateEntry(entry); err != nil {
				return err
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				return err
			}
			z, err := Zip(data, entry)
			if err != nil {
				return err
			}
			return os.WriteFile(out, z, 0o644)
		}
		if entryFlag != "" {
			return errors.New("-entry is only valid for zip")
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			gz, err := Gzip(data)
			if err != nil {
				return err
			}
			dst := filepath.Join(out, filepath.Base(f)+".gz")
			if err := os.WriteFile(dst, gz, 0o644); err != nil {
				return err
			}
			fmt.Printf("%s: %d -> %d bytes (%.1f%%)\n", filepath.Base(f), len(data), len(gz), 100*float64(len(gz))/float64(len(data)))
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mkzip:", err)
		os.Exit(1)
	}
}
