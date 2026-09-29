package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Exchange plans (PLAN.md §5.3, customer clarification: standard platform
// plans such as «Основной» / «Магазин»). Every 1C exchange plan is a table
// _NodeNNN (_IDRRef, _Code, _Description, _SentNo, _ReceivedNo,
// _PredefinedID); NNN is the plan's table type number. Points are tagged
// with their plan table so one profile can show points of several plans.

var reNodeTable = regexp.MustCompile(`(?i)^_Node([0-9]+)$`)

// PlanNoOf returns NNN of a 1C exchange-plan table name "_NodeNNN", else 0.
func PlanNoOf(table string) int {
	m := reNodeTable.FindStringSubmatch(strings.TrimSpace(table))
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// PlanTableOf returns table when it is an exchange-plan table, else "".
func PlanTableOf(table string) string {
	if PlanNoOf(table) == 0 {
		return ""
	}
	return strings.TrimSpace(table)
}

// RefTypeReference is the value of a 1C 8.1 composite-type column
// _FldNNN_TYPE (binary(1)) meaning "the value is a reference": then
// _FldNNN_RTRef (binary(4), big-endian) is the referenced table type number
// (NNN of _NodeNNN / _ReferenceNNN) and _FldNNN_RRRef (binary(16)) the
// referenced _IDRRef.
const RefTypeReference byte = 0x08

// MaxRTRef bounds Mapping.TimesLinkRTRef (a positive int32 table number).
const MaxRTRef = 1<<31 - 1

// RTRefBytes encodes a table type number as the 4-byte big-endian _RTRef.
func RTRefBytes(n int) []byte {
	return []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// RTRefValue decodes a 4-byte big-endian _RTRef value.
func RTRefValue(b []byte) (int, bool) {
	if len(b) != 4 {
		return 0, false
	}
	return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]), true
}

// Composite reports whether the linked mapping uses a 1C composite
// reference: TimesTable rows are filtered on TimesLinkTypeColumn = 0x08 and
// TimesLinkRTRefColumn = RTRefBytes(TimesLinkRTRef), and TimesLinkColumn
// (the _RRRef part) is matched to Table.PointKeyColumn.
func (m Mapping) Composite() bool {
	return strings.TrimSpace(m.TimesLinkTypeColumn) != "" || strings.TrimSpace(m.TimesLinkRTRefColumn) != ""
}

// PlanTable returns the exchange-plan table of the mapping (Table when it
// is _NodeNNN), else "".
func (m Mapping) PlanTable() string { return PlanTableOf(m.Table) }

// PlanNo returns NNN of the mapping's exchange-plan table, else 0.
func (m Mapping) PlanNo() int { return PlanNoOf(m.Table) }

func (m Mapping) validateComposite() error {
	if !m.Composite() {
		if m.TimesLinkRTRef != 0 {
			return errors.New("mapping: timesLinkRTRef requires timesLinkTypeColumn and timesLinkRTRefColumn")
		}
		return nil
	}
	switch {
	case !m.Linked():
		return errors.New("mapping: a composite link requires timesTable")
	case strings.TrimSpace(m.TimesLinkTypeColumn) == "" || strings.TrimSpace(m.TimesLinkRTRefColumn) == "":
		return errors.New("mapping: a composite link needs both timesLinkTypeColumn (_TYPE) and timesLinkRTRefColumn (_RTRef)")
	case m.TimesLinkRTRef <= 0 || m.TimesLinkRTRef > MaxRTRef:
		return fmt.Errorf("mapping: timesLinkRTRef must be the referenced table number (1..%d), got %d", MaxRTRef, m.TimesLinkRTRef)
	}
	return nil
}

// AllMappings returns the primary mapping followed by the additional
// mappings (one per exchange-plan table / times table). An unconfigured
// primary mapping yields nil.
func (p Profile) AllMappings() []Mapping {
	if !p.Mapping.Configured() {
		return nil
	}
	return append([]Mapping{p.Mapping}, p.Mappings...)
}

func (p Profile) validateMappings() error {
	if len(p.Mappings) == 0 {
		return nil
	}
	if !p.Mapping.Configured() {
		return errors.New("mappings: additional mappings require a configured primary mapping")
	}
	for i, m := range p.Mappings {
		if !m.Configured() {
			return fmt.Errorf("mappings[%d]: not configured (table or manual SQL required)", i)
		}
		if err := m.Validate(); err != nil {
			return fmt.Errorf("mappings[%d]: %w", i, err)
		}
	}
	return nil
}

// NodePoint is one row of a 1C exchange-plan table, listed for the tray so
// the user can pick a known point for DiscoverByValue (JSON for IPC).
type NodePoint struct {
	PlanTable string `json:"planTable"`
	PlanNo    int    `json:"planNo"`
	Ref       string `json:"ref,omitempty"` // hex _IDRRef
	Code      string `json:"code,omitempty"`
	Name      string `json:"name"`
}

// SuggestPlanMappings turns ranked candidates (best first) into one mapping
// per exchange-plan table: the best timestamp candidate (single or linked)
// of every _NodeNNN table. The first mapping is meant as Profile.Mapping,
// the others as Profile.Mappings. Counters candidates are not used.
func SuggestPlanMappings(cands []DiscoveryCandidate) []Mapping {
	seen := map[string]bool{}
	var out []Mapping
	for _, c := range cands {
		if c.Mode == CandidateCounters {
			continue
		}
		pt := PlanTableOf(c.Table)
		if pt == "" {
			continue
		}
		k := strings.ToLower(c.Schema + "." + pt)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, c.Mapping())
	}
	return out
}
