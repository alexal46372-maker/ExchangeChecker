package trayalert

import (
	"strings"
	"testing"
)

// TestCHK_TRAY_105_SoundOutput — phase 16 E: «Вывод звука» per level.
// Stored per level in the HKCU JSON ("output"), the defaults are the PC
// speaker for both levels, a missing / unknown value normalizes to the
// speaker (old settings keep their behaviour), the three values survive a
// save / load round trip independently for «Предупреждение» and «Ошибка»;
// the old per-level volume no longer changes the sound (SoundFor).
func TestCHK_TRAY_105_SoundOutput(t *testing.T) {
	d := DefaultSoundPrefs(nil)
	if d.Warning.Output != OutputSpeaker || d.Error.Output != OutputSpeaker {
		t.Fatalf("defaults %+v", d)
	}
	if len(SoundOutputs) != 3 || SoundOutputs[0] != OutputSpeaker || SoundOutputs[1] != OutputSpeakers || SoundOutputs[2] != OutputBoth {
		t.Fatalf("order %v", SoundOutputs)
	}
	// an old stored value (phase 15): no "output" → the speaker
	old := `{"warning":{"kind":"beep","freqHz":800,"durMs":200,"volume":30,"loop":true,"gapMs":500},` +
		`"error":{"kind":"beep","freqHz":1200,"durMs":300,"volume":0,"loop":true,"gapMs":400},"master":60}`
	p, err := ParseSoundPrefs(old, d)
	if err != nil || p.Warning.Output != OutputSpeaker || p.Error.Output != OutputSpeaker || p.Error.Volume != 0 {
		t.Fatalf("old value: %+v %v", p, err)
	}
	if s := SoundFor(p.Error, p.Master); !s.ToneOn || s.Volume != 60 {
		t.Errorf("old per-level volume 0 %% silenced the error tone: %+v", s)
	}
	// unknown value → the speaker
	p, _ = ParseSoundPrefs(strings.Replace(old, `"loop":true,"gapMs":500}`, `"loop":true,"gapMs":500,"output":"radio"}`, 1), d)
	if p.Warning.Output != OutputSpeaker {
		t.Errorf("unknown output %q", p.Warning.Output)
	}
	// per level, round trip
	for _, w := range SoundOutputs {
		for _, e := range SoundOutputs {
			q := d
			q.Warning.Output, q.Error.Output = w, e
			raw := q.Normalized(d).String()
			if !strings.Contains(raw, `"output":"`+string(w)+`"`) {
				t.Fatalf("stored %s", raw)
			}
			back, err := ParseSoundPrefs(raw, d)
			if err != nil || back.Warning.Output != w || back.Error.Output != e {
				t.Fatalf("%s/%s: %+v %v", w, e, back, err)
			}
		}
	}
	// a file keeps its output setting (disabled in the UI) but plays the file
	f := SoundProfile{Kind: SoundFile, File: `C:\a.wav`, FreqHz: 800, DurMs: 100, Output: OutputBoth}.Normalized(d.Warning)
	if f.Kind != SoundFile || f.Output != OutputBoth {
		t.Errorf("file profile %+v", f)
	}
}
