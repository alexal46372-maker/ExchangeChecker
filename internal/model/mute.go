package model

// Phase 12 (EC Service, CHK-MON-20 / CHK-IPC-15): mute a profile until it
// recovers. mute_until_recovered / unmute_profile by profileId; capability
// mute_profile. Access level is the same as ack_alert (any local client,
// not admin-only). While muted: no alert / alert_repeat / Viber / Event Log
// for that profile; polling continues; get_status shows muted + mutedSince
// on the profile. Clear: unmute_profile or the profile returns to LevelOK.
// Persisted next to ack-state.json (mute-state.json).

const (
	CmdMuteUntilRecovered = "mute_until_recovered"
	CmdUnmuteProfile      = "unmute_profile"
	CapMuteProfile        = "mute_profile"

	ResultMuted   = "muted"
	ResultUnmuted = "unmuted"
)
