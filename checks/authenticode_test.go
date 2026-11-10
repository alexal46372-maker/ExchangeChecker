package checks_test

import (
	"bytes"
	"crypto"
	_ "crypto/md5" // crypto.MD5 for legacy countersignatures
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Pure-Go Authenticode verification of a PE (CHK-SETUP-19): PE image hash
// vs SpcIndirectDataContent, messageDigest attribute, RSA signature of the
// signer, certificate chain up to the Microsoft cross-certificate (pinned
// by thumbprint: Go's x509 refuses SHA1 chains, the signatures are checked
// here with crypto/rsa), and the RFC 2985 countersignature (timestamp)
// inside the signer certificate validity.

var (
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidSpcIndirect   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 2, 1, 4}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidCounterSig    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 6}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidSHA1          = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidMD5           = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 5}
)

type acContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

type acIssuerSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type acAttribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}

type acSignerInfo struct {
	Version         int
	IssuerSerial    acIssuerSerial
	DigestAlg       pkix.AlgorithmIdentifier
	AuthAttrs       asn1.RawValue `asn1:"optional,tag:0"`
	DigestEncAlg    pkix.AlgorithmIdentifier
	EncryptedDigest []byte
	UnauthAttrs     asn1.RawValue `asn1:"optional,tag:1"`
}

type acSignedData struct {
	Version     int
	DigestAlgs  asn1.RawValue
	ContentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	Certificates asn1.RawValue  `asn1:"optional,tag:0"`
	CRLs         asn1.RawValue  `asn1:"optional,tag:1"`
	SignerInfos  []acSignerInfo `asn1:"set"`
}

type acDigestInfo struct {
	Alg    pkix.AlgorithmIdentifier
	Digest []byte
}

type acIndirect struct {
	Data   asn1.RawValue
	Digest acDigestInfo
}

// authenticodeResult is what verifyAuthenticode established.
type authenticodeResult struct {
	DigestAlg   string
	Signer      *x509.Certificate
	Chain       []*x509.Certificate // signer first, cross-certificate last
	SigningTime time.Time
	TSA         *x509.Certificate
	TSDigestAlg string
}

func hashFor(oid asn1.ObjectIdentifier) (crypto.Hash, string, error) {
	switch {
	case oid.Equal(oidSHA1):
		return crypto.SHA1, "sha1", nil
	case oid.Equal(oidSHA256):
		return crypto.SHA256, "sha256", nil
	case oid.Equal(oidMD5):
		// only legacy RFC 2985 timestamps (VeriSign TSA, 2008) use MD5
		return crypto.MD5, "md5", nil
	}
	return 0, "", fmt.Errorf("digest algorithm %v not supported", oid)
}

// peAuthenticodeHash: the image hash without the checksum, the security
// directory entry and the certificate table (Authenticode spec §"Calculating
// the PE Image Hash"; the cert table is the last thing in the file).
func peAuthenticodeHash(b []byte, h crypto.Hash) (digest []byte, certOff, certLen uint32, err error) {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return nil, 0, 0, errors.New("not a PE (MZ)")
	}
	peOff := binary.LittleEndian.Uint32(b[0x3c:])
	if int(peOff)+24 > len(b) || !bytes.Equal(b[peOff:peOff+4], []byte("PE\x00\x00")) {
		return nil, 0, 0, errors.New("not a PE (signature)")
	}
	opt := int(peOff) + 24
	magic := binary.LittleEndian.Uint16(b[opt:])
	var ddOff int
	switch magic {
	case 0x20b:
		ddOff = opt + 112
	case 0x10b:
		ddOff = opt + 96
	default:
		return nil, 0, 0, fmt.Errorf("optional header magic %#x", magic)
	}
	csum := opt + 64
	sec := ddOff + 4*8
	certOff = binary.LittleEndian.Uint32(b[sec:])
	certLen = binary.LittleEndian.Uint32(b[sec+4:])
	if certOff == 0 || certLen == 0 || int(certOff)+int(certLen) > len(b) {
		return nil, 0, 0, errors.New("no certificate table (unsigned)")
	}
	w := h.New()
	w.Write(b[:csum])
	w.Write(b[csum+4 : sec])
	w.Write(b[sec+8 : certOff])
	if tail := int(certOff) + int(certLen); tail < len(b) {
		w.Write(b[tail:])
	}
	return w.Sum(nil), certOff, certLen, nil
}

func attrValue(attrs []acAttribute, oid asn1.ObjectIdentifier) (asn1.RawValue, bool) {
	for _, a := range attrs {
		if a.Type.Equal(oid) {
			var v asn1.RawValue
			if _, err := asn1.Unmarshal(a.Values.Bytes, &v); err == nil {
				return v, true
			}
		}
	}
	return asn1.RawValue{}, false
}

