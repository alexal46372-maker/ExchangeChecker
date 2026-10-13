// Command mkzip packages a release (phase 7e, CHK-DELIV-01). Std-only.
//
//	mkzip gz  -o <dir> <file>...           deterministic <dir>/<name>.gz (gzip -9,
//	                                        no name, no mtime) for the setup payload
//	mkzip zip -o <out.zip> <Setup.exe>      deterministic zip with exactly one entry
//	                                        ExchangeCheckerSetup.exe
//	mkzip version                           prints X.Y.Z for a vX.Y.Z tag build,
//	                                        0.0.0-rel.<N> for workflow_dispatch,
//	                                        else 0.0.0-ci.<GITHUB_RUN_NUMBER>
//
// Nothing is uploaded (CHK-CI-01): CI builds and checks the zip in the job.
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
	"time"
)

// SetupEntry is the only file in the delivery zip (DELIVERY.md).
const SetupEntry = "ExchangeCheckerSetup.exe"

// zipTime: fixed entry time, so equal inputs give equal zips.
var zipTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var tagRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

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
		// manual release build (release.yml): own series, never clashes
		// with the 0.0.0-ci.N numbers of ci.yml
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

// Zip builds the delivery zip: one entry SetupEntry, fixed time, deflate.
func Zip(setup []byte) ([]byte, error) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	h := &zip.FileHeader{Name: SetupEntry, Method: zip.Deflate, Modified: zipTime}
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
			return fmt.Errorf("usage: mkzip %s -o <out> <file>...", args[0])
		}
		out, files := args[2], args[3:]
		if args[0] == "zip" {
			if len(files) != 1 || filepath.Base(files[0]) != SetupEntry {
				return fmt.Errorf("zip takes exactly one %s", SetupEntry)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				return err
			}
			z, err := Zip(data)
			if err != nil {
				return err
			}
			return os.WriteFile(out, z, 0o644)
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
