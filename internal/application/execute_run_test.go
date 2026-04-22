package application_test

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/source/fake"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// --- fixture --------------------------------------------------------

type execFixture struct {
	runs      *testsupport.FakeRunRepo
	diffs     *testsupport.FakeDiffRepo
	gateway   *testsupport.FakeSourceGateway
	chainHead *testsupport.FakeChainHead
	clock     *testsupport.FakeClock
	useCase   application.ExecuteRun
	baseTime  time.Time
}

func newExecFixture() *execFixture {
	base := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	runs := testsupport.NewFakeRunRepo()
	diffs := testsupport.NewFakeDiffRepo()
	gateway := testsupport.NewFakeSourceGateway()
	head := testsupport.NewFakeChainHead()
	clock := testsupport.NewFakeClock(base)

	head.SetTip(chain.OptimismMainnet, 1000)
	head.SetFinalized(chain.OptimismMainnet, 990)

	return &execFixture{
		runs:      runs,
		diffs:     diffs,
		gateway:   gateway,
		chainHead: head,
		clock:     clock,
		baseTime:  base,
		useCase: application.ExecuteRun{
			Runs:      runs,
			Diffs:     diffs,
			Sources:   gateway,
			ChainHead: head,
			Clock:     clock,
			Policy:    diff.DefaultPolicy{SourceTrust: []source.SourceID{"rpc", "blockscout", "indexer"}},
		},
	}
}

func (f *execFixture) seedRun(t *testing.T, id verification.RunID, metrics []verification.Metric, blocks []chain.BlockNumber, plans ...verification.AddressSamplingPlan) {
	t.Helper()
	r, err := verification.NewRun(
		id,
		chain.OptimismMainnet,
		verification.FixedList{Numbers: blocks},
		metrics,
		verification.ManualTrigger{User: "u"},
		f.baseTime,
		plans...,
	)
	require.NoError(t, err)
	require.NoError(t, f.runs.Save(context.Background(), r))
}

func mkAddressLatest(t *testing.T, bal uint64, nonce uint64, reflected uint64) source.AddressLatestResult {
	t.Helper()
	b := new(big.Int).SetUint64(bal)
	n := nonce
	rb := chain.BlockNumber(reflected)
	return source.AddressLatestResult{
		Balance:        b,
		Nonce:          &n,
		FetchedAt:      time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
		ReflectedBlock: &rb,
	}
}

func mkAddressAtBlock(t *testing.T, bal uint64, nonce uint64, block uint64) source.AddressAtBlockResult {
	t.Helper()
	b := new(big.Int).SetUint64(bal)
	n := nonce
	bn := chain.BlockNumber(block)
	return source.AddressAtBlockResult{
		Balance:        b,
		Nonce:          &n,
		Block:          bn,
		FetchedAt:      time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
		ReflectedBlock: &bn,
	}
}

func mkBlockResult(t *testing.T, hashHex, parentHex string, ts int64, txCount uint64) source.BlockResult {
	t.Helper()
	h, err := chain.NewHash32(hashHex)
	require.NoError(t, err)
	p, err := chain.NewHash32(parentHex)
	require.NoError(t, err)
	timestamp := time.Unix(ts, 0).UTC()
	return source.BlockResult{
		Hash:       &h,
		ParentHash: &p,
		Timestamp:  &timestamp,
		TxCount:    &txCount,
		FetchedAt:  time.Unix(ts, 0).UTC(),
	}
}

func hex32(pattern string) string {
	// Repeat `pattern` (hex chars) until 64 characters, then prefix 0x.
	n := 64
	b := strings.Repeat(pattern, (n+len(pattern)-1)/len(pattern))[:n]
	return "0x" + b
}

// --- tests ----------------------------------------------------------

