package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 10 (CHK-SVC-06): Viber settings (viber.json) and the bot token
// (viber.dpapi: DPAPI LocalMachine like secrets.dpapi, DACL
// SYSTEM+Administrators). Neither is part of export_config.
const (
	ViberFile      = "viber.json"
	ViberTokenFile = "viber.dpapi"
)

type viberDoc struct {
	Version int                 `json:"version"`
	Viber   model.ViberSettings `json:"viber"`
}

type viberTokenDoc struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
}

// LoadViber reads viber.json; missing = model.DefaultViberSettings().
func (s *Store) LoadViber() (model.ViberSettings, error) {
	b, err := s.readFile(ViberFile)
	if err != nil {
		return model.DefaultViberSettings(), err
	}
	if b == nil {
		return model.DefaultViberSettings(), nil
	}
	doc := viberDoc{Viber: model.DefaultViberSettings()}
	if err := json.Unmarshal(b, &doc); err != nil {
		return model.DefaultViberSettings(), fmt.Errorf("config: parse %s: %w", ViberFile, err)
	}
	if err := checkVersion(ViberFile, doc.Version); err != nil {
		return model.DefaultViberSettings(), err
	}
	if doc.Viber.Receivers == nil {
		doc.Viber.Receivers = []string{}
	}
	if err := doc.Viber.Validate(); err != nil {
		return model.DefaultViberSettings(), fmt.Errorf("config: %s: %w", ViberFile, err)
	}
	return doc.Viber, nil
}

// SaveViber validates and atomically writes viber.json.
func (s *Store) SaveViber(v model.ViberSettings) error {
	if err := v.Validate(); err != nil {
		return err
	}
	b, err := marshalJSON(viberDoc{Version: SchemaVersion, Viber: v})
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Dir, ViberFile, b, 0o644)
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
