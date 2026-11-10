package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// TestCHK_TRAY_99_SpeakerLine — phase 14: the status line of the PC
// speaker, the «Тон» label and volume (0 % = silent, > 0 % = full level).
func TestCHK_TRAY_99_SpeakerLine(t *testing.T) {
	for _, c := range []struct {
		st   model.SpeakerStatus
		want string
	}{
		{model.SpeakerStatus{Available: true, DriverVersion: "1.2"}, "Системный динамик: доступен"},
		{model.SpeakerStatus{Reason: "драйвер не установлен"}, "Системный динамик: недоступен (драйвер не установлен) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: trayipc.SpeakerNoService}, "Системный динамик: недоступен (нет связи со службой) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: trayipc.SpeakerNoCap}, "Системный динамик: недоступен (служба не поддерживает системный динамик) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: trayipc.SpeakerTimeout}, "Системный динамик: недоступен (служба не ответила вовремя) — тон звучит через колонки"},
		{model.SpeakerStatus{}, "Системный динамик: недоступен (причина не указана) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgMonitorInternal}, "Системный динамик: недоступен (" + model.MonitorMessage(model.MsgMonitorInternal) + ") — тон звучит через колонки"},
	} {
		if got := SpeakerLine(c.st); got != c.want {
			t.Errorf("%+v: %q, want %q", c.st, got, c.want)
		}
	}
	for _, k := range []string{trayipc.SpeakerNoService, trayipc.SpeakerNoCap, trayipc.SpeakerTimeout} {
		if !i18n.Has(k) {
			t.Errorf("reason %s not in the catalog", k)
		}
	}
	if l := i18n.T(i18n.KeySoundBeep); l != "Тон (системный динамик)" {
		t.Errorf("label %q", l)
	}
	if n := i18n.T(i18n.KeySoundToneVolume); n != "громкость не регулируется" {
		t.Errorf("volume note %q", n)
	}
	// volume: profile × master = 0 % → silent; anything above → full
	for _, c := range []struct{ prof, master, want int }{
		{0, 100, 0}, {80, 0, 0}, {0, 0, 0}, {1, 100, 100}, {50, 50, 100}, {100, 100, 100},
	} {
		if got := trayalert.ToneLevel(trayalert.EffectiveVolume(c.prof, c.master)); got != c.want {
			t.Errorf("%d×%d: level %d, want %d", c.prof, c.master, got, c.want)
		}
	}
}
