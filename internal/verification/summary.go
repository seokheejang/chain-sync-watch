package verification

import (
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// SubjectKind enumerates the shapes a comparison subject can take.
// One Run's summary typically mixes several kinds — a schedule that
// checks block hashes and a handful of addresses records blocks and
// address_latest entries side by side. Short string constants keep
// the JSONB payload compact without giving up self-description.
type SubjectKind string

const (
	// SubjectKindBlock — a block-anchored comparison (BlockImmutable
	// metrics). Carries Block.
	SubjectKindBlock SubjectKind = "block"
	// SubjectKindAddressLatest — an address at chain tip
	// (AddressLatest metrics). Carries Address.
	SubjectKindAddressLatest SubjectKind = "address_latest"
	// SubjectKindAddressAtBlock — an address at a historical block
	// (AddressAtBlock metrics). Carries Address + Block.
	SubjectKindAddressAtBlock SubjectKind = "address_at_block"
	// SubjectKindERC20Balance — per-token balance for one holder at
	// tip. Carries Address (holder) + Token (erc20 contract).
	SubjectKindERC20Balance SubjectKind = "erc20_balance"
	// SubjectKindERC20Holdings — the aggregate holdings list for one
	// holder at tip. Carries Address.
	SubjectKindERC20Holdings SubjectKind = "erc20_holdings"
	// SubjectKindSnapshot — chain-level aggregate (Snapshot metric).
	// Carries Name (the metric key, e.g. "total_addresses").
	SubjectKindSnapshot SubjectKind = "snapshot"
)

// Subject identifies one thing a Run compared across sources. The
// same aggregate (e.g. one block) appears once per Run regardless of
// how many metrics were measured on it — the metric dimension lives
// in Run.Metrics() and is crossed with Subjects implicitly when
// reporting comparison counts.
//
// Address and Token are stored as value objects so their validation
// lives in one place (the chain package) and mapper layers cannot
// leak malformed hex into the aggregate. Block is a pointer to let
// SubjectKindAddressLatest / ERC20Holdings omit it without defaulting
// to zero (which is a real block height).
type Subject struct {
	Kind    SubjectKind
	Block   *chain.BlockNumber
	Address *chain.Address
	Token   *chain.Address
	// Name is the snapshot metric key (SubjectKindSnapshot only).
	Name string
}

// Validate enforces kind-specific field presence. Malformed Subjects
// would persist as-is but make the later UI / retention logic
// brittle, so we reject them at the ingestion point (RecordSummary).
func (s Subject) Validate() error {
	switch s.Kind {
	case SubjectKindBlock:
		if s.Block == nil {
			return errors.New("subject: block kind requires Block")
		}
	case SubjectKindAddressLatest, SubjectKindERC20Holdings:
		if s.Address == nil {
			return fmt.Errorf("subject: %s kind requires Address", s.Kind)
		}
	case SubjectKindAddressAtBlock:
		if s.Address == nil || s.Block == nil {
			return errors.New("subject: address_at_block kind requires Address + Block")
		}
	case SubjectKindERC20Balance:
		if s.Address == nil || s.Token == nil {
			return errors.New("subject: erc20_balance kind requires Address + Token")
		}
	case SubjectKindSnapshot:
		if s.Name == "" {
			return errors.New("subject: snapshot kind requires Name")
		}
	case "":
		return errors.New("subject: kind is empty")
	default:
		return fmt.Errorf("subject: unknown kind %q", s.Kind)
	}
	return nil
}

// RunSummary captures "what the run actually saw" so operators can
// audit success without per-comparison persistence. It intentionally
// excludes raw values returned by each source — that's opt-in via
// raw_response.persist and lives elsewhere.
//
// AnchorBlock is the tip resolved at run time for block-anchored
// metrics. Snapshot-only runs that have no tip context leave it nil.
//
// Subjects is the heterogeneous list of compared entities. Kind
// mixing is expected: one Run may record blocks, addresses, and
// erc20 holdings side by side.
//
// SourcesUsed is a time-of-run snapshot of participating source IDs
// — later edits to the sources table must not mutate the recorded
// summary. Held as []string rather than []source.SourceID so this
// domain file doesn't take an import on the source package; the
// application layer converts.
//
// ComparisonsCount is the total number of individual (subject ×
// metric × source-pair) comparisons attempted; not necessarily
// Subjects×Metrics×... because the engine may skip unsupported
// combinations at runtime.
type RunSummary struct {
	AnchorBlock      *chain.BlockNumber
	Subjects         []Subject
	SourcesUsed      []string
	ComparisonsCount int
}

// IsZero reports whether the summary has no meaningful content. The
// domain treats the zero value as "not recorded"; persistence uses
// this to avoid writing empty-but-present payloads.
func (s RunSummary) IsZero() bool {
	return s.AnchorBlock == nil &&
		len(s.Subjects) == 0 &&
		len(s.SourcesUsed) == 0 &&
		s.ComparisonsCount == 0
}

// Validate walks the summary and returns the first structural error.
// Callers inside RecordSummary rely on this to guard against mapper
// bugs and application-layer miscounts (negative counts, malformed
// subjects).
func (s RunSummary) Validate() error {
	if s.ComparisonsCount < 0 {
		return fmt.Errorf("summary: comparisons_count < 0 (%d)", s.ComparisonsCount)
	}
	for i, subj := range s.Subjects {
		if err := subj.Validate(); err != nil {
			return fmt.Errorf("summary.subjects[%d]: %w", i, err)
		}
	}
	for i, sid := range s.SourcesUsed {
		if sid == "" {
			return fmt.Errorf("summary.sources_used[%d]: empty id", i)
		}
	}
	return nil
}
