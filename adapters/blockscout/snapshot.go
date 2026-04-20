package blockscout

import (
	"context"
	"strconv"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawStats is the subset of /api/v2/stats we consume.
type rawStats struct {
	TotalBlocks       string `json:"total_blocks"`
	TotalAddresses    string `json:"total_addresses"`
	TotalTransactions string `json:"total_transactions"`
}

// FetchSnapshot reads chain-wide aggregates from /api/v2/stats.
// Blockscout is the only Tier B source that returns chain totals,
// so these numbers are observation-only (no cross-indexer check).
func (a *Adapter) FetchSnapshot(ctx context.Context, _ source.SnapshotQuery) (source.SnapshotResult, error) {
	var raw rawStats
	if err := a.getJSON(ctx, "/stats", &raw); err != nil {
		return source.SnapshotResult{SourceID: ID}, err
	}
	out := source.SnapshotResult{
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}
	if n, ok := parseDecimalU64(raw.TotalAddresses); ok {
		out.TotalAddressCount = &n
	}
	if n, ok := parseDecimalU64(raw.TotalTransactions); ok {
		out.TotalTxCount = &n
	}
	return out, nil
}

func parseDecimalU64(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
