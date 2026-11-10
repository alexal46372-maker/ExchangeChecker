package checks_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
	"github.com/alexal46372-maker/ExchangeChecker/third_party/inpout"
)

// CHK-SETUP-19 — the InpOutx64 driver in third_party / the payload: sha256
// = config.InpOutSHA256 = the committed file; the Authenticode signature is
// intact, signed by the expected signer, chains to the Microsoft
// cross-certificate (kernel-mode signing on Windows 7 x64) and is
// timestamped inside the certificate validity. A flipped byte fails.

func readDriver(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(findModuleRoot(t), "third_party", "inpout", inpout.SysName))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCHK_SETUP_19_DriverHash(t *testing.T) {
	disk := readDriver(t)
	s := sha256.Sum256(disk)
	if hex.EncodeToString(s[:]) != config.InpOutSHA256 {
		t.Fatalf("CHK-SETUP-19: sha256(third_party)=%x != config.InpOutSHA256 %s", s, config.InpOutSHA256)
	}
	if !bytes.Equal(disk, inpout.SysFile) {
		t.Fatal("CHK-SETUP-19: embedded inpout.SysFile differs from the committed file")
	}
	if !wininstall.DriverHashOK(inpout.SysFile) || wininstall.DriverPayloadName != inpout.SysName {
		t.Fatal("CHK-SETUP-19: wininstall does not accept the committed driver")
	}
	// README states the same hash
	readme, err := os.ReadFile(filepath.Join(findModuleRoot(t), "third_party", "inpout", "README.md"))
	if err != nil || !bytes.Contains(readme, []byte(config.InpOutSHA256)) {
		t.Fatalf("CHK-SETUP-19: README.md must state the sha256 (%v)", err)
	}
	lic, err := os.ReadFile(filepath.Join(findModuleRoot(t), "third_party", "inpout", "LICENSE.txt"))
	if err != nil || !bytes.Contains(lic, []byte("Permission is hereby granted, free of charge")) || !bytes.Contains(lic, []byte("Phil Gibbons")) {
		t.Fatalf("CHK-SETUP-19: LICENSE.txt (MIT, highrez) missing (%v)", err)
	}
	t.Logf("PASS CHK-SETUP-19 hash: sha256=%s", config.InpOutSHA256)
}

func TestCHK_SETUP_19_DriverSignature(t *testing.T) {
	b := readDriver(t)
	r, err := verifyAuthenticode(b, inpout.CrossCertSHA1)
	if err != nil {
		t.Fatalf("CHK-SETUP-19: signature: %v", err)
	}
	if r.Signer.Subject.CommonName != inpout.SignerCN || sha1Hex(r.Signer) != inpout.SignerCertSHA1 {
		t.Fatalf("CHK-SETUP-19: signer %q %s, want %q %s", r.Signer.Subject.CommonName, sha1Hex(r.Signer), inpout.SignerCN, inpout.SignerCertSHA1)
	}
	top := r.Chain[len(r.Chain)-1]
	if top.Issuer.CommonName != inpout.CrossCertIssuer {
		t.Fatalf("CHK-SETUP-19: chain ends at %q issued by %q, want issuer %q", top.Subject.String(), top.Issuer.CommonName, inpout.CrossCertIssuer)
	}
	if r.DigestAlg != "sha1" && r.DigestAlg != "sha256" {
		t.Fatalf("digest %s", r.DigestAlg)
	}
	t.Logf("PASS CHK-SETUP-19 signature: %s, signer %q (%s), chain %d certs → %q, timestamp %s by %q (%s)",
		r.DigestAlg, r.Signer.Subject.CommonName, sha1Hex(r.Signer), len(r.Chain), top.Issuer.CommonName,
		r.SigningTime.UTC().Format("2006-01-02 15:04:05Z"), r.TSA.Subject.CommonName, r.TSDigestAlg)
}

// negative cases: one flipped byte in the code, in the signature blob, in
// the signer's signature; wrong pinned cross-certificate
func TestCHK_SETUP_19_DriverSignatureNegative(t *testing.T) {
	b := readDriver(t)
	if _, err := verifyAuthenticode(b, inpout.CrossCertSHA1); err != nil {
		t.Fatalf("control: %v", err)
	}
	flip := func(off int) []byte { c := append([]byte(nil), b...); c[off] ^= 0x01; return c }
	if _, err := verifyAuthenticode(flip(0x400+16), inpout.CrossCertSHA1); err == nil || !strings.Contains(err.Error(), "PE image hash mismatch") {
		t.Errorf("flipped code byte: %v", err)
	}
	if wininstall.DriverHashOK(flip(0x400 + 16)) {
		t.Error("flipped code byte passes DriverHashOK")
	}
	if _, err := verifyAuthenticode(b, "0000000000000000000000000000000000000000"); err == nil || !strings.Contains(err.Error(), "cross-certificate") {
		t.Errorf("wrong pinned cross-certificate: %v", err)
	}
	// corrupt the signer's RSA signature (EncryptedDigest): find it by
	// re-parsing and flipping a byte in the middle of it
	r, _ := verifyAuthenticode(b, inpout.CrossCertSHA1)
	i := bytes.Index(b, r.Signer.Raw)
	if i < 0 {
		t.Fatal("signer certificate bytes not found")
	}
	if _, err := verifyAuthenticode(flip(i+len(r.Signer.Raw)/2), inpout.CrossCertSHA1); err == nil {
		t.Error("corrupt signer certificate accepted")
	}
	// truncated: no certificate table
	if _, err := verifyAuthenticode(b[:0x400], inpout.CrossCertSHA1); err == nil {
		t.Error("truncated file accepted")
	}
}

// TestCHK_SETUP_19_Built — release-like build (EC_BUILD_DIR): the setup
// exe carries the driver byte for byte; the payload-free uninstaller does
// not (the driver is linked only with -tags payload).
func TestCHK_SETUP_19_Built(t *testing.T) {
	dir := os.Getenv("EC_BUILD_DIR")
	if dir == "" {
		t.Skip("EC_BUILD_DIR not set (CI release build step)")
	}
	setup, err := os.ReadFile(filepath.Join(dir, config.SetupExeName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(setup, inpout.SysFile) {
		t.Fatal("CHK-SETUP-19: setup exe does not carry inpoutx64.sys byte for byte")
	}
	un, err := os.ReadFile(filepath.Join(dir, config.UninstallExeName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(un, inpout.SysFile[:4096]) {
		t.Fatal("CHK-SETUP-19: the payload-free uninstaller must not carry the driver")
	}
	t.Logf("PASS CHK-SETUP-19 built: %s carries inpoutx64.sys (%d bytes, sha256 %s)", config.SetupExeName, len(inpout.SysFile), config.InpOutSHA256)
}
