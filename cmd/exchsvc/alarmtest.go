package main

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/notify"
)

// alarmTester: the Viber message of alarm_test (no Event Log entry) (phase 11, CHK-IPC-11).
type alarmTester struct {
	viber *notify.Notifier
	lang  string
}

func (a alarmTester) AlarmTest(ctx context.Context, e model.AlertEvent, viber bool, account string) (model.AlarmTestResult, error) {
	var res model.AlarmTestResult
	if viber {
		text := "Тест"
		if a.lang == model.LangEN {
			text = "Test"
		}
		vr, key := a.viber.TestMessage(ctx, "ExchangeChecker: "+text)
		res.Viber = &model.AlarmTestViber{ViberTestResult: *vr, ErrorKey: key}
	}
	return res, nil
}
