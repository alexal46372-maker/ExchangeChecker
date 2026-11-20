// Package trayalert is the tray alert logic (phase 6b, CHK-TRAY-03),
// without WinAPI: which active alerts the alert window shows, when the
// alert sound plays, is muted and re-armed, the ack_alert requests of
// the «Подтвердить» button and how their replies are handled, the beep
// loop behind a Beeper interface, and the window texts (Russian).
//
// Rules (PLAN §6.2, §6.3, §12 p.43, p.50):
//   - the service decides warning/error; the tray shows the active alerts
//     of the snapshot (StatusSnapshot.Alerts) that are not OK and not
//     acknowledged;
//   - a new alert or a worse level («изменение статуса») opens the window
//     and starts the endless beep (tone of the worst shown level);
//   - since phase 16 (CHK-TRAY-104) the alert window cannot be closed,
//     only minimized; an alert is dismissed only by «Подтвердить»
//     (Controller.Close is not used by the window any more);
//   - «Подтвердить» sends ack_alert per profile (profile-level alert) or
//     per point (pointKey); ok, not_found and no_active_alert remove the
//     alert from the window (the latter two: already gone / acknowledged),
//     other errors are shown in the window and the alert stays.
package trayalert
