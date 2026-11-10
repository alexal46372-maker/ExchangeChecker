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
