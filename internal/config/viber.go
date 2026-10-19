package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Phase 10 (CHK-SVC-06): the Viber bot token (viber.dpapi: DPAPI
// LocalMachine like secrets.dpapi, DACL SYSTEM+Administrators). The
// settings are settings.viber (settings.json). Neither is part of
// export_config.
const ViberTokenFile = "viber.dpapi"

type viberTokenDoc struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
}

// HasViberToken reports whether viber.dpapi exists (no decryption).
func (s *Store) HasViberToken() bool {
	fi, err := os.Stat(s.Path(ViberTokenFile))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// LoadViberToken decrypts viber.dpapi ("" when missing).
func (s *Store) LoadViberToken() (string, error) {
	raw, err := s.ReadFileRaw(ViberTokenFile)
	if err != nil || raw == nil {
		return "", err
	}
	p, err := s.protector()
	if err != nil {
		return "", err
	}
	plain, err := p.Unprotect(raw)
	if err != nil {
		return "", fmt.Errorf("config: decrypt %s: %w", ViberTokenFile, err)
	}
	defer wipe(plain)
	var doc viberTokenDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return "", fmt.Errorf("config: parse %s: %w", ViberTokenFile, err)
	}
	return doc.Token, nil
}

// SaveViberToken encrypts and atomically writes viber.dpapi; "" removes
// the file.
func (s *Store) SaveViberToken(token string) error {
	if token == "" {
		err := os.Remove(s.Path(ViberTokenFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	p, err := s.protector()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(viberTokenDoc{Version: SchemaVersion, Token: token})
	if err != nil {
		return err
	}
	defer wipe(plain)
	blob, err := p.Protect(plain)
	if err != nil {
		return fmt.Errorf("config: encrypt %s: %w", ViberTokenFile, err)
	}
	return writeSecretAtomic(s.Dir, ViberTokenFile, blob)
}
