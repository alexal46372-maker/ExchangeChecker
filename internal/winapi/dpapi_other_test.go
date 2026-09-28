//go:build !windows

package winapi

import (
	"errors"
	"testing"
)

func TestCHK_CFG_01_DPAPIStubNotSupported(t *testing.T) {
	if _, err := ProtectData([]byte("x"), nil, CryptProtectLocalMachine); !errors.Is(err, ErrNotSupported) {
		t.Errorf("ProtectData err = %v", err)
	}
	if _, err := UnprotectData([]byte("x"), nil, 0); !errors.Is(err, ErrNotSupported) {
		t.Errorf("UnprotectData err = %v", err)
	}
}
