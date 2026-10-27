package monitor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_08_CloneProfile — clone_profile copies connection, mapping
// and thresholds; never the password; default name "<old> (копия)"; new
// unique id (or newId if free); needPassword true; expectedRevision
// mandatory; duplicate newId / unknown source -> errors.
func TestCHK_CFG_08_CloneProfile(t *testing.T) {
	e := newCfgEnv(t)
	src := sqlProf6("s1")
	src.Name = "Склад"
	src.WarnMinutes, src.ErrorMinutes, src.StalledMinutes = 15, 45, 90
	src.Mapping = model.Mapping{Mode: model.MappingAuto, Table: "_Node1", PointNameColumn: "n", LastSendColumn: "s", LastRecvColumn: "r"}
	src.Encrypt, src.TLSMinVersion = model.EncryptDisable, "1.0"
	if _, err := e.m.SaveProfile(src, "Pw-Secret!", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if e.secrets()["s1"] != "Pw-Secret!" {
		t.Fatal("setup secret")
	}

	res, needPW, err := e.m.CloneProfile("s1", "", "", e.rev(), "a")
	if err != nil || !needPW || res.Profile.ID == "" || res.Profile.ID == "s1" || res.Revision == "" {
		t.Fatalf("%+v need=%v %v", res, needPW, err)
	}
	p := res.Profile
	if p.Name != "Склад (копия)" || p.HasPassword || p.Server != src.Server || p.Login != "user1" ||
		p.WarnMinutes != 15 || p.ErrorMinutes != 45 || p.StalledMinutes != 90 ||
		p.Mapping.Table != "_Node1" || p.Encrypt != model.EncryptDisable {
		t.Fatalf("clone fields %+v", p)
	}
	if _, ok := e.secrets()[p.ID]; ok || e.secrets()["s1"] != "Pw-Secret!" {
		t.Fatalf("secrets touched %v", e.secrets())
	}
	view, _, _ := e.m.GetConfig()
	if len(view.Profiles) != 2 {
		t.Fatalf("profiles %d", len(view.Profiles))
	}

	// explicit newId + newName (EN settings)
	st := model.DefaultSettings()
	st.Language = model.LangEN
	if _, err := e.m.SaveSettings(st, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	res2, needPW, err := e.m.CloneProfile("s1", "clone-x", "My Copy", e.rev(), "a")
	if err != nil || !needPW || res2.Profile.ID != "clone-x" || res2.Profile.Name != "My Copy" {
		t.Fatalf("%+v %v %v", res2, needPW, err)
	}
	// default EN name
	res3, _, err := e.m.CloneProfile("s1", "", "", e.rev(), "a")
	if err != nil || res3.Profile.Name != "Склад (copy)" {
		t.Fatalf("en name %+v %v", res3, err)
	}

	wantCode(t, "dup", func() error {
		_, _, err := e.m.CloneProfile("s1", "clone-x", "", e.rev(), "a")
		return err
	}(), model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid)
	wantCode(t, "missing", func() error {
		_, _, err := e.m.CloneProfile("nope", "", "", e.rev(), "a")
		return err
	}(), model.ErrCodeNotFound, model.MsgMonitorUnknownProfile)
	wantCode(t, "rev", func() error {
		_, _, err := e.m.CloneProfile("s1", "", "", "", "a")
		return err
	}(), model.ErrCodeInvalidRequest, model.MsgMonitorBadRequest)

	// Windows auth clone still needPassword true (no secret copied)
	w := prof("w1")
	w.Name = "Win"
	if _, err := e.m.SaveProfile(w, "", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	rw, needPW, err := e.m.CloneProfile("w1", "", "", e.rev(), "a")
	if err != nil || !needPW || rw.Profile.Name != "Win (copy)" {
		t.Fatalf("win %+v %v %v", rw, needPW, err)
	}
}

// TestCHK_CFG_08_CloneProfileJSON — reply shape fields.
func TestCHK_CFG_08_CloneProfileJSON(t *testing.T) {
	e := newCfgEnv(t)
	if _, err := e.m.SaveProfile(sqlProf6("s1"), "x", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	res, needPW, err := e.m.CloneProfile("s1", "n1", "N", e.rev(), "a")
	if err != nil {
		t.Fatal(err)
	}
	msg := model.IPCMessage{Type: model.MsgOK, Result: model.ResultCloned, ProfileID: res.Profile.ID,
		Revision: res.Revision, NeedPassword: needPW, Profile: &res.Profile, Reloaded: true}
	b, _ := json.Marshal(msg)
	for _, k := range []string{`"result":"cloned"`, `"profileId":"n1"`, `"needPassword":true`, `"revision":`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("json %s lacks %s", b, k)
		}
	}
	if strings.Contains(string(b), "Pw-") || strings.Contains(string(b), `"password"`) {
		t.Fatalf("password leaked %s", b)
	}
}