func TestExecuteRun_AllSourcesAgree(t *testing.T) {
	f := newExecFixture()

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash, source.CapBlockTimestamp))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash, source.CapBlockTimestamp))
	idx := fake.New("indexer", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash, source.CapBlockTimestamp))
	br := mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42)
	rpc.SetBlockResponse(br, nil)
	bs.SetBlockResponse(br, nil)
	idx.SetBlockResponse(br, nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)
	f.gateway.Register(idx)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		[]chain.BlockNumber{100, 101, 102},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))

	r, err := f.runs.FindByID(context.Background(), "r1")
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.Zero(t, f.diffs.Count())
}

func TestExecuteRun_OneSourceDisagreesProducesDiff(t *testing.T) {
	f := newExecFixture()

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	idx := fake.New("indexer", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))

	agree := mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42)
	divergent := mkBlockResult(t, hex32("cd"), hex32("00"), 1700000000, 42)

	rpc.SetBlockResponse(agree, nil)
	bs.SetBlockResponse(agree, nil)
	idx.SetBlockResponse(divergent, nil)

	f.gateway.Register(rpc)
	f.gateway.Register(bs)
	f.gateway.Register(idx)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{100},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))

	r, err := f.runs.FindByID(context.Background(), "r1")
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.Equal(t, 1, f.diffs.Count())

	recs, err := f.diffs.FindByRun(context.Background(), "r1")
	require.NoError(t, err)
	require.Len(t, recs, 1)
	rec := recs[0]
	require.Equal(t, chain.BlockNumber(100), rec.Discrepancy.Block)
	require.Equal(t, verification.MetricBlockHash, rec.Discrepancy.Metric)
	require.Equal(t, diff.SevCritical, rec.Judgement.Severity)
	require.Equal(t, []source.SourceID{"blockscout", "rpc"}, rec.Judgement.TrustedSources)
	// Phase 7C.1: meta is populated from the metric Capability +
	// the Finalized anchor the fixture seeds (990).
	require.Equal(t, source.TierA, rec.Tier)
	require.Equal(t, chain.BlockNumber(990), rec.AnchorBlock)
}

func TestExecuteRun_SourceErrorsAreNonFatal(t *testing.T) {
	f := newExecFixture()

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	br := mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42)
	rpc.SetBlockResponse(br, nil)
	bs.SetBlockResponse(source.BlockResult{}, errors.New("upstream flaky"))

	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{100},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))

	r, err := f.runs.FindByID(context.Background(), "r1")
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, r.Status())
	// One Source returned data, the other errored — no comparison
	// possible with <2 snapshots, so zero diffs.
	require.Zero(t, f.diffs.Count())
}

func TestExecuteRun_SingleSourceIsFatal(t *testing.T) {
	f := newExecFixture()
	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	rpc.SetBlockResponse(mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42), nil)
	f.gateway.Register(rpc)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{100},
	)

	err := f.useCase.Execute(context.Background(), "r1")
	require.Error(t, err)

	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusFailed, r.Status())
	require.Contains(t, r.ErrorMessage(), "at least 2 sources")
}

func TestExecuteRun_FinalizedUnreachableIsFatal(t *testing.T) {
	f := newExecFixture()
	// Clear finalized to simulate unreachable upstream.
	f.chainHead = testsupport.NewFakeChainHead()
	f.chainHead.SetTip(chain.OptimismMainnet, 1000)
	// Finalized intentionally not set.
	f.useCase.ChainHead = f.chainHead

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{100},
	)

	err := f.useCase.Execute(context.Background(), "r1")
	require.Error(t, err)
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusFailed, r.Status())
}

func TestExecuteRun_EmptyBlockListCompletesWithNoDiffs(t *testing.T) {
	f := newExecFixture()
	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	// FixedList with no entries.
	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		nil,
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.Zero(t, f.diffs.Count())
}

func TestExecuteRun_OnlySnapshotMetricsCompletesWithoutComparison(t *testing.T) {
	// Phase 5B scope: BlockImmutable only. Snapshot metrics are
	// skipped silently.
	f := newExecFixture()
	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapTotalTxCount))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapTotalTxCount))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricTotalTxCount},
		[]chain.BlockNumber{100},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.Zero(t, f.diffs.Count())
}

