// Package winres writes and reads Windows resources for Go-linked exe
// files without external tools (no rc.exe, mc.exe or third-party modules;
// PLAN §12 п. 51, phase 7a): a COFF object (.syso, amd64) with one .rsrc
// section and IMAGE_REL_AMD64_ADDR32NB relocations on every
// IMAGE_RESOURCE_DATA_ENTRY, which the Go linker merges into the image.
//
// Supported resource types: RT_ICON + RT_GROUP_ICON (from an .ico file),
// RT_MANIFEST, RT_VERSION (VS_VERSIONINFO) and RT_MESSAGETABLE. The same
// package parses resources back from a .syso or from a linked PE image
// (tests, CI checks). Standard library only; builds on every GOOS.
//
// Check ID: CHK-RES-01.
package winres