// parseAttrs parses an IMPLICIT [n] SET OF Attribute and returns the DER
// of the same set with the universal SET tag (what the signature covers).
func parseAttrs(raw asn1.RawValue) ([]acAttribute, []byte, error) {
	if len(raw.FullBytes) == 0 {
		return nil, nil, nil
	}
	set := append([]byte(nil), raw.FullBytes...)
	set[0] = 0x31
	var attrs []acAttribute
	if _, err := asn1.UnmarshalWithParams(set, &attrs, "set"); err != nil {
		return nil, nil, err
	}
	return attrs, set, nil
}

func verifyRSA(cert *x509.Certificate, h crypto.Hash, signed, sig []byte) error {
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA key")
	}
	w := h.New()
	w.Write(signed)
	d := w.Sum(nil)
	err := rsa.VerifyPKCS1v15(pub, h, d, sig)
	if err != nil && h == crypto.MD5 {
		// legacy VeriSign TSA (2008): PKCS#1 v1.5 block with the bare MD5,
		// no DigestInfo
		err = rsa.VerifyPKCS1v15(pub, 0, d, sig)
	}
	return err
}

func certHash(c *x509.Certificate) crypto.Hash {
	switch c.SignatureAlgorithm {
	case x509.SHA1WithRSA:
		return crypto.SHA1
	case x509.SHA256WithRSA:
		return crypto.SHA256
	case x509.MD5WithRSA:
		return crypto.MD5
	}
	return 0
}

func sha1Hex(c *x509.Certificate) string { s := sha1.Sum(c.Raw); return hex.EncodeToString(s[:]) }

func findCert(certs []*x509.Certificate, issuer []byte, serial *big.Int) *x509.Certificate {
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, issuer) && c.SerialNumber.Cmp(serial) == 0 {
			return c
		}
	}
	return nil
}

