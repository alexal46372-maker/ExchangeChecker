package traysettings

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// HasBrowseSubnet / HasBrowseLocal / HasTestSteps: phase 11 capabilities
// (CHK-TRAY-52); the matching UI is hidden until the service announces them.
func HasBrowseSubnet(has func(string) bool) bool { return has != nil && has(model.CapBrowseSubnet) }
func HasBrowseLocal(has func(string) bool) bool  { return has != nil && has(model.CapBrowseLocal) }
func HasTestSteps(has func(string) bool) bool    { return has != nil && has(model.CapTestSteps) }

// BrowseRequestMode builds sql_browse with an explicit mode ("" | local |
// broadcast | subnet | host). Empty Mode = local+broadcast, or host when
// the server box is filled. Port comes from the port box when > 0.
func (z *Wizard) BrowseRequestMode(mode string) model.IPCRequest {
	host, _ := SplitServer(strings.TrimSpace(z.Server))
	req := model.IPCRequest{Cmd: model.CmdSQLBrowse, Host: strings.TrimSpace(host), Mode: mode}
	if p := strings.TrimSpace(z.Port); p != "" {
		var n int
		fmt.Sscanf(p, "%d", &n)
		if n > 0 && n <= 65535 {
			req.Port = n
		}
	}
	return req
}

// MsgBrowsePublicNetwork: optional advice key if the service adds one
// (FirewallCurrentProfiles via svc).
const MsgBrowsePublicNetwork = "sql.browse_advice_public_network"

// BrowsePublicNetworkHint: when the service reports an active Public
// firewall profile, return the RU/EN hint; else "".
func BrowsePublicNetworkHint(r model.SQLBrowseResult, lang string) string {
	for _, k := range r.Advice {
		if k == MsgBrowsePublicNetwork || strings.Contains(strings.ToLower(k), "public") {
			if m := model.SQLMessage(k, lang); m != "" {
				return m
			}
			return i18n.T(i18n.KeyWizPublicNetwork, lang)
		}
	}
	return ""
}

// FormatBrowseDone builds the result text of a sql_browse reply (shared by
// BrowseDone): count line, public-network hint, advice keys.
func FormatBrowseDone(z *Wizard, r model.SQLBrowseResult) string {
	z.Instances = append([]model.SQLServerInstance(nil), r.Instances...)
	z.BrowsePartial, z.Responders = r.Partial || r.Truncated, r.Responders
	z.BrowseAdvice = append([]string(nil), r.Advice...)
	z.BrowseLocal = r.Local
	var s string
	if len(z.Instances) == 0 {
		s = z.t(i18n.KeyWizNoneFound)
	} else {
		s = fmt.Sprintf(z.t(i18n.KeyWizFound), len(z.Instances), r.Responders)
		if z.BrowsePartial {
			s += " " + z.t(i18n.KeyWizFoundPartial)
		}
	}
	if tip := BrowsePublicNetworkHint(r, z.Lang); tip != "" {
		s += "\r\n" + tip
	}
	for _, k := range r.Advice {
		if k == MsgBrowsePublicNetwork || strings.Contains(strings.ToLower(k), "public") {
			continue // already in tip
		}
		if msg := model.SQLMessage(k, z.Lang); msg != "" {
			s += "\r\n" + msg
		}
	}
	return s
}

// BrowseProgressLine is the status text of one sql_browse_progress push.
func (z *Wizard) BrowseProgressLine(p model.SQLBrowseProgress) string {
	return fmt.Sprintf(z.t(i18n.KeyWizBrowseProgress), p.Phase, p.Done, p.Total, p.Found)
}

// TestStepsRequest builds sql_test_steps (no password over IPC; profileId
// only when editing a saved profile on the same server).
func (z *Wizard) TestStepsRequest(profileID string) model.IPCRequest {
	server := strings.TrimSpace(z.Server)
	if p := strings.TrimSpace(z.Port); p != "" {
		server = server + "," + p
	}
	return model.IPCRequest{Cmd: model.CmdSQLTestSteps, Server: server, ProfileID: profileID}
}

// TestStepLine is one row of the step list (tick / cross + title + detail).
func (z *Wizard) TestStepLine(st model.SQLCheckStep) string {
	mark := "✓"
	if !st.OK {
		mark = "✗"
	}
	title := CheckStepTitle(st.ID, z.Lang)
	line := mark + " " + title
	if st.Ms > 0 {
		line += fmt.Sprintf(" (%d ms)", st.Ms)
	}
	if st.Detail != "" {
		line += " — " + st.Detail
	}
	return line
}

// CheckStepTitle is the RU/EN label of a sql_test_steps stage id.
func CheckStepTitle(id, lang string) string {
	key := map[string]string{
		model.SQLStepParse: i18n.KeyWizCheckParse, model.SQLStepDNS: i18n.KeyWizCheckDNS,
		model.SQLStepBrowser: i18n.KeyWizCheckBrowser, model.SQLStepTCP: i18n.KeyWizCheckTCP,
		model.SQLStepPrelogin: i18n.KeyWizCheckPrelogin, model.SQLStepTLS: i18n.KeyWizCheckTLS,
		model.SQLStepLogin: i18n.KeyWizCheckLogin, model.SQLStepDB: i18n.KeyWizCheckDB,
		model.SQLStepTables: i18n.KeyWizCheckTables,
	}[id]
	if key == "" {
		return id
	}
	return i18n.T(key, lang)
}

// TestStepsAdvice under the first failed step (error + advice keys).
func (z *Wizard) TestStepsAdvice(steps []model.SQLCheckStep) string {
	for _, st := range steps {
		if st.OK {
			continue
		}
		var parts []string
		if st.ErrorKey != "" {
			if m := model.SQLMessage(st.ErrorKey, z.Lang); m != "" {
				parts = append(parts, m)
			}
		}
		key := st.AdviceKey
		if key == "" {
			key = model.SQLCheckAdviceKey(st.ID)
		}
		if m := model.SQLMessage(key, z.Lang); m != "" {
			parts = append(parts, m)
		}
		return strings.Join(parts, "\r\n")
	}
	return ""
}

// RetryAfterSeconds turns retryAfterMs into a whole-second countdown (≥1).
func RetryAfterSeconds(ms int64) int64 {
	sec := (ms + 999) / 1000
	if sec < 1 {
		sec = 1
	}
	return sec
}
