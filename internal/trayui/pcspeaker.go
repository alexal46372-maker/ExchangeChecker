package trayui

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// speakerReasons: the reasons of speaker_status and the notify.speaker_*
// error keys of the service → short Russian texts (CHK-TRAY-99).
var speakerReasons = map[string]string{
	model.SpeakerUnsupportedOS:   i18n.KeySpeakerUnsupportedOS,
	model.SpeakerNotInstalled:    i18n.KeySpeakerNotInstalled,
	model.SpeakerBlocked:         i18n.KeySpeakerBlocked,
	model.SpeakerOpenFailed:      i18n.KeySpeakerOpenFailed,
	model.SpeakerModuleFailed:    i18n.KeySpeakerModuleFailed,
	model.MsgSpeakerNotInstalled: i18n.KeySpeakerNotInstalled,
	model.MsgSpeakerBusy:         i18n.KeySpeakerBusy,
	model.MsgSpeakerNotOwner:     i18n.KeySpeakerNotOwner,
	model.MsgSpeakerInvalid:      i18n.KeySpeakerInvalid,
	model.MsgSpeakerFailed:       i18n.KeySpeakerFailed,
	trayipc.SpeakerDenied:        i18n.KeySpeakerDenied, // MERGE: model.MsgSpeakerDenied
}

// SpeakerReason renders the reason of a speaker that is not available:
// a reason / error key of the service (short Russian text), a tray
// catalog key (trayipc.SpeakerNoService / SpeakerNoCap / SpeakerTimeout),
// another service message key, else the text as sent ("" = «причина не
// указана»).
func SpeakerReason(r string) string {
	if k, ok := speakerReasons[r]; ok {
		return i18n.T(k)
	}
	switch {
	case r == "":
		return i18n.T(i18n.KeySpeakerNoReason)
	case i18n.Has(r):
		return i18n.T(r)
	}
	if s := model.MonitorMessage(r); s != "" {
		return s
	}
	return r
}

// SpeakerLine is the status line of the «Звук» section (phase 14,
// CHK-TRAY-99): «Системный динамик: доступен» or «Системный динамик:
// недоступен (причина) — тон звучит через колонки».
func SpeakerLine(st model.SpeakerStatus) string {
	if st.Available {
		return i18n.T(i18n.KeySpeakerAvailable)
	}
	return fmt.Sprintf(i18n.T(i18n.KeySpeakerUnavailable), SpeakerReason(st.Reason))
}