// verifyAuthenticode checks the embedded signature of a PE. crossSHA1 pins
// the top certificate of the chain (Microsoft cross-certificate).
func verifyAuthenticode(b []byte, crossSHA1 string) (authenticodeResult, error) {
	var res authenticodeResult
	// WIN_CERTIFICATE
	_, certOff, certLen, err := peAuthenticodeHash(b, crypto.SHA1)
	if err != nil {
		return res, err
	}
	wc := b[certOff : certOff+certLen]
	if len(wc) < 8 || binary.LittleEndian.Uint16(wc[4:]) != 0x0200 || binary.LittleEndian.Uint16(wc[6:]) != 0x0002 {
		return res, errors.New("WIN_CERTIFICATE is not WIN_CERT_TYPE_PKCS_SIGNED_DATA rev 2.0")
	}
	dwLen := binary.LittleEndian.Uint32(wc)
	if dwLen < 8 || dwLen > certLen {
		return res, errors.New("WIN_CERTIFICATE length")
	}
	var ci acContentInfo
	if _, err := asn1.Unmarshal(wc[8:dwLen], &ci); err != nil {
		return res, fmt.Errorf("PKCS#7: %w", err)
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return res, errors.New("not SignedData")
	}
	var sd acSignedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return res, fmt.Errorf("SignedData: %w", err)
	}
	if !sd.ContentInfo.ContentType.Equal(oidSpcIndirect) {
		return res, errors.New("content is not SPC_INDIRECT_DATA")
	}
	// RawValue with explicit,tag:0 keeps the [0] wrapper: Bytes is the
	// SpcIndirectDataContent SEQUENCE
	var indSeq asn1.RawValue
	if _, err := asn1.Unmarshal(sd.ContentInfo.Content.Bytes, &indSeq); err != nil {
		return res, fmt.Errorf("SpcIndirectDataContent: %w", err)
	}
	var ind acIndirect
	if _, err := asn1.Unmarshal(indSeq.FullBytes, &ind); err != nil {
		return res, fmt.Errorf("SpcIndirectDataContent: %w", err)
	}
	h, hname, err := hashFor(ind.Digest.Alg.Algorithm)
	if err != nil || h == crypto.MD5 {
		return res, fmt.Errorf("image digest: %v %v", hname, err)
	}
	res.DigestAlg = hname
	img, _, _, err := peAuthenticodeHash(b, h)
	if err != nil {
		return res, err
	}
	if !bytes.Equal(img, ind.Digest.Digest) {
		return res, fmt.Errorf("PE image hash mismatch (file modified): %x != signed %x", img, ind.Digest.Digest)
	}
	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil {
		return res, fmt.Errorf("certificates: %w", err)
	}
	if len(sd.SignerInfos) != 1 {
		return res, fmt.Errorf("%d signer infos, want 1", len(sd.SignerInfos))
	}
	si := sd.SignerInfos[0]
	signer := findCert(certs, si.IssuerSerial.Issuer.FullBytes, si.IssuerSerial.Serial)
	if signer == nil {
		return res, errors.New("signer certificate not in the signature")
	}
	sh, _, err := hashFor(si.DigestAlg.Algorithm)
	if err != nil || sh == crypto.MD5 {
		return res, fmt.Errorf("signer digest: %v", err)
	}
	attrs, attrSet, err := parseAttrs(si.AuthAttrs)
	if err != nil || attrs == nil {
		return res, fmt.Errorf("authenticated attributes: %v", err)
	}
	md, ok := attrValue(attrs, oidMessageDigest)
	if !ok {
		return res, errors.New("messageDigest attribute missing")
	}
	// Authenticode: the digest covers the value of SpcIndirectDataContent
	// (without its SEQUENCE tag and length)
	w := sh.New()
	w.Write(indSeq.Bytes)
	if !bytes.Equal(md.Bytes, w.Sum(nil)) {
		return res, errors.New("messageDigest attribute does not match the signed content")
	}
	if ct, ok := attrValue(attrs, oidContentType); !ok || !bytes.Contains(ct.FullBytes, []byte{0x06, 0x0a, 0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0x37, 0x02, 0x01, 0x04}) {
		return res, errors.New("contentType attribute is not SPC_INDIRECT_DATA")
	}
	if err := verifyRSA(signer, sh, attrSet, si.EncryptedDigest); err != nil {
		return res, fmt.Errorf("signer signature: %w", err)
	}
	res.Signer = signer
	// chain: each certificate signed by the next one, up to the pinned
	// cross-certificate
	chain := []*x509.Certificate{signer}
	for cur := signer; ; {
		if sha1Hex(cur) == crossSHA1 {
			break
		}
		var parent *x509.Certificate
		for _, c := range certs {
			if c != cur && bytes.Equal(c.RawSubject, cur.RawIssuer) {
				if verifyRSA(c, certHash(cur), cur.RawTBSCertificate, cur.Signature) == nil {
					parent = c
					break
				}
			}
		}
		if parent == nil {
			return res, fmt.Errorf("chain: no verified issuer for %q (cross-certificate %s not reached)", cur.Subject.CommonName, crossSHA1)
		}
		chain = append(chain, parent)
		cur = parent
		if len(chain) > 8 {
			return res, errors.New("chain too long")
		}
	}
	res.Chain = chain
	// countersignature (timestamp)
	ua, _, err := parseAttrs(si.UnauthAttrs)
	if err != nil {
		return res, fmt.Errorf("unauthenticated attributes: %w", err)
	}
	cs, ok := attrValue(ua, oidCounterSig)
	if !ok {
		return res, errors.New("no timestamp countersignature")
	}
	var csi acSignerInfo
	if _, err := asn1.Unmarshal(cs.FullBytes, &csi); err != nil {
		return res, fmt.Errorf("countersignature: %w", err)
	}
	tsa := findCert(certs, csi.IssuerSerial.Issuer.FullBytes, csi.IssuerSerial.Serial)
	if tsa == nil {
		return res, errors.New("TSA certificate not in the signature")
	}
	ch, chName, err := hashFor(csi.DigestAlg.Algorithm)
	if err != nil {
		return res, err
	}
	cattrs, cset, err := parseAttrs(csi.AuthAttrs)
	if err != nil {
		return res, err
	}
	cmd, ok := attrValue(cattrs, oidMessageDigest)
	w = ch.New()
	w.Write(si.EncryptedDigest)
	if !ok || !bytes.Equal(cmd.Bytes, w.Sum(nil)) {
		return res, errors.New("countersignature does not cover the signer signature")
	}
	if err := verifyRSA(tsa, ch, cset, csi.EncryptedDigest); err != nil {
		return res, fmt.Errorf("countersignature: %w", err)
	}
	st, ok := attrValue(cattrs, oidSigningTime)
	if !ok {
		return res, errors.New("countersignature without signingTime")
	}
	var tm time.Time
	if _, err := asn1.Unmarshal(st.FullBytes, &tm); err != nil {
		return res, fmt.Errorf("signingTime: %w", err)
	}
	res.SigningTime, res.TSA, res.TSDigestAlg = tm, tsa, chName
	for _, c := range append([]*x509.Certificate{tsa}, chain...) {
		if tm.Before(c.NotBefore) || tm.After(c.NotAfter) {
			return res, fmt.Errorf("timestamp %s outside the validity of %q (%s…%s)", tm, c.Subject.CommonName, c.NotBefore, c.NotAfter)
		}
	}
	return res, nil
}
