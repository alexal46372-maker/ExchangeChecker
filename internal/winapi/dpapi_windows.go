//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// dataBlob mirrors DATA_BLOB (wincrypt.h).
type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32Once            sync.Once
	crypt32Err             error
	procCryptProtectData   *syscall.LazyProc
	procCryptUnprotectData *syscall.LazyProc
)

func loadCrypt32() error {
	crypt32Once.Do(func() {
		mod := newSystemDLL("crypt32.dll")
		procCryptProtectData = mod.NewProc("CryptProtectData")
		procCryptUnprotectData = mod.NewProc("CryptUnprotectData")
		if err := procCryptProtectData.Find(); err != nil {
			crypt32Err = fmt.Errorf("winapi: crypt32.dll: %w", err)
			return
		}
		if err := procCryptUnprotectData.Find(); err != nil {
			crypt32Err = fmt.Errorf("winapi: crypt32.dll: %w", err)
		}
	})
	return crypt32Err
}

func blobOf(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

// takeBlob copies a DPAPI-allocated output blob into Go memory and frees it.
func takeBlob(b *dataBlob) []byte {
	if b.pbData == nil {
		return []byte{}
	}
	out := make([]byte, int(b.cbData))
	copy(out, unsafe.Slice(b.pbData, int(b.cbData)))
	localFree(unsafe.Pointer(b.pbData))
	b.pbData = nil
	b.cbData = 0
	return out
}

// ProtectData encrypts plain with CryptProtectData. Use
// CryptProtectLocalMachine|CryptProtectUIForbidden for secrets that the
// LocalSystem service must decrypt after the tray (a user) saved them.
func ProtectData(plain, entropy []byte, flags uint32) ([]byte, error) {
	if err := loadCrypt32(); err != nil {
		return nil, err
	}
	descr, err := syscall.UTF16PtrFromString(dpapiDescription)
	if err != nil {
		return nil, err
	}
	in := blobOf(plain)
	var ent *dataBlob
	if len(entropy) > 0 {
		ent = blobOf(entropy)
	}
	var out dataBlob
	r, _, e := callProc(procCryptProtectData,
		uintptr(unsafe.Pointer(in)),
		uintptr(unsafe.Pointer(descr)),
		uintptr(unsafe.Pointer(ent)),
		0, // pvReserved
		0, // pPromptStruct
		uintptr(flags),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(in)
	runtime.KeepAlive(ent)
	runtime.KeepAlive(plain)
	runtime.KeepAlive(entropy)
	runtime.KeepAlive(descr)
	if r == 0 {
		return nil, fmt.Errorf("winapi: CryptProtectData: %w", e)
	}
	return takeBlob(&out), nil
}

// UnprotectData decrypts a blob produced by ProtectData (same entropy).
func UnprotectData(blob, entropy []byte, flags uint32) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("winapi: CryptUnprotectData: empty input")
	}
	if err := loadCrypt32(); err != nil {
		return nil, err
	}
	in := blobOf(blob)
	var ent *dataBlob
	if len(entropy) > 0 {
		ent = blobOf(entropy)
	}
	var out dataBlob
	r, _, e := callProc(procCryptUnprotectData,
		uintptr(unsafe.Pointer(in)),
		0, // ppszDataDescr: not needed
		uintptr(unsafe.Pointer(ent)),
		0, // pvReserved
		0, // pPromptStruct
		uintptr(flags),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(in)
	runtime.KeepAlive(ent)
	runtime.KeepAlive(blob)
	runtime.KeepAlive(entropy)
	if r == 0 {
		return nil, fmt.Errorf("winapi: CryptUnprotectData: %w", e)
	}
	return takeBlob(&out), nil
}
