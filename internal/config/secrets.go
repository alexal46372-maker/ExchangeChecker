package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Protector encrypts/decrypts the secrets file payload.
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(blob []byte) ([]byte, error)
}

// SecretsEntropy is the DPAPI optional entropy for secrets.dpapi.
var SecretsEntropy = []byte("ExchangeChecker/secrets.dpapi/v1")

// DPAPIProtector uses CryptProtectData with CRYPTPROTECT_LOCAL_MACHINE |
// CRYPTPROTECT_UI_FORBIDDEN: the tray (user) encrypts, the LocalSystem
// service decrypts. Access boundary = ACL of the ProgramData folder
// (PLAN.md §4.1, §12 p.9).
type DPAPIProtector struct {
	Entropy []byte
}

// Protect implements Protector.
func (d DPAPIProtector) Protect(plain []byte) ([]byte, error) {
	return winapi.ProtectData(plain, d.Entropy, winapi.CryptProtectLocalMachine|winapi.CryptProtectUIForbidden)
}

// Unprotect implements Protector.
func (d DPAPIProtector) Unprotect(blob []byte) ([]byte, error) {
	return winapi.UnprotectData(blob, d.Entropy, winapi.CryptProtectUIForbidden)
}

// DefaultProtector returns the DPAPI machine-key protector.
func DefaultProtector() Protector { return DPAPIProtector{Entropy: SecretsEntropy} }

// ErrNoProtector is returned when a Store has no Protector.
var ErrNoProtector = errors.New("config: no secrets protector configured")

// Secrets maps profile ID -> password.
type Secrets map[string]string

// Prune deletes secrets of profiles that no longer exist; returns count.
func (s Secrets) Prune(profiles []model.Profile) int {
	keep := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		keep[p.ID] = true
	}
	n := 0
	for id := range s {
		if !keep[id] {
			delete(s, id)
			n++
		}
	}
	return n
}

type secretsDoc struct {
	Version   int               `json:"version"`
	Passwords map[string]string `json:"passwords"`
}

func (s *Store) protector() (Protector, error) {
	if s.Protector == nil {
		return nil, ErrNoProtector
	}
	return s.Protector, nil
}

// LoadSecrets decrypts secrets.dpapi. Missing file -> empty map, nil error.
func (s *Store) LoadSecrets() (Secrets, error) {
	b, err := s.ReadFileRaw(SecretsFile)
	if err != nil {
		return nil, err
	}
	return s.DecodeSecrets(b)
}

// DecodeSecrets is LoadSecrets over raw file bytes (ReadFileRaw; nil =
// missing file -> empty map).
func (s *Store) DecodeSecrets(raw []byte) (Secrets, error) {
	b := trimBOM(raw)
	if b == nil {
		return Secrets{}, nil
	}
	p, err := s.protector()
	if err != nil {
		return nil, err
	}
	plain, err := p.Unprotect(b)
	if err != nil {
		return nil, fmt.Errorf("config: decrypt %s: %w", SecretsFile, err)
	}
	defer wipe(plain)
	var doc secretsDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", SecretsFile, err)
	}
	if err := checkVersion(SecretsFile, doc.Version); err != nil {
		return nil, err
	}
	sec := Secrets{}
	for k, v := range doc.Passwords {
		sec[k] = v
	}
	return sec, nil
}

// SaveSecrets encrypts and atomically writes secrets.dpapi.
func (s *Store) SaveSecrets(sec Secrets) error {
	p, err := s.protector()
	if err != nil {
		return err
	}
	doc := secretsDoc{Version: SchemaVersion, Passwords: map[string]string{}}
	for k, v := range sec {
		if k == "" {
			return errors.New("config: secrets: empty profile id")
		}
		doc.Passwords[k] = v
	}
	plain, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("config: marshal secrets: %w", err)
	}
	defer wipe(plain)
	blob, err := p.Protect(plain)
	if err != nil {
		return fmt.Errorf("config: encrypt %s: %w", SecretsFile, err)
	}
	return writeSecretAtomic(s.Dir, SecretsFile, blob)
}

// SecureSecretsFile brings an existing secrets.dpapi to SecretsSDDL
// (Windows: SetNamedSecurityInfoW with PROTECTED_DACL_SECURITY_INFORMATION;
// elsewhere mode 0600). Called once at service start (phase 6b,
// CHK-CFG-03), so a file written before phase 6 (inherited, Users-read
// DACL) does not wait for the next password write. A missing file is not
// an error: present = false, nothing is created.
func (s *Store) SecureSecretsFile() (present bool, err error) {
	path := s.Path(SecretsFile)
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, secureSecretFile(path)
}

// Password returns the stored password for profileID.
func (s *Store) Password(profileID string) (string, bool, error) {
	sec, err := s.LoadSecrets()
	if err != nil {
		return "", false, err
	}
	pw, ok := sec[profileID]
	return pw, ok, nil
}

// SetPassword stores (or, for an empty password, deletes) a profile password.
func (s *Store) SetPassword(profileID, password string) error {
	if profileID == "" {
		return errors.New("config: secrets: empty profile id")
	}
	sec, err := s.LoadSecrets()
	if err != nil {
		return err
	}
	if password == "" {
		delete(sec, profileID)
	} else {
		sec[profileID] = password
	}
	return s.SaveSecrets(sec)
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