func TestExecuteRun_RunNotFoundPropagates(t *testing.T) {
	f := newExecFixture()
	err := f.useCase.Execute(context.Background(), "nope")
	require.ErrorIs(t, err, application.ErrRunNotFound)
}

func TestExecuteRun_UnsupportedCapabilityIsSkipped(t *testing.T) {
	f := newExecFixture()

	rpc := fake.New("rpc", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBlockTimestamp),
	)
	// blockscout supports only Hash, not Timestamp.
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))

	rpcBR := mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42)
	bsBR := source.BlockResult{
		Hash:      rpcBR.Hash,
		FetchedAt: rpcBR.FetchedAt,
	}
	rpc.SetBlockResponse(rpcBR, nil)
	bs.SetBlockResponse(bsBR, nil)

	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		[]chain.BlockNumber{100},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusCompleted, r.Status())
	// Hash: both sources agree → 0 diffs
	// Timestamp: only one source (rpc) has it → skipped (need >=2)
	require.Zero(t, f.diffs.Count())
}

func TestExecuteRun_MultipleBlocksMultipleMetrics(t *testing.T) {
	f := newExecFixture()

	rpc := fake.New("rpc", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBlockTxCount),
	)
	bs := fake.New("blockscout", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBlockTxCount),
	)

	// Per-block handlers: block 100 agrees, block 101 diverges on hash only.
	rpc.SetBlockHandler(func(_ context.Context, q source.BlockQuery) (source.BlockResult, error) {
		switch q.Number {
		case 100:
			return mkBlockResult(t, hex32("a"), hex32("0"), 1700000100, 10), nil
		case 101:
			return mkBlockResult(t, hex32("1"), hex32("0"), 1700000200, 20), nil
		}
		return source.BlockResult{}, source.ErrNotFound
	})
	bs.SetBlockHandler(func(_ context.Context, q source.BlockQuery) (source.BlockResult, error) {
		switch q.Number {
		case 100:
			return mkBlockResult(t, hex32("a"), hex32("0"), 1700000100, 10), nil
		case 101:
			// Hash disagrees — tx count agrees.
			return mkBlockResult(t, hex32("2"), hex32("0"), 1700000200, 20), nil
		}
		return source.BlockResult{}, source.ErrNotFound
	})

	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTxCount},
		[]chain.BlockNumber{100, 101},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Equal(t, 1, f.diffs.Count(), "only one disagreement expected: block 101, hash")

	recs, _ := f.diffs.FindByRun(context.Background(), "r1")
	require.Len(t, recs, 1)
	require.Equal(t, chain.BlockNumber(101), recs[0].Discrepancy.Block)
	require.Equal(t, "block.hash", recs[0].Discrepancy.Metric.Key)
}

func TestExecuteRun_ToleranceOverrideSuppressesDiff(t *testing.T) {
	// Same setup as "one source disagrees", but a ToleranceResolver
	// override pins MetricBlockHash to Observational, which never
	// reports disagreement. The disagreement therefore produces no
	// DiffRecord.
	f := newExecFixture()
	f.useCase.Tolerance = application.DefaultToleranceResolver{
		Overrides: map[string]diff.Tolerance{
			verification.MetricBlockHash.Key: diff.Observational{},
		},
	}

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	rpc.SetBlockResponse(mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 42), nil)
	bs.SetBlockResponse(mkBlockResult(t, hex32("cd"), hex32("00"), 1700000000, 42), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{100},
	)
	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count(), "observational tolerance should suppress the diff")
}

func TestExecuteRun_CannotStartFromNonPendingRun(t *testing.T) {
	f := newExecFixture()
	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBlockHash},
		[]chain.BlockNumber{},
	)
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.NoError(t, r.Start(f.baseTime))
	require.NoError(t, f.runs.Save(context.Background(), r))

	err := f.useCase.Execute(context.Background(), "r1")
	require.Error(t, err)
}

