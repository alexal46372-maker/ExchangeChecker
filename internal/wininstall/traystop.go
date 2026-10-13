package wininstall

// TrayProc is one tray process under the install folder (CHK-SETUP-09).
type TrayProc struct {
	PID     uint32
	Session uint32
}

// SplitTrayStop separates same-session PIDs (post TrayExitMessage) from
// other-session PIDs (TerminateProcess). Decision 9 / PLAN §7.3.
func SplitTrayStop(selfSession uint32, procs []TrayProc) (post, terminate []uint32) {
	for _, p := range procs {
		if p.Session == selfSession {
			post = append(post, p.PID)
		} else {
			terminate = append(terminate, p.PID)
		}
	}
	return post, terminate
}
