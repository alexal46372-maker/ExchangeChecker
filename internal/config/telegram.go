package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Phase 13 (CHK-CFG-10): Telegram bot token (telegram.dpapi: DPAPI LocalMachine
// like viber.dpapi / secrets.dpapi, DACL SYSTEM+Administrators).
const TelegramTokenFile = "telegram.dpapi"

type telegramTokenDoc struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
}

// HasTelegramToken reports whether telegram.dpapi exists (no decryption).
func (s *Store) HasTelegramToken() bool {
	fi, err := os.Stat(s.Path(TelegramTokenFile))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// LoadTelegramToken decrypts telegram.dpapi ("" when missing).
func (s *Store) LoadTelegramToken() (string, error) {
	raw, err := s.ReadFileRaw(TelegramTokenFile)
	if err != nil || raw == nil {
		return "", err
	}
	p, err := s.protector()
	if err != nil {
		return "", err
	}
	plain, err := p.Unprotect(raw)
	if err != nil {
		return "", fmt.Errorf("config: decrypt %s: %w", TelegramTokenFile, err)
	}
	defer wipe(plain)
	var doc telegramTokenDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return "", fmt.Errorf("config: parse %s: %w", TelegramTokenFile, err)
	}
	return doc.Token, nil
}

// SaveTelegramToken encrypts and atomically writes telegram.dpapi; "" removes
// the file.
func (s *Store) SaveTelegramToken(token string) error {
	if token == "" {
		unlock := LockPath(s.Path(TelegramTokenFile))
		err := os.Remove(s.Path(TelegramTokenFile))
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
	plain, err := json.Marshal(telegramTokenDoc{Version: SchemaVersion, Token: token})
	if err != nil {
		return err
	}
	defer wipe(plain)
	blob, err := p.Protect(plain)
	if err != nil {
		return fmt.Errorf("config: encrypt %s: %w", TelegramTokenFile, err)
	}
	return writeSecretAtomic(s.Dir, TelegramTokenFile, blob)
}
