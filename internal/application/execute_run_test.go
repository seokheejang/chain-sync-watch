package application_test

import (
	"context"
	"errors"
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

func (f *execFixture) seedRun(t *testing.T, id verification.RunID, metrics []verification.Metric, blocks []chain.BlockNumber) {
	t.Helper()
	r, err := verification.NewRun(
		id,
		chain.OptimismMainnet,
		verification.FixedList{Numbers: blocks},
		metrics,
		verification.ManualTrigger{User: "u"},
		f.baseTime,
	)
	require.NoError(t, err)
	require.NoError(t, f.runs.Save(context.Background(), r))
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
	// Manually transition the Run past pending to trigger the guard.
	r, _ := f.runs.FindByID(context.Background(), "r1")
	require.NoError(t, r.Start(f.baseTime))
	require.NoError(t, f.runs.Save(context.Background(), r))

	err := f.useCase.Execute(context.Background(), "r1")
	require.Error(t, err)
}
