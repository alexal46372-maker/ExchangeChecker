package model

// Sample limits (phase 6, G1/G4, CHK-SQL-02/04): sample / preview rows go
// to the tray over IPC, so free text read from the database is bounded.
const (
	// MaxSampleCellRunes caps a text cell of a sample row (SampleRow.Point)
	// in characters (runes), not bytes.
	MaxSampleCellRunes = 256
	// MaxSampleRows caps the rows of one preview (sqlcheck.PreviewMapping).
	// Worst case of one preview: 100 x (256 runes x 6 bytes of JSON
	// escaping + times) ~ 160 KiB, far below IPCMaxMessage.
	MaxSampleRows = 100
)

// TruncateSampleCell cuts s to MaxSampleCellRunes characters on a rune
// boundary and reports whether it was cut. Invalid UTF-8 bytes count as
// one character each (they are never split).
func TruncateSampleCell(s string) (string, bool) {
	if len(s) <= MaxSampleCellRunes { // fast path: <= 256 bytes = <= 256 runes
		return s, false
	}
	n := 0
	for i := range s {
		if n == MaxSampleCellRunes {
			return s[:i], true
		}
		n++
	}
	return s, false
}

// TruncateSampleRows applies TruncateSampleCell to the Point of every row
// in place and sets PointTruncated.
func TruncateSampleRows(rows []SampleRow) {
	for i := range rows {
		rows[i].truncate()
	}
}

func (r *SampleRow) truncate() {
	var cut bool
	r.Point, cut = TruncateSampleCell(r.Point)
	r.PointTruncated = r.PointTruncated || cut
}

// TruncateCandidates applies TruncateSampleRows to the sample rows and the
// matched row of every candidate in place.
func TruncateCandidates(cands []DiscoveryCandidate) {
	for i := range cands {
		TruncateSampleRows(cands[i].SampleRows)
		if cands[i].Matched != nil {
			cands[i].Matched.truncate()
		}
	}
}
