package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Phase 13b (CHK-CFG-09): SMTP password (email.dpapi: DPAPI LocalMachine
// like viber.dpapi / secrets.dpapi, DACL SYSTEM+Administrators).
const EmailPasswordFile = "email.dpapi"

type emailPasswordDoc struct {
	Version  int    `json:"version"`
	Password string `json:"password"`
}

// HasEmailPassword reports whether email.dpapi exists (no decryption).
func (s *Store) HasEmailPassword() bool {
	fi, err := os.Stat(s.Path(EmailPasswordFile))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// LoadEmailPassword decrypts email.dpapi ("" when missing).
func (s *Store) LoadEmailPassword() (string, error) {
	raw, err := s.ReadFileRaw(EmailPasswordFile)
	if err != nil || raw == nil {
		return "", err
	}
	p, err := s.protector()
	if err != nil {
		return "", err
	}
	plain, err := p.Unprotect(raw)
	if err != nil {
		return "", fmt.Errorf("config: decrypt %s: %w", EmailPasswordFile, err)
	}
	defer wipe(plain)
	var doc emailPasswordDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return "", fmt.Errorf("config: parse %s: %w", EmailPasswordFile, err)
	}
	return doc.Password, nil
}

// SaveEmailPassword encrypts and atomically writes email.dpapi; "" removes
// the file.
func (s *Store) SaveEmailPassword(password string) error {
	if password == "" {
		unlock := LockPath(s.Path(EmailPasswordFile))
		err := os.Remove(s.Path(EmailPasswordFile))
		unlock()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	p, err := s.protector()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(emailPasswordDoc{Version: SchemaVersion, Password: password})
	if err != nil {
		return err
	}
	defer wipe(plain)
	blob, err := p.Protect(plain)
	if err != nil {
		return fmt.Errorf("config: encrypt %s: %w", EmailPasswordFile, err)
	}
	return writeSecretAtomic(s.Dir, EmailPasswordFile, blob)
}
