package winapi

// SCM structure layouts shared with tests (amd64 sizes, CHK-SETUP-03).
// The Windows wrappers live in scm_windows.go.

// SCAction is SC_ACTION (8 bytes on amd64).
type SCAction struct {
	Type  uint32
	Delay uint32 // milliseconds
}

// ServiceFailureActionsW is SERVICE_FAILURE_ACTIONSW (40 bytes on amd64).
type ServiceFailureActionsW struct {
	ResetPeriod uint32
	_           uint32 // padding to align pointers on amd64
	RebootMsg   *uint16
	Command     *uint16
	ActionsLen  uint32
	_           uint32
	Actions     *SCAction
}

// ServiceDescriptionW is SERVICE_DESCRIPTIONW.
type ServiceDescriptionW struct {
	Description *uint16
}

// ServiceDelayedAutoStartInfo is SERVICE_DELAYED_AUTO_START_INFO.
type ServiceDelayedAutoStartInfo struct {
	DelayedAutoStart uint32 // BOOL
}

// ServiceFailureActionsFlag is SERVICE_FAILURE_ACTIONS_FLAG.
type ServiceFailureActionsFlag struct {
	FailureActionsOnNonCrashFailures uint32 // BOOL
}
