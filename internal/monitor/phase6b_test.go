package monitor

import (
	"os"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// editAfterLoad is a FileConfig whose Load is followed by a manual edit of
// settings.json (a racing writer between reading the files and computing
// the revision).
type editAfterLoad struct {
	FileConfig
	edit func()
}

func (e editAfterLoad) Load() Config {
	c := e.FileConfig.Load()
	if e.edit != nil {
		e.edit()
	}
	return c
}

// TestCHK_CFG_03_RevisionFromLoadedBytes — phase 6b: the snapshot's
// configRevision is computed from the very bytes FileConfig.Load parsed,
// so it cannot describe a file that changed after the read (the applied
// settings and the revision always match); the edit is picked up, with
// its own revision, by the next reload; save_* return the revision of
// the reload they triggered (= the snapshot's).
func TestCHK_CFG_03_RevisionFromLoadedBytes(t *testing.T) {
	st := config.NewStore(t.TempDir())
	st.Protector = plainProtector{}
	s1 := model.DefaultSettings()
	s1.WarnMinutes, s1.ErrorMinutes = 11, 22
	if err := st.SaveSettings(s1); err != nil {
		t.Fatal(err)
	}
	raw1, _ := st.ReadFileRaw(config.SettingsFile)
	rev1 := revisionOf(raw1, nil, nil)
	s2 := s1
	s2.WarnMinutes = 12
	edited := false
	src := editAfterLoad{FileConfig: FileConfig{Store: st}, edit: func() {
		if !edited {
			edited = true
			if err := st.SaveSettings(s2); err != nil {
				t.Error(err)
			}
		}
	}}
	opt := baseOpt(newFactory(), src, &fakeClock{t: t0})
	opt.ConfigStore = st
	opt.CommandInterval = -1
	m := New(opt)
	m.reloadConfig(true)
	snap := m.Snapshot()
	if snap.Settings == nil || snap.Settings.WarnMinutes != 11 {
		t.Fatalf("applied settings: %+v", snap.Settings)
	}
	if snap.ConfigRevision != rev1 {
		t.Fatalf("configRevision %s, want %s (the revision of the applied bytes)", snap.ConfigRevision, rev1)
	}
	disk, _ := configRevision(st)
	if disk == rev1 {
		t.Fatal("precondition: the edit did not change the files")
	}
	// next reload applies the edit with its own revision
	m.reloadConfig(true)
	if snap := m.Snapshot(); snap.Settings.WarnMinutes != 12 || snap.ConfigRevision != disk {
		t.Fatalf("after reload: warn %d rev %s (want 12 / %s)", snap.Settings.WarnMinutes, snap.ConfigRevision, disk)
	}
	// a save returns the revision of the bytes its reload applied
	rev, err := m.SaveSettings(s1, disk, "x")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(st.Path(config.SettingsFile))
	if want := revisionOf(raw, nil, nil); rev != want || m.Snapshot().ConfigRevision != want {
		t.Fatalf("save_settings revision %s, snapshot %s, want %s", rev, m.Snapshot().ConfigRevision, want)
	}
}