func TestExecuteRun_AddressLatest_DisagreementProducesDiff(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{addr}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	rpc.SetAddressLatestResponse(mkAddressLatest(t, 1000, 5, 990), nil)
	bs.SetAddressLatestResponse(mkAddressLatest(t, 2000, 5, 990), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Equal(t, 1, f.diffs.Count())

	records, err := f.diffs.FindByRun(context.Background(), "r1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	d := records[0].Discrepancy
	require.Equal(t, verification.MetricBalanceLatest.Key, d.Metric.Key)
	require.Equal(t, diff.SubjectAddress, d.Subject.Type)
	require.NotNil(t, d.Subject.Address)
	require.Equal(t, addr, *d.Subject.Address)
	require.Equal(t, "1000", d.Values["rpc"].Raw)
	require.Equal(t, "2000", d.Values["blockscout"].Raw)

	require.Len(t, sampler.Calls, 1)
	require.Equal(t, verification.KindKnownAddresses, sampler.Calls[0].Kind)
	require.Equal(t, chain.BlockNumber(990), sampler.Calls[0].At)
}

func TestExecuteRun_AddressLatest_NoPlansSkipsPass(t *testing.T) {
	f := newExecFixture()
	sampler := testsupport.NewFakeAddressSampler()
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count())
	require.Empty(t, sampler.Calls, "sampler must not be called when Run has no plans")
}

func TestExecuteRun_AddressLatest_NilSamplerSkipsPass(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	rpc.SetAddressLatestResponse(mkAddressLatest(t, 1000, 5, 990), nil)
	bs.SetAddressLatestResponse(mkAddressLatest(t, 2000, 5, 990), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count(), "address loop must no-op when Addresses port is nil")
}

func TestExecuteRun_AddressLatest_DedupesAcrossPlans(t *testing.T) {
	f := newExecFixture()
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{a, b}
	sampler.Results[verification.KindTopNHolders] = []chain.Address{b}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	rpc.SetAddressLatestResponse(mkAddressLatest(t, 1000, 5, 990), nil)
	bs.SetAddressLatestResponse(mkAddressLatest(t, 1000, 5, 990), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{a, b}},
		verification.TopNHolders{N: 10},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count(), "agreeing values must not produce diffs")

	require.Len(t, sampler.Calls, 2, "both plans should be invoked once each")
}

