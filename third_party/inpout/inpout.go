// Package inpout carries the unmodified, Authenticode-signed InpOutx64
// kernel driver from the official highrez distribution (see README.md,
// LICENSE.txt). Only the installer payload build links it (phase 14,
// CHK-SETUP-19); the product never rebuilds or patches the file.
package inpout

import _ "embed"

// SysFile is inpoutx64.sys byte for byte (sha256 = config.InpOutSHA256).
//
//go:embed inpoutx64.sys
var SysFile []byte

// SysName is the file name of the driver inside the payload and on disk.
const SysName = "inpoutx64.sys"

// Expected Authenticode signer of SysFile (CHK-SETUP-19): the leaf
// certificate and the Microsoft cross-certificate that makes the chain
// valid for kernel-mode code signing on Windows 7 x64 (VeriSign Class 3
// Public Primary CA → Microsoft Code Verification Root).
const (
	SignerCN        = "Red Fox UK Limited"
	SignerCertSHA1  = "44a7c0e0680e59bd76f0077d60b7b3b55ef68518"
	CrossCertSHA1   = "58455389cf1d0cd6a08e3ce216f65adff7a86408"
	CrossCertIssuer = "Microsoft Code Verification Root"
)
