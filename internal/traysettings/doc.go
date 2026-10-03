// Package traysettings is the platform-independent logic of the tray's
// database settings window (phase 6c, CHK-TRAY-04/05/06): the profile
// form and its conversion to model.Profile, local validation, password
// and encryption hints, the read-only / administrator editor state with
// the configuration revision, the IPC requests (get_config, save_profile,
// delete_profile, test_profile, poll_now) and the texts of their replies.
//
// The tray never writes configuration files: every change is a
// save_profile / delete_profile request to the service, which checks the
// administrator rights itself (PLAN.md §12 p.46, 57). The window itself
// is internal/wintray.
package traysettings
