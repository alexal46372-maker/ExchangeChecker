package trayipc

// Phase 17 (Alex 17.11.2026, CHK-TRAY-109): pausing the screen flash while
// the alert window is minimized. Protocol agreed with EC Service
// (feat/svc-phase17, not merged yet):
//
//   - pause: the existing dismiss_flash {eventId} (CHK-IPC-19);
//   - resume: resume_flash {eventId} clears the dismiss mark; when the event
//     is still active and not confirmed the service pushes flash with the
//     same eventId at once; reply ok {resumed:true}; a confirmed / cleared /
//     unknown event → ok {resumed:false}, nothing happens; no eventId →
//     invalid_request;
//   - hello capability resume_flash; without it the tray does not pause the
//     flash on minimize at all.
//
// TODO(phase 17, after feat/svc-phase17 is merged): replace these local
// names with the model.* constants of the service branch
// (model.CmdResumeFlash / model.CapResumeFlash) and read the reply's
// resumed field from model.IPCMessage.
const (
	CmdResumeFlash = "resume_flash"
	CapResumeFlash = "resume_flash"
)
