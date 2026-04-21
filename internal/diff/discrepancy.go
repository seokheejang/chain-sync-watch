package diff

import (
	"errors"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// SubjectType classifies what a Discrepancy is "about": one of a
// block, an address, a contract, or the whole chain. The type drives
// how downstream consumers (UI, alerting) route the record — a
// disagreement about a contract's balance goes on a different wall
// than a block-hash mismatch.
type SubjectType string

const (
	// SubjectBlock — keyed by block number only (no Address).
	SubjectBlock SubjectType = "block"
	// SubjectAddress — keyed by an EOA's Address.
	SubjectAddress SubjectType = "address"
	// SubjectContract — keyed by a contract's Address.
	SubjectContract SubjectType = "contract"
	// SubjectChain — chain-wide aggregates (no Address, block is
	// the observation height).
	SubjectChain SubjectType = "chain"
)

// Subject identifies the entity a Discrepancy is keyed by. Address
// is populated for SubjectAddress and SubjectContract; SubjectBlock
// and SubjectChain leave it nil. Keeping it as a pointer rather
// than a zero Address lets consumers distinguish "not applicable"
// from "the zero address".
type Subject struct {
	Type    SubjectType
	Address *chain.Address
}

// ValueSnapshot is one Source's view of a metric at the moment the
// verification layer fetched it. Raw is the canonical string we
// persist — adapters normalise to a stable form (RPC-style hex for
// hashes, decimal for counts, 0x-prefixed hex for balances) so
// Tolerance comparators can trust a string equality check.
//
// Typed carries the original Go value for richer comparison when a
// tolerance needs arithmetic (NumericTolerance parses it directly
// when Raw is a big integer).
//
// ReflectedBlock is populated only by Sources that expose it in
// their response (Blockscout's block_number_balance_updated_at
// being the canonical example). It powers AnchorWindowed tolerance:
// a reflected block outside the Run's anchor window means the
// sample is stale, and should be discarded rather than judged.
type ValueSnapshot struct {
	Raw            string
	Typed          any
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
}

// IsZero reports whether the snapshot has never been populated.
// Useful for tolerance implementations that want to distinguish
// "no value" from "empty value".
func (v ValueSnapshot) IsZero() bool {
	return v.Raw == "" && v.Typed == nil && v.FetchedAt.IsZero() && v.ReflectedBlock == nil
}

// Discrepancy records one disagreement between Sources for a
// single (Run, Metric, Block, Subject) tuple. Values maps each
// participating SourceID to its snapshot; the actual comparison
// lives in Tolerance / JudgementPolicy.
type Discrepancy struct {
	RunID      verification.RunID
	Metric     verification.Metric
	Block      chain.BlockNumber
	Subject    Subject
	Values     map[source.SourceID]ValueSnapshot
	DetectedAt time.Time
}

// NewDiscrepancy constructs a Discrepancy after validating the
// invariants that must always hold:
//
//   - runID non-empty
//   - subject.Type set
//   - values has at least two entries (one Source cannot disagree
//     with itself)
//   - each SourceID key is non-empty
//
// The values map is copied defensively so mutations on the
// caller's side cannot reach the stored record.
func NewDiscrepancy(
	runID verification.RunID,
	metric verification.Metric,
	block chain.BlockNumber,
	subject Subject,
	values map[source.SourceID]ValueSnapshot,
	detectedAt time.Time,
) (Discrepancy, error) {
	if runID == "" {
		return Discrepancy{}, errors.New("discrepancy: run id is empty")
	}
	if subject.Type == "" {
		return Discrepancy{}, errors.New("discrepancy: subject type is empty")
	}
	if len(values) < 2 {
		return Discrepancy{}, fmt.Errorf("discrepancy: need at least 2 source values, got %d", len(values))
	}
	for sid := range values {
		if sid == "" {
			return Discrepancy{}, errors.New("discrepancy: source id is empty")
		}
	}
	vs := make(map[source.SourceID]ValueSnapshot, len(values))
	for k, v := range values {
		vs[k] = v
	}
	return Discrepancy{
		RunID:      runID,
		Metric:     metric,
		Block:      block,
		Subject:    subject,
		Values:     vs,
		DetectedAt: detectedAt,
	}, nil
}
