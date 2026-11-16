package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// TestCHK_TRAY_99_SpeakerLine — phase 14: the status line of the PC
// speaker, the «Тон» label and volume (master 0 % = silent, > 0 % = full
// level; phase 16: no per-level volume).
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
		// the service's reasons and notify.speaker_* keys — short Russian texts
		{model.SpeakerStatus{Reason: model.SpeakerNotInstalled}, "Системный динамик: недоступен (драйвер не установлен) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.SpeakerUnsupportedOS}, "Системный динамик: недоступен (не поддерживается этой версией Windows) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.SpeakerBlocked}, "Системный динамик: недоступен (драйвер заблокирован) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.SpeakerOpenFailed}, "Системный динамик: недоступен (не удалось открыть драйвер) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.SpeakerModuleFailed}, "Системный динамик: недоступен (не удалось загрузить драйвер) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerNotInstalled}, "Системный динамик: недоступен (драйвер не установлен) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerBusy}, "Системный динамик: недоступен (динамик занят другим подключением) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerNotOwner}, "Системный динамик: недоступен (тон включило другое подключение) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerInvalid}, "Системный динамик: недоступен (неверная частота или длительность тона) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerFailed}, "Системный динамик: недоступен (ошибка обращения к динамику) — тон звучит через колонки"},
		{model.SpeakerStatus{Reason: model.MsgSpeakerDenied}, "Системный динамик: недоступен (служба отклонила запрос тона) — тон звучит через колонки"},
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
	if l := i18n.T(i18n.KeySoundBeep); l != "Тон" { // phase 16 E: «Вывод звука» says where
		t.Errorf("label %q", l)
	}
	// volume: phase 16 E (CHK-TRAY-105) — the only volume is the tray
	// slider: silent only at master 0 %, the old per-level volume is
	// ignored; files play at the master volume
	for _, c := range []struct{ prof, master, want int }{
		{0, 100, 100}, {80, 0, 0}, {0, 0, 0}, {1, 50, 100}, {1, 1, 100}, {50, 50, 100}, {100, 100, 100},
	} {
		snd := trayalert.SoundFor(trayalert.SoundProfile{Kind: trayalert.SoundBeep, Volume: c.prof}, c.master)
		if got := trayalert.ToneLevel(snd); got != c.want {
			t.Errorf("%d×%d: level %d, want %d", c.prof, c.master, got, c.want)
		}
		if snd.Volume != c.master {
			t.Errorf("%d×%d: file volume %d, want the master %d", c.prof, c.master, snd.Volume, c.master)
		}
	}
	if trayalert.ToneLevel(trayalert.Sound{Volume: 0}) != 0 || trayalert.ToneLevel(trayalert.Sound{Volume: 30}) != 100 {
		t.Error("a hand-built Sound: Volume decides")
	}
}
