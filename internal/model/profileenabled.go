package model

// Phase 13 (EC Service, CHK-MON-21 / CHK-IPC-18): enable / disable one
// profile without a full save_profile.
//
//	{cmd:"set_enabled", profileId, enabled, expectedRevision?} (admin)
//	→ ok {profileId, enabled, revision}
//	push {type:"profile_enabled", profileId, enabled, revision} to all clients
const (
	CapProfileEnabled = "profile_enabled"
	CmdSetEnabled     = "set_enabled"
	MsgProfileEnabled = "profile_enabled"
)
