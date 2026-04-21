package diff_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// standardTrust mirrors the OSS-default trust ranking:
// RPC > external explorers > custom indexer.
var standardTrust = []source.SourceID{"rpc", "blockscout", "etherscan", "routescan", "indexer"}

func makeDiscrepancy(t *testing.T, metric verification.Metric, values map[source.SourceID]diff.ValueSnapshot) diff.Discrepancy {
	t.Helper()
	d, err := diff.NewDiscrepancy(
		"rid",
		metric,
		chain.BlockNumber(100),
		diff.Subject{Type: diff.SubjectBlock},
		values,
		time.Now(),
	)
	require.NoError(t, err)
	return d
}

func TestDefaultPolicy_AllAgreeReturnsInfo(t *testing.T) {
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc"},
		"blockscout": {Raw: "0xabc"},
		"indexer":    {Raw: "0xabc"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevInfo, j.Severity)
	require.Equal(t, []source.SourceID{"blockscout", "indexer", "rpc"}, j.TrustedSources)
}

func TestDefaultPolicy_BlockImmutableCriticalOutlier(t *testing.T) {
	// Scenario: RPC=A, Blockscout=A, Indexer=B
	// BlockImmutable category → Critical, trusted = cluster with RPC.
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc"},
		"blockscout": {Raw: "0xabc"},
		"indexer":    {Raw: "0xabd"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevCritical, j.Severity)
	require.Equal(t, []source.SourceID{"blockscout", "rpc"}, j.TrustedSources)
}

func TestDefaultPolicy_AddressLatestWarningOutlier(t *testing.T) {
	d := makeDiscrepancy(t, verification.MetricBalanceLatest, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "1000000"},
		"blockscout": {Raw: "1000000"},
		"indexer":    {Raw: "999999"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevWarning, j.Severity)
	require.Equal(t, []source.SourceID{"blockscout", "rpc"}, j.TrustedSources)
}

func TestDefaultPolicy_AllThreeDisagree(t *testing.T) {
	// Scenario: RPC=A, Blockscout=B, Indexer=C (three singleton clusters).
	// BlockImmutable → Critical, trusted = RPC (highest-ranked).
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xA"},
		"blockscout": {Raw: "0xB"},
		"indexer":    {Raw: "0xC"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevCritical, j.Severity)
	require.Equal(t, []source.SourceID{"rpc"}, j.TrustedSources)
}

func TestDefaultPolicy_NoRPCAvailable(t *testing.T) {
	// Scenario: RPC absent; Blockscout=A, Indexer=B.
	// AddressLatest → Warning, trusted = cluster with higher-ranked
	// Source (blockscout).
	d := makeDiscrepancy(t, verification.MetricBalanceLatest, map[source.SourceID]diff.ValueSnapshot{
		"blockscout": {Raw: "1000"},
		"indexer":    {Raw: "1001"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevWarning, j.Severity)
	require.Equal(t, []source.SourceID{"blockscout"}, j.TrustedSources)
}

func TestDefaultPolicy_NoRankedSourceFallsBackToLargestCluster(t *testing.T) {
	// With an empty SourceTrust, fall back to the largest cluster.
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"srcA": {Raw: "0x1"},
		"srcB": {Raw: "0x1"},
		"srcC": {Raw: "0x2"},
	})
	j := diff.DefaultPolicy{}.Judge(d)
	require.Equal(t, diff.SevCritical, j.Severity)
	require.Equal(t, []source.SourceID{"srcA", "srcB"}, j.TrustedSources)
}

func TestDefaultPolicy_NoRankedSourceTiebreakByRawLex(t *testing.T) {
	// Two singleton clusters, no trust ranking → lexicographic
	// tiebreak on the Raw value.
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"srcA": {Raw: "0xAA"},
		"srcB": {Raw: "0xBB"},
	})
	j := diff.DefaultPolicy{}.Judge(d)
	require.Equal(t, []source.SourceID{"srcA"}, j.TrustedSources)
}

func TestDefaultPolicy_SnapshotSeverityIsInfo(t *testing.T) {
	// Snapshot metrics never escalate even when Sources diverge.
	d := makeDiscrepancy(t, verification.MetricTotalAddressCount, map[source.SourceID]diff.ValueSnapshot{
		"blockscout": {Raw: "100000000"},
		"indexer":    {Raw: "79000000"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevInfo, j.Severity)
}

func TestDefaultPolicy_AddressAtBlockCritical(t *testing.T) {
	d := makeDiscrepancy(t, verification.MetricBalanceAtBlock, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "500"},
		"blockscout": {Raw: "600"},
	})
	j := diff.DefaultPolicy{SourceTrust: standardTrust}.Judge(d)
	require.Equal(t, diff.SevCritical, j.Severity)
	require.Equal(t, []source.SourceID{"rpc"}, j.TrustedSources)
}

func TestDefaultPolicy_ReasoningIsDeterministic(t *testing.T) {
	// Same input, same reasoning string — required for idempotent
	// persistence.
	d := makeDiscrepancy(t, verification.MetricBlockHash, map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc"},
		"blockscout": {Raw: "0xabc"},
		"indexer":    {Raw: "0xabd"},
	})
	p := diff.DefaultPolicy{SourceTrust: standardTrust}
	j1 := p.Judge(d)
	j2 := p.Judge(d)
	require.Equal(t, j1.Reasoning, j2.Reasoning)
	require.Contains(t, j1.Reasoning, "trusted=[blockscout,rpc]")
}

func TestJudgementPolicy_InterfaceCompliance(t *testing.T) {
	var _ diff.JudgementPolicy = diff.DefaultPolicy{}
}
