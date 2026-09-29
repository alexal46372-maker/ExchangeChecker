package monitor

import (
	"context"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestProfile runs IPC "test_profile" (CHK-IPC-03).
//
// The profile is the saved one (req.ProfileID, its saved password) or the
// unsaved req.Profile with req.Password. Rules:
//   - Windows authentication runs as the service account, so it is tested
//     only for a saved profile with exactly its saved connection
//     parameters; otherwise the result is category config with
//     MsgTestWindowsUnsaved (no connection is made).
//   - SQL authentication with an empty password reuses the saved password
//     of the same profile ID only if host, instance, port, login and auth
//     mode are equal and the encryption is not weaker
//     (model.CanReuseSavedPassword); otherwise invalid_request with
//     MsgTestPasswordRequired — the saved password is never sent to
//     another server or over a weaker channel.
//   - EVERY request goes through the command rate limit first
//     (IPCCommandInterval, error rate_limited; phase 4b: before the
//     profile / password checks), so the pipe can be used neither as a
//     port scanner nor as an oracle for the saved parameters; a test
//     then goes through the Tester (max 2 at a time, error busy; single
//     15 s deadline).
//
// The password is never logged, never stored and scrubbed from errors.
func (m *Monitor) TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error) {
	// the rate limit comes FIRST (phase 4b): every request — also one
	// answered without a connection (invalid_request "enter the password",
	// Windows authentication of an unsaved profile, not_found) — uses the
	// slot, so the pipe is neither a port scanner nor an unlimited oracle
	// for guessing which parameters match a saved profile
	m.cmdMu.Lock()
	ok := m.allowLocked(cmdKeyTest)
	m.cmdMu.Unlock()
	if !ok {
		return model.TestProfileResult{}, ErrRateLimited
	}
	p, pw, res, err := m.resolveTest(req)
	if err != nil || res != nil {
		if res != nil {
			m.opt.Log.Printf("test_profile %s (%s/%s): %s", p.ID, p.Server, p.Database, res.Category)
			return *res, nil
		}
		return model.TestProfileResult{}, err
	}
	r, err := m.tester.Run(ctx, p, pw)
	if err != nil {
		m.opt.Log.Printf("test_profile %s (%s/%s): %v", p.ID, p.Server, p.Database, err)
		return r, err
	}
	m.opt.Log.Printf("test_profile %s (%s/%s, encrypt %s): %s in %d ms %s", p.ID, p.Server, p.Database,
		r.Encrypt, r.Category, r.DurationMs, r.Error)
	return r, nil
}

// testRequestError is an invalid_request error with a message key.
func testRequestError(key, msg string) error {
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: key, Message: msg}
}

// resolveTest picks the profile and password of a test request. res is
// set when the answer is already known without a connection (Windows
// authentication of an unsaved profile).
func (m *Monitor) resolveTest(req model.IPCRequest) (model.Profile, string, *model.TestProfileResult, error) {
	var saved *entry
	lookup := func(id string) {
		if id == "" {
			return
		}
		m.mu.Lock()
		saved = m.entries[id]
		m.mu.Unlock()
	}
	if req.Profile == nil {
		if req.ProfileID == "" {
			return model.Profile{}, "", nil, testRequestError(model.MsgTestInvalidProfile, "test_profile: profileId or profile is required")
		}
		lookup(req.ProfileID)
		if saved == nil {
			return model.Profile{}, "", nil, ErrUnknownProfile
		}
		return saved.profile, saved.password, nil, nil
	}
	p := *req.Profile
	if p.ID == "" {
		p.ID = req.ProfileID
	}
	check := p
	if strings.TrimSpace(check.ID) == "" {
		check.ID = "test"
	}
	check.Mapping, check.Mappings = model.Mapping{}, nil // not needed to connect
	if err := check.Validate(); err != nil {
		return p, "", nil, testRequestError(model.MsgTestInvalidProfile, err.Error())
	}
	lookup(p.ID)
	if p.AuthMode == model.AuthWindows {
		if saved != nil && saved.profile.AuthMode == model.AuthWindows && model.SameConnection(p, saved.profile) {
			return saved.profile, saved.password, nil, nil
		}
		return p, "", &model.TestProfileResult{ProfileID: p.ID, Category: model.TestConfig, ErrorKey: model.MsgTestWindowsUnsaved,
			Error:   "Windows authentication is tested only for a saved profile with its saved connection settings",
			Encrypt: string(p.EffectiveEncrypt()), TLSMin: p.EffectiveTLSMinVersion()}, nil
	}
	if req.Password != "" {
		return p, req.Password, nil, nil
	}
	if saved != nil && saved.password != "" && model.CanReuseSavedPassword(p, saved.profile) {
		return p, saved.password, nil, nil
	}
	return p, "", nil, testRequestError(model.MsgTestPasswordRequired, "test_profile: password required")
}
