package dto

import (
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ValueSnapshotView is the per-source observed value inside a Diff
// response. Raw is the canonical comparison string; FetchedAt and
// ReflectedBlock give the frontend what it needs to render anchor-
// window diagnostics.
type ValueSnapshotView struct {
	SourceID       string    `json:"source_id"`
	Raw            string    `json:"raw"`
	FetchedAt      time.Time `json:"fetched_at"`
	ReflectedBlock *uint64   `json:"reflected_block,omitempty" doc:"Block height this observation reflects, when the source exposes it"`
}

// SubjectView mirrors diff.Subject on the wire.
type SubjectView struct {
	Type    string  `json:"type" enum:"block,address,contract,chain"`
	Address *string `json:"address,omitempty" doc:"EIP-55 address when type is address/contract"`
}

// DiffView is the canonical GET representation of a DiffRecord. We
// flatten the Discrepancy + Judgement + meta into one JSON object
// so frontend code does not have to chase two nested containers;
// all read paths ship this shape.
type DiffView struct {
	ID             string              `json:"id"`
	RunID          string              `json:"run_id"`
	MetricKey      string              `json:"metric_key"`
	MetricCategory string              `json:"metric_category"`
	Block          uint64              `json:"block"`
	Subject        SubjectView         `json:"subject"`
	Values         []ValueSnapshotView `json:"values"`
	DetectedAt     time.Time           `json:"detected_at"`

	Severity       string   `json:"severity"`
	TrustedSources []string `json:"trusted_sources"`

	Resolved   bool       `json:"resolved"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`

	Tier         string `json:"tier" enum:"A,B,C,unknown" doc:"Metric tier at save time"`
	AnchorBlock  uint64 `json:"anchor_block" doc:"Run's verification anchor"`
	SamplingSeed *int64 `json:"sampling_seed,omitempty" doc:"Sampling seed for Tier B reproducibility"`
}

// ToDiffView renders a DiffRecord into the wire shape. The values
// map is flattened into a slice sorted by SourceID so frontend
// diffing / snapshot tests are deterministic.
func ToDiffView(id application.DiffID, rec application.DiffRecord) DiffView {
	d := rec.Discrepancy

	vals := make([]ValueSnapshotView, 0, len(d.Values))
	for sid, v := range d.Values {
		view := ValueSnapshotView{
			SourceID:  string(sid),
			Raw:       v.Raw,
			FetchedAt: v.FetchedAt,
		}
		if v.ReflectedBlock != nil {
			rb := v.ReflectedBlock.Uint64()
			view.ReflectedBlock = &rb
		}
		vals = append(vals, view)
	}
	sortSnapshotsBySource(vals)

	subj := SubjectView{Type: string(d.Subject.Type)}
	if d.Subject.Address != nil {
		s := d.Subject.Address.String()
		subj.Address = &s
	}

	trusted := make([]string, len(rec.Judgement.TrustedSources))
	for i, s := range rec.Judgement.TrustedSources {
		trusted[i] = string(s)
	}

	return DiffView{
		ID:             string(id),
		RunID:          string(d.RunID),
		MetricKey:      d.Metric.Key,
		MetricCategory: string(d.Metric.Category),
		Block:          d.Block.Uint64(),
		Subject:        subj,
		Values:         vals,
		DetectedAt:     d.DetectedAt,
		Severity:       string(rec.Judgement.Severity),
		TrustedSources: trusted,
		Resolved:       rec.Resolved,
		ResolvedAt:     rec.ResolvedAt,
		Tier:           tierString(rec.Tier),
		AnchorBlock:    rec.AnchorBlock.Uint64(),
		SamplingSeed:   rec.SamplingSeed,
	}
}

// tierString renders source.Tier into the string exposed on the
// wire. source.TierUnknown (zero value) becomes "unknown" so the
// frontend can distinguish "tier info missing" from "Tier A".
func tierString(t source.Tier) string {
	switch t {
	case source.TierA:
		return "A"
	case source.TierB:
		return "B"
	case source.TierC:
		return "C"
	default:
		return "unknown"
	}
}

// ListDiffsResponse is the paginated GET /diffs body.
type ListDiffsResponse struct {
	Items  []DiffView `json:"items"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// ReplayDiffResponse is the POST /diffs/{id}/replay body.
type ReplayDiffResponse struct {
	OriginalDiffID string  `json:"original_diff_id"`
	Resolved       bool    `json:"resolved"`
	NewDiffID      *string `json:"new_diff_id,omitempty" doc:"Populated only when sources still disagree"`
}

// ToReplayDiffResponse converts the application result.
func ToReplayDiffResponse(r application.ReplayResult) ReplayDiffResponse {
	out := ReplayDiffResponse{
		OriginalDiffID: string(r.OriginalDiffID),
		Resolved:       r.Resolved,
	}
	if r.NewDiffID != nil {
		s := string(*r.NewDiffID)
		out.NewDiffID = &s
	}
	return out
}

// ParseSeverity maps a wire severity filter string to the domain
// value. Empty input returns a nil pointer (no filter); unknown
// values return an error so the route can respond 400.
func ParseSeverity(s string) (*diff.Severity, error) {
	if s == "" {
		return nil, nil
	}
	sev := diff.Severity(s)
	switch sev {
	case diff.SevCritical, diff.SevWarning, diff.SevInfo:
		return &sev, nil
	}
	return nil, errSeverity(s)
}

type severityError string

func (e severityError) Error() string { return "unknown severity: " + string(e) }

func errSeverity(s string) error { return severityError(s) }

// --- helpers ---------------------------------------------------------

func sortSnapshotsBySource(vs []ValueSnapshotView) {
	// simple insertion sort; N is typically 2–5 sources.
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && vs[j].SourceID < vs[j-1].SourceID; j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}
