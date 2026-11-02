package traysettings

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ResolveDiscovery is the one decision on a Discovery result shared by the
// «Добавить базу» wizard and the expert «Таблицы обмена…» dialog
// (fix/tray-wizard-autofind, CHK-TRAY-83/84): model.SQLAutoPickFor over
// the candidates (computed here, never trusted from a reply). Only an
// autoPick with AutoApply is applied: the main mapping and one mapping per
// other exchange plan (autoPick Plans; 1C 8.1 counters plans are part of
// it, CHK-SQL-16). Otherwise a Problem: none (no candidate), ambiguous
// (two equally good candidates for one table), low_score (below the
// threshold) or incomplete (no send/receive time, no points — e.g. a
// counters plan with 0 points — or the search was cut short).
func ResolveDiscovery(cands []model.DiscoveryCandidate, lang string) (model.Mapping, []model.Mapping, *Problem) {
	main, plans, _, pr := resolveDiscovery(cands, lang)
	return main, plans, pr
}

// Problem codes of ResolveDiscovery and the wizard (Problem.Code).
const (
	ProblemNone           = "none"
	ProblemAmbiguous      = "ambiguous"
	ProblemLowScore       = "low_score"
	ProblemIncomplete     = "incomplete"
	ProblemWindowsUnsaved = "windows_unsaved"
	ProblemTruncated      = "truncated"
)

// resolveDiscovery also returns the best candidate: the main one when
// applied, else the one to preselect under «Подробнее» (-1: none).
func resolveDiscovery(cands []model.DiscoveryCandidate, lang string) (model.Mapping, []model.Mapping, int, *Problem) {
	lang = i18n.Normalize(lang)
	if len(cands) == 0 {
		return model.Mapping{}, nil, -1, &Problem{Code: ProblemNone, Text: i18n.T(i18n.KeyWizErrNone, lang)}
	}
	p := model.SQLAutoPickFor(cands)
	if p.Index < 0 || p.Index >= len(cands) {
		return model.Mapping{}, nil, -1, &Problem{Code: ProblemNone, Text: i18n.T(i18n.KeyWizErrNone, lang)}
	}
	if !p.AutoApply {
		switch p.ReasonKey {
		case model.MsgSQLAutoPickAmbiguous:
			b := cands[p.Index]
			other := -1
			for i, c := range cands {
				if i != p.Index && strings.EqualFold(c.Schema, b.Schema) && strings.EqualFold(c.Table, b.Table) &&
					(other < 0 || c.Score > cands[other].Score) {
					other = i
				}
			}
			if other >= 0 {
				d := Discovery{Lang: lang}
				return model.Mapping{}, nil, p.Index, &Problem{Code: ProblemAmbiguous,
					Text: fmt.Sprintf(i18n.T(i18n.KeyWizErrAmbiguous, lang), ambiguousName(d, b), ambiguousName(d, cands[other]))}
			}
		case model.MsgSQLAutoPickLowScore:
			return model.Mapping{}, nil, p.Index, &Problem{Code: ProblemLowScore, Text: i18n.T(i18n.KeyWizErrLowScore, lang)}
		}
		// incomplete, a counters fallback outside 1C plans, or any other
		// reason: never applied without the user
		return model.Mapping{}, nil, p.Index, &Problem{Code: ProblemIncomplete, Text: i18n.T(i18n.KeyWizErrIncomplete, lang)}
	}
	if len(p.Plans) > 0 {
		main := cands[p.Plans[0]].Mapping()
		var plans []model.Mapping
		seen := map[string]bool{PlanKey(main): true}
		for _, i := range p.Plans[1:] {
			m := cands[i].Mapping()
			if k := PlanKey(m); k != "" && !seen[k] {
				seen[k] = true
				plans = append(plans, m)
			}
		}
		if len(plans)+1 > model.MaxMappings {
			plans = plans[:model.MaxMappings-1]
		}
		return main, plans, p.Plans[0], nil
	}
	return cands[p.Index].Mapping(), nil, p.Index, nil
}

// FoundText is «Найдено: план X (+N), поля …» of a resolved discovery.
func FoundText(main model.Mapping, plans []model.Mapping, lang string) string {
	name := main.PlanTable()
	if name == "" {
		name = MappingName(main)
	}
	s := fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlan, lang), name)
	if len(plans) > 0 {
		s += fmt.Sprintf(i18n.T(i18n.KeyWizFoundPlans, lang), len(plans))
	}
	var cols []string
	for _, c := range []string{main.PointNameColumn, main.PointIDColumn, main.LastSendColumn, main.LastRecvColumn, main.SentNoColumn, main.ReceivedNoColumn} {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	if len(cols) > 0 {
		s += ", " + fmt.Sprintf(i18n.T(i18n.KeyWizFoundFields, lang), strings.Join(cols, ", "))
	}
	return s
}

// ambiguousName names a candidate of an ambiguous result: its list line
// and its columns (two candidates of one table differ only there).
func ambiguousName(d Discovery, c model.DiscoveryCandidate) string {
	var cols []string
	for _, x := range []string{c.PointCol, c.PointIDCol, c.SendCol, c.RecvCol} {
		if x != "" {
			cols = append(cols, x)
		}
	}
	return "«" + strings.TrimSpace(d.CandidateLine(c)) + " (" + strings.Join(cols, ", ") + ")»"
}