func TestExecuteRun_AddressLatest_BudgetExhaustedSkipsSource(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{addr}
	f.useCase.Addresses = sampler

	budget := testsupport.NewFakeRateLimitBudget(map[source.SourceID]int{
		"rpc":        10,
		"blockscout": 10,
		"indexer":    0,
	})
	f.useCase.Budget = budget

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	idx := fake.New("indexer", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	rpc.SetAddressLatestResponse(mkAddressLatest(t, 1000, 5, 990), nil)
	bs.SetAddressLatestResponse(mkAddressLatest(t, 2000, 5, 990), nil)
	idx.SetAddressLatestResponse(mkAddressLatest(t, 9999, 5, 990), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)
	f.gateway.Register(idx)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Equal(t, 1, f.diffs.Count())

	records, err := f.diffs.FindByRun(context.Background(), "r1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	d := records[0].Discrepancy
	_, hasIndexer := d.Values["indexer"]
	require.False(t, hasIndexer, "indexer should be skipped due to budget exhaustion")

	require.Equal(t, 9, budget.Remaining("rpc"))
	require.Equal(t, 9, budget.Remaining("blockscout"))
	require.Equal(t, 0, budget.Remaining("indexer"))
}

func TestExecuteRun_AddressLatest_SamplerErrorFailsRun(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Err = errors.New("sampler exploded")
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceLatest},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	err := f.useCase.Execute(context.Background(), "r1")
	require.Error(t, err)

	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.Equal(t, verification.StatusFailed, r.Status())
}

func TestExecuteRun_AddressAtBlock_DisagreementProducesDiff(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{addr}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	rpc.SetAddressAtBlockResponse(mkAddressAtBlock(t, 1000, 5, 500), nil)
	bs.SetAddressAtBlockResponse(mkAddressAtBlock(t, 2000, 5, 500), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceAtBlock},
		[]chain.BlockNumber{500},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Equal(t, 1, f.diffs.Count())

	records, err := f.diffs.FindByRun(context.Background(), "r1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	d := records[0].Discrepancy
	require.Equal(t, verification.MetricBalanceAtBlock.Key, d.Metric.Key)
	require.Equal(t, diff.SubjectAddress, d.Subject.Type)
	require.NotNil(t, d.Subject.Address)
	require.Equal(t, addr, *d.Subject.Address)
	require.Equal(t, chain.BlockNumber(500), d.Block, "Discrepancy.Block should be the queried historical block")
	require.Equal(t, chain.BlockNumber(990), records[0].AnchorBlock, "AnchorBlock meta stays the Run's finalized anchor")
	require.Equal(t, "1000", d.Values["rpc"].Raw)
	require.Equal(t, "2000", d.Values["blockscout"].Raw)
}

func TestExecuteRun_AddressAtBlock_NoBlocksSkipsPass(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{addr}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	rpc.SetAddressAtBlockResponse(mkAddressAtBlock(t, 1000, 5, 500), nil)
	bs.SetAddressAtBlockResponse(mkAddressAtBlock(t, 2000, 5, 500), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceAtBlock},
		[]chain.BlockNumber{},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count(), "no blocks means no AddressAtBlock comparisons")
}

func TestExecuteRun_AddressAtBlock_UnsupportedSourceIsSkipped(t *testing.T) {
	f := newExecFixture()
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{addr}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	rpc.SetAddressAtBlockResponse(mkAddressAtBlock(t, 1000, 5, 500), nil)
	// blockscout lacks the archive capability.
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtLatest))
	bs.SetAddressAtBlockResponse(source.AddressAtBlockResult{}, source.ErrUnsupported)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceAtBlock},
		[]chain.BlockNumber{500},
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Zero(t, f.diffs.Count(), "only one source supports archive — nothing to compare")
}

func TestExecuteRun_AddressAtBlock_CartesianOverBlocksAndAddresses(t *testing.T) {
	f := newExecFixture()
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")

	sampler := testsupport.NewFakeAddressSampler()
	sampler.Results[verification.KindKnownAddresses] = []chain.Address{a, b}
	f.useCase.Addresses = sampler

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBalanceAtBlock))

	// Disagree only at (addr=b, block=501); all other 3 cells agree.
	handler := func(agreeBal, divergeBal uint64) func(context.Context, source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
		return func(_ context.Context, q source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
			if q.Address == b && q.Block == 501 {
				return mkAddressAtBlock(t, divergeBal, 5, uint64(q.Block)), nil
			}
			return mkAddressAtBlock(t, agreeBal, 5, uint64(q.Block)), nil
		}
	}
	rpc.SetAddressAtBlockHandler(handler(100, 100))
	bs.SetAddressAtBlockHandler(handler(100, 999))

	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	f.seedRun(t, "r1",
		[]verification.Metric{verification.MetricBalanceAtBlock},
		[]chain.BlockNumber{500, 501},
		verification.KnownAddresses{Addresses: []chain.Address{a, b}},
	)

	require.NoError(t, f.useCase.Execute(context.Background(), "r1"))
	require.Equal(t, 1, f.diffs.Count(), "exactly one cell (b, 501) disagrees")

	records, _ := f.diffs.FindByRun(context.Background(), "r1")
	require.Len(t, records, 1)
	d := records[0].Discrepancy
	require.Equal(t, chain.BlockNumber(501), d.Block)
	require.NotNil(t, d.Subject.Address)
	require.Equal(t, b, *d.Subject.Address)
}
