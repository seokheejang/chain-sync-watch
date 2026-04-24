package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ExecuteRun is the verification engine. It loads a Run, fixes an
// anchor, fans out block fetches across Sources, compares per-
// metric values under the Tolerance a resolver hands back, and
// persists DiffRecords for every disagreement.
//
// Passes executed in order:
//
//   - BlockImmutable metrics: one FetchBlock per (block, source),
//     compared across every Source that supports the Capability.
//   - AddressLatest metrics: when the Run carries at least one
//     AddressSamplingPlan AND an AddressSampler is injected,
//     ExecuteRun resolves every plan, deduplicates the merged
//     address set, and fans out FetchAddressLatest. ValueSnapshot
//     .ReflectedBlock is populated so AnchorWindowed tolerance can
//     discard stale samples.
//   - AddressAtBlock metrics: archive-backed historical state. Same
//     AddressSamplingPlan set as AddressLatest, but the fan-out is
//     (address × block) cartesian over the Run's block sampling
//     strategy. Sources that don't support archive reads return
//     ErrUnsupported and are skipped.
//   - ERC-20 holdings: per-address FetchERC20Holdings with a
//     canonical sorted "contract=balance" serialization, driven by
//     the same AddressSamplingPlan set. Tier B — Budget
//     reserve/refund is applied.
//   - ERC-20 balance: (address × token) cartesian over
//     AddressSamplingPlan × TokenSamplingPlan sets. Tokens come
//     from the TokenSampler port. Tier C — Budget applied per fetch.
//
// Cross-cutting:
//
//   - Tolerance is resolved per Metric via the ToleranceResolver
//     port (nil falls back to ExactMatch). Pair-wise outcomes
//     collapse to whole-sample decisions: any pair signalling
//     needDiscard drops the sample; otherwise disagreement happens
//     when any pair says ok=false.
//   - DiffRepository.Save receives SaveDiffMeta (Tier derived from
//     Capability, AnchorBlock from ChainHead.Finalized).
//   - Per-source fetches run in parallel; the outer loops (blocks,
//     addresses, tokens) stay sequential so adapter rate limits are
//     naturally backpressured.
//   - Budget integration: when Budget is non-nil, every
//     per-source fetch reserves one unit keyed by Source ID before
//     the call and refunds on error. Block fetches skip Budget (low
//     call volume).
//
// Terminal states:
//
//   - run.Complete — reached the end of the block and address
//     loops without a fatal error.
//   - run.Fail     — anchor resolution, tip resolution, address
//     sampling, or source enumeration failed; OR fewer than two
//     Sources were configured for the chain.
//
// Per-(Source, Block|Address, Metric) fetch failures are non-fatal
// — ExecuteRun skips them and continues. The run still finishes
// "completed" because partial coverage is useful; failures surface
// via logs rather than failing the whole pass.
type ExecuteRun struct {
	Runs      RunRepository
	Diffs     DiffRepository
	Sources   SourceGateway
	ChainHead ChainHead
	Clock     Clock
	Policy    diff.JudgementPolicy
	// Tolerance is optional; nil means DefaultToleranceResolver{}.
	Tolerance ToleranceResolver
	// Addresses is optional; nil disables the AddressLatest loop
	// even when the Run carries AddressSamplingPlans.
	Addresses AddressSampler
	// Tokens is optional; nil disables the ERC20 Balance (per-token)
	// pass even when the Run carries TokenSamplingPlans.
	Tokens TokenSampler
	// Budget is optional; nil disables per-fetch budget accounting.
	Budget RateLimitBudget
}

// Execute runs the full verification pipeline for runID.
func (uc ExecuteRun) Execute(ctx context.Context, runID verification.RunID) error {
	run, err := uc.Runs.FindByID(ctx, runID)
	if err != nil {
		return err
	}

	if err := run.Start(uc.Clock.Now()); err != nil {
		return fmt.Errorf("execute run: start: %w", err)
	}
	if err := uc.Runs.Save(ctx, run); err != nil {
		return fmt.Errorf("execute run: save started: %w", err)
	}

	if innerErr := uc.executeInner(ctx, run); innerErr != nil {
		// Fail transitions are best-effort; the inner error is the
		// one the caller wants.
		_ = run.Fail(uc.Clock.Now(), innerErr)
		_ = uc.Runs.Save(ctx, run)
		return innerErr
	}

	if err := run.Complete(uc.Clock.Now()); err != nil {
		return fmt.Errorf("execute run: complete: %w", err)
	}
	if err := uc.Runs.Save(ctx, run); err != nil {
		return fmt.Errorf("execute run: save completed: %w", err)
	}
	return nil
}

// runContext bundles the per-execution state every pass reads. We
// snapshot the Run's metric/plan slices once in executeInner because
// run.Metrics()/AddressPlans()/TokenPlans() each allocate a defensive
// copy — calling them inside the pass loops would recopy on every
// block/address.
type runContext struct {
	sources      []source.Source
	metrics      []verification.Metric
	addressPlans []verification.AddressSamplingPlan
	tokenPlans   []verification.TokenSamplingPlan
	resolver     ToleranceResolver
	blocks       []chain.BlockNumber
	anchor       chain.BlockNumber
	// summary accumulates "what this run looked at" across every
	// pass. Populated inline by the pass functions so Complete has
	// a fully-built RunSummary to seal onto the aggregate.
	summary *summaryAccumulator
}

// summaryAccumulator collects Subjects and comparison counts as the
// run progresses. Not thread-safe — passes run sequentially per Run.
// Kept next to runContext because they share a lifetime and neither
// escapes Execute.
type summaryAccumulator struct {
	anchor      *chain.BlockNumber
	sources     []string
	subjects    []verification.Subject
	comparisons int
}

func (a *summaryAccumulator) setSourcesFrom(ss []source.Source) {
	ids := make([]string, len(ss))
	for i, s := range ss {
		ids[i] = string(s.ID())
	}
	a.sources = ids
}

func (a *summaryAccumulator) setAnchor(b chain.BlockNumber) {
	ab := b
	a.anchor = &ab
}

func (a *summaryAccumulator) addBlock(b chain.BlockNumber) {
	bb := b
	a.subjects = append(a.subjects, verification.Subject{
		Kind:  verification.SubjectKindBlock,
		Block: &bb,
	})
}

func (a *summaryAccumulator) addAddressLatest(addr chain.Address) {
	aa := addr
	a.subjects = append(a.subjects, verification.Subject{
		Kind:    verification.SubjectKindAddressLatest,
		Address: &aa,
	})
}

func (a *summaryAccumulator) addAddressAtBlock(addr chain.Address, b chain.BlockNumber) {
	aa := addr
	bb := b
	a.subjects = append(a.subjects, verification.Subject{
		Kind:    verification.SubjectKindAddressAtBlock,
		Address: &aa,
		Block:   &bb,
	})
}

func (a *summaryAccumulator) addERC20Holdings(addr chain.Address) {
	aa := addr
	a.subjects = append(a.subjects, verification.Subject{
		Kind:    verification.SubjectKindERC20Holdings,
		Address: &aa,
	})
}

func (a *summaryAccumulator) addERC20Balance(addr, token chain.Address) {
	aa := addr
	tt := token
	a.subjects = append(a.subjects, verification.Subject{
		Kind:    verification.SubjectKindERC20Balance,
		Address: &aa,
		Token:   &tt,
	})
}

func (a *summaryAccumulator) addComparisons(n int) { a.comparisons += n }

func (a *summaryAccumulator) build() verification.RunSummary {
	return verification.RunSummary{
		AnchorBlock:      a.anchor,
		Subjects:         a.subjects,
		SourcesUsed:      a.sources,
		ComparisonsCount: a.comparisons,
	}
}

// executeInner is the body of a successfully-started Run. Any
// error it returns causes Execute to transition the run to failed.
func (uc ExecuteRun) executeInner(ctx context.Context, run *verification.Run) error {
	sources, err := uc.Sources.ForChain(run.ChainID())
	if err != nil {
		return fmt.Errorf("execute run: source gateway: %w", err)
	}
	if len(sources) < 2 {
		return errors.New("execute run: need at least 2 sources for comparison")
	}

	anchor, err := uc.ChainHead.Finalized(ctx, run.ChainID())
	if err != nil {
		return fmt.Errorf("execute run: finalized: %w", err)
	}
	tip, err := uc.ChainHead.Tip(ctx, run.ChainID())
	if err != nil {
		return fmt.Errorf("execute run: tip: %w", err)
	}

	acc := &summaryAccumulator{}
	acc.setSourcesFrom(sources)
	acc.setAnchor(anchor)

	rc := runContext{
		sources:      sources,
		metrics:      run.Metrics(),
		addressPlans: run.AddressPlans(),
		tokenPlans:   run.TokenPlans(),
		resolver:     uc.resolver(),
		anchor:       anchor,
		blocks: run.Strategy().Blocks(verification.SamplingContext{
			TipBlock: tip,
			Now:      uc.Clock.Now(),
		}),
		summary: acc,
	}

	if err := uc.runBlockPass(ctx, run, rc); err != nil {
		return err
	}
	if err := uc.runAddressLatestPass(ctx, run, rc); err != nil {
		return err
	}
	if err := uc.runAddressAtBlockPass(ctx, run, rc); err != nil {
		return err
	}
	if err := uc.runERC20HoldingsLatestPass(ctx, run, rc); err != nil {
		return err
	}
	if err := uc.runERC20BalanceLatestPass(ctx, run, rc); err != nil {
		return err
	}

	// Seal the summary onto the aggregate while still in Running —
	// RecordSummary rejects terminal states. Errors here are soft:
	// the aggregate falls back to a zero summary, which the UI
	// renders as "no detail recorded" rather than failing the whole
	// run for a bookkeeping mistake.
	_ = run.RecordSummary(acc.build())
	return nil
}

// runBlockPass iterates the Run's block sample and dispatches the
// BlockImmutable comparison per block. Block fetches skip Budget
// (low call volume; follow-up if it becomes a problem).
func (uc ExecuteRun) runBlockPass(ctx context.Context, run *verification.Run, rc runContext) error {
	blockMetrics := filterByCategory(rc.metrics, verification.CatBlockImmutable)
	if len(blockMetrics) == 0 {
		return nil
	}
	for _, block := range rc.blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		results := fanOutFetch(ctx, rc.sources, nil, func(ctx context.Context, s source.Source) (source.BlockResult, error) {
			return s.FetchBlock(ctx, source.BlockQuery{Number: block})
		})
		err := compareAndSave(ctx, uc, run, rc.resolver, blockMetrics, results,
			diff.Subject{Type: diff.SubjectBlock},
			block, rc.anchor, "block", projectBlock)
		if err != nil {
			return err
		}
		if rc.summary != nil {
			rc.summary.addBlock(block)
			rc.summary.addComparisons(len(blockMetrics))
		}
	}
	return nil
}

// runAddressLatestPass drives the AddressLatest stratum loop. It is
// a no-op when any of these preconditions is unmet:
//
//   - The Run has no AddressSamplingPlans.
//   - The Run has no AddressLatest metrics.
//   - Addresses (the sampler port) is nil.
//
// Otherwise it resolves every plan into a concrete address set,
// deduplicates across plans, and fans out FetchAddressLatest per
// address.
func (uc ExecuteRun) runAddressLatestPass(ctx context.Context, run *verification.Run, rc runContext) error {
	if len(rc.addressPlans) == 0 || uc.Addresses == nil {
		return nil
	}
	addressMetrics := filterByCategory(rc.metrics, verification.CatAddressLatest)
	if len(addressMetrics) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), rc.addressPlans, rc.anchor)
	if err != nil {
		return fmt.Errorf("execute run: address sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		a := addr
		results := fanOutFetch(ctx, rc.sources, uc.Budget, func(ctx context.Context, s source.Source) (source.AddressLatestResult, error) {
			return s.FetchAddressLatest(ctx, source.AddressQuery{Address: a})
		})
		subj := diff.Subject{Type: diff.SubjectAddress, Address: &a}
		if err := compareAndSave(ctx, uc, run, rc.resolver, addressMetrics, results,
			subj, rc.anchor, rc.anchor, "address", projectAddressLatest); err != nil {
			return err
		}
		if rc.summary != nil {
			rc.summary.addAddressLatest(a)
			rc.summary.addComparisons(len(addressMetrics))
		}
	}
	return nil
}

// runAddressAtBlockPass drives the AddressAtBlock stratum loop. The
// preconditions mirror runAddressLatestPass, plus the Run's
// SamplingStrategy must yield at least one block — AddressAtBlock is
// anchored to specific historical heights, so an empty block list
// means "nothing to verify at depth" and we skip silently.
//
// Iteration order: addresses outer, blocks inner. That keeps a
// single address's archive reads contiguous on each source, which
// matters for adapters that cache archive state per-address.
// Sources that don't support archive reads surface ErrUnsupported
// from FetchAddressAtBlock; the comparator skips those fetches like
// any other per-call error.
func (uc ExecuteRun) runAddressAtBlockPass(ctx context.Context, run *verification.Run, rc runContext) error {
	if len(rc.addressPlans) == 0 || uc.Addresses == nil {
		return nil
	}
	addressMetrics := filterByCategory(rc.metrics, verification.CatAddressAtBlock)
	if len(addressMetrics) == 0 || len(rc.blocks) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), rc.addressPlans, rc.anchor)
	if err != nil {
		return fmt.Errorf("execute run: address-at-block sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		a := addr
		subj := diff.Subject{Type: diff.SubjectAddress, Address: &a}
		for _, block := range rc.blocks {
			if err := ctx.Err(); err != nil {
				return err
			}
			b := block
			results := fanOutFetch(ctx, rc.sources, uc.Budget, func(ctx context.Context, s source.Source) (source.AddressAtBlockResult, error) {
				return s.FetchAddressAtBlock(ctx, source.AddressAtBlockQuery{Address: a, Block: b})
			})
			if err := compareAndSave(ctx, uc, run, rc.resolver, addressMetrics, results,
				subj, b, rc.anchor, "address-at-block", projectAddressAtBlock); err != nil {
				return err
			}
			if rc.summary != nil {
				rc.summary.addAddressAtBlock(a, b)
				rc.summary.addComparisons(len(addressMetrics))
			}
		}
	}
	return nil
}

// runERC20HoldingsLatestPass drives the ERC-20 holdings pass. Same
// preconditions as runAddressLatestPass, but filtered by Capability
// (CapERC20HoldingsAtLatest) rather than Category — MetricERC20*
// share the CatAddressLatest category with plain balance/nonce
// metrics, so a category filter would fan out to the wrong fetch
// method.
//
// Comparison is exact-string on the canonical serialization emitted
// by extractERC20HoldingsField. Sources that filter spam tokens
// will naturally disagree with sources that do not; operators who
// do not want to see those diffs wire an Observational tolerance
// override for MetricERC20HoldingsLatest.
func (uc ExecuteRun) runERC20HoldingsLatestPass(ctx context.Context, run *verification.Run, rc runContext) error {
	if len(rc.addressPlans) == 0 || uc.Addresses == nil {
		return nil
	}
	metrics := filterByCapability(rc.metrics, source.CapERC20HoldingsAtLatest)
	if len(metrics) == 0 {
		return nil
	}
	addrs, err := uc.collectAddresses(ctx, run.ChainID(), rc.addressPlans, rc.anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-holdings sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		a := addr
		results := fanOutFetch(ctx, rc.sources, uc.Budget, func(ctx context.Context, s source.Source) (source.ERC20HoldingsResult, error) {
			return s.FetchERC20Holdings(ctx, source.ERC20HoldingsQuery{Address: a})
		})
		subj := diff.Subject{Type: diff.SubjectAddress, Address: &a}
		if err := compareAndSave(ctx, uc, run, rc.resolver, metrics, results,
			subj, rc.anchor, rc.anchor, "erc20-holdings", projectERC20Holdings); err != nil {
			return err
		}
		if rc.summary != nil {
			rc.summary.addERC20Holdings(a)
			rc.summary.addComparisons(len(metrics))
		}
	}
	return nil
}

// runERC20BalanceLatestPass drives the ERC20 balance (per-token)
// pass. Unlike runERC20HoldingsLatestPass, this path needs both the
// AddressSampler (to enumerate which accounts to query) AND the
// TokenSampler (to enumerate which token contracts to query); fan
// out is the (address × token) cartesian. Disagreements come in
// two flavors: a source may refuse to serve the token (returns
// ErrUnsupported or nil Balance — extractor returns ok=false and
// the slot is dropped), or two sources may report different
// balances for the same pair (the comparison signal).
//
// The pass is a no-op when any precondition fails: no AddressPlans,
// no TokenPlans, nil Addresses sampler, nil Tokens sampler, or no
// CapERC20BalanceAtLatest metric in the Run.
//
// The persisted Subject stays SubjectAddress keyed by `addr`; the
// token contract is NOT encoded in the Discrepancy key, so callers
// that want per-(address, token) DiffRecord lookup must read the
// Metric.Key and the ValueSnapshot.Raw together. A future schema
// extension can add a SubjectToken field if operators ask for it.
func (uc ExecuteRun) runERC20BalanceLatestPass(ctx context.Context, run *verification.Run, rc runContext) error {
	if len(rc.addressPlans) == 0 || len(rc.tokenPlans) == 0 {
		return nil
	}
	if uc.Addresses == nil || uc.Tokens == nil {
		return nil
	}
	metrics := filterByCapability(rc.metrics, source.CapERC20BalanceAtLatest)
	if len(metrics) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), rc.addressPlans, rc.anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-balance address sampling: %w", err)
	}
	tokens, err := uc.collectTokens(ctx, run.ChainID(), rc.tokenPlans, rc.anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-balance token sampling: %w", err)
	}

	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		a := addr
		subj := diff.Subject{Type: diff.SubjectAddress, Address: &a}
		for _, tok := range tokens {
			if err := ctx.Err(); err != nil {
				return err
			}
			t := tok
			results := fanOutFetch(ctx, rc.sources, uc.Budget, func(ctx context.Context, s source.Source) (source.ERC20BalanceResult, error) {
				return s.FetchERC20Balance(ctx, source.ERC20BalanceQuery{Address: a, Token: t})
			})
			if err := compareAndSave(ctx, uc, run, rc.resolver, metrics, results,
				subj, rc.anchor, rc.anchor, "erc20-balance", projectERC20Balance); err != nil {
				return err
			}
			if rc.summary != nil {
				rc.summary.addERC20Balance(a, t)
				rc.summary.addComparisons(len(metrics))
			}
		}
	}
	return nil
}

// collectAddresses resolves every AddressSamplingPlan through the
// sampler port and returns the deduplicated, byte-sorted union.
func (uc ExecuteRun) collectAddresses(
	ctx context.Context,
	chainID chain.ChainID,
	plans []verification.AddressSamplingPlan,
	anchor chain.BlockNumber,
) ([]chain.Address, error) {
	return collectSampled(ctx, plans, func(ctx context.Context, p verification.AddressSamplingPlan) ([]chain.Address, error) {
		return uc.Addresses.Sample(ctx, chainID, p, anchor)
	})
}

// collectTokens resolves every TokenSamplingPlan through the sampler
// port and returns the deduplicated, byte-sorted union.
func (uc ExecuteRun) collectTokens(
	ctx context.Context,
	chainID chain.ChainID,
	plans []verification.TokenSamplingPlan,
	anchor chain.BlockNumber,
) ([]chain.Address, error) {
	return collectSampled(ctx, plans, func(ctx context.Context, p verification.TokenSamplingPlan) ([]chain.Address, error) {
		return uc.Tokens.Sample(ctx, chainID, p, anchor)
	})
}

// resolver returns the configured ToleranceResolver or
// DefaultToleranceResolver when the caller left it nil.
func (uc ExecuteRun) resolver() ToleranceResolver {
	if uc.Tolerance == nil {
		return DefaultToleranceResolver{}
	}
	return uc.Tolerance
}

// --- Generic fan-out / compare helpers -------------------------------

// sourceFetchResult pairs a Source with its typed fetch outcome.
// err != nil means the fetch (or Budget reserve) failed; the caller
// drops that slot from the comparison.
type sourceFetchResult[R any] struct {
	source source.Source
	result R
	err    error
}

// fanOutFetch runs fetch concurrently across every source, enforcing
// the Budget reserve/refund contract when budget is non-nil. When
// Reserve fails, we do NOT call fetch — Reserve's error goes on the
// slot. When fetch fails after a successful Reserve, we Refund so
// the upstream never counts a call that failed in transit.
//
// The returned slice mirrors the input sources slice positionally,
// so callers that care about ordering (logs, metric labels) keep
// stability.
func fanOutFetch[R any](
	ctx context.Context,
	sources []source.Source,
	budget RateLimitBudget,
	fetch func(context.Context, source.Source) (R, error),
) []sourceFetchResult[R] {
	results := make([]sourceFetchResult[R], len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			if budget != nil {
				if err := budget.Reserve(ctx, s.ID(), 1); err != nil {
					results[i] = sourceFetchResult[R]{source: s, err: err}
					return
				}
			}
			r, err := fetch(ctx, s)
			if err != nil && budget != nil {
				_ = budget.Refund(ctx, s.ID(), 1)
			}
			results[i] = sourceFetchResult[R]{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
}

// samplingPlan is the tiny interface subset collectSampled needs
// from AddressSamplingPlan / TokenSamplingPlan.
type samplingPlan interface{ Kind() string }

// collectSampled runs each plan through sample, deduplicates the
// union by address bytes, and returns a byte-sorted slice. An error
// from any plan fails the whole collection so partial coverage
// cannot skew the comparison silently.
func collectSampled[P samplingPlan](
	ctx context.Context,
	plans []P,
	sample func(context.Context, P) ([]chain.Address, error),
) ([]chain.Address, error) {
	seen := map[chain.Address]struct{}{}
	var out []chain.Address
	for _, p := range plans {
		got, err := sample(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("plan %q: %w", p.Kind(), err)
		}
		for _, a := range got {
			if _, dup := seen[a]; dup {
				continue
			}
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].Bytes(), out[j].Bytes()) < 0
	})
	return out, nil
}

// snapshotProjector renders a source-specific result into the
// comparison-ready ValueSnapshot for a given Capability, or
// ok=false when the source did not populate the required field.
type snapshotProjector[R any] func(source.Capability, R) (diff.ValueSnapshot, bool)

// compareAndSave is the shared body every pass runs over its
// fetched results. It walks metrics, builds a per-metric snapshot
// map (filtered by capability support and the projector's extractor
// verdict), applies tolerance, and persists a DiffRecord on
// disagreement.
//
// recordBlock goes into Discrepancy.Block — this is the historical
// height under comparison (block for BlockImmutable / AddressAtBlock
// passes) or the Run's anchor for "latest" observations where the
// query has no specific block key.
//
// anchor goes into SaveDiffMeta.AnchorBlock — this is always the
// Run's finalized anchor, used for replay.
//
// saveKind is embedded in the save-diff error message for
// operators debugging persistence failures.
func compareAndSave[R any](
	ctx context.Context,
	uc ExecuteRun,
	run *verification.Run,
	resolver ToleranceResolver,
	metrics []verification.Metric,
	results []sourceFetchResult[R],
	subject diff.Subject,
	recordBlock chain.BlockNumber,
	anchor chain.BlockNumber,
	saveKind string,
	project snapshotProjector[R],
) error {
	for _, m := range metrics {
		snapshots := map[source.SourceID]diff.ValueSnapshot{}
		for _, fr := range results {
			if fr.err != nil {
				continue
			}
			if !fr.source.Supports(m.Capability) {
				continue
			}
			snap, ok := project(m.Capability, fr.result)
			if !ok {
				continue
			}
			snapshots[fr.source.ID()] = snap
		}
		if len(snapshots) < 2 {
			continue
		}
		switch applyTolerance(resolver.For(m), m, snapshots, anchor) {
		case toleranceAgree, toleranceDiscard:
			continue
		case toleranceDisagree:
			d, err := diff.NewDiscrepancy(
				run.ID(),
				m,
				recordBlock,
				subject,
				snapshots,
				uc.Clock.Now(),
			)
			if err != nil {
				continue
			}
			j := uc.Policy.Judge(d)
			meta := SaveDiffMeta{
				Tier:        m.Capability.Tier(),
				AnchorBlock: anchor,
			}
			if _, err := uc.Diffs.Save(ctx, &d, j, meta); err != nil {
				return fmt.Errorf("execute run: save diff (%s): %w", saveKind, err)
			}
		}
	}
	return nil
}

// --- Snapshot projectors ---------------------------------------------
//
// Each projector adapts a source-specific result type into the
// common ValueSnapshot shape consumed by compareAndSave. Block
// results have no ReflectedBlock field because the comparison is
// keyed to the queried block itself; the other result types thread
// ReflectedBlock through for AnchorWindowed tolerance.

func projectBlock(capb source.Capability, r source.BlockResult) (diff.ValueSnapshot, bool) {
	raw, ok := extractBlockField(capb, r)
	if !ok {
		return diff.ValueSnapshot{}, false
	}
	return diff.ValueSnapshot{Raw: raw, FetchedAt: r.FetchedAt}, true
}

func projectAddressLatest(capb source.Capability, r source.AddressLatestResult) (diff.ValueSnapshot, bool) {
	raw, ok := extractAddressLatestField(capb, r)
	if !ok {
		return diff.ValueSnapshot{}, false
	}
	return diff.ValueSnapshot{Raw: raw, FetchedAt: r.FetchedAt, ReflectedBlock: r.ReflectedBlock}, true
}

func projectAddressAtBlock(capb source.Capability, r source.AddressAtBlockResult) (diff.ValueSnapshot, bool) {
	raw, ok := extractAddressAtBlockField(capb, r)
	if !ok {
		return diff.ValueSnapshot{}, false
	}
	return diff.ValueSnapshot{Raw: raw, FetchedAt: r.FetchedAt, ReflectedBlock: r.ReflectedBlock}, true
}

func projectERC20Balance(capb source.Capability, r source.ERC20BalanceResult) (diff.ValueSnapshot, bool) {
	raw, ok := extractERC20BalanceField(capb, r)
	if !ok {
		return diff.ValueSnapshot{}, false
	}
	return diff.ValueSnapshot{Raw: raw, FetchedAt: r.FetchedAt, ReflectedBlock: r.ReflectedBlock}, true
}

func projectERC20Holdings(capb source.Capability, r source.ERC20HoldingsResult) (diff.ValueSnapshot, bool) {
	raw, ok := extractERC20HoldingsField(capb, r)
	if !ok {
		return diff.ValueSnapshot{}, false
	}
	return diff.ValueSnapshot{Raw: raw, FetchedAt: r.FetchedAt, ReflectedBlock: r.ReflectedBlock}, true
}

// --- Tolerance -------------------------------------------------------

// toleranceOutcome collapses per-pair Tolerance results into a
// whole-sample decision.
type toleranceOutcome int

const (
	toleranceAgree toleranceOutcome = iota
	toleranceDisagree
	toleranceDiscard
)

// applyTolerance runs tol over every unordered pair of snapshots
// and reduces the results:
//
//   - Any pair returning needDiscard=true drops the whole sample
//     (conservative: one stale source taints the observation).
//   - All pairs returning ok=true → agree, no diff.
//   - Otherwise → disagreement, caller persists a DiffRecord.
//
// Pairs are iterated in SourceID-sorted order so the outcome is
// deterministic given the same snapshot set.
func applyTolerance(
	tol diff.Tolerance,
	m verification.Metric,
	snapshots map[source.SourceID]diff.ValueSnapshot,
	anchor chain.BlockNumber,
) toleranceOutcome {
	type entry struct {
		sid  source.SourceID
		snap diff.ValueSnapshot
	}
	entries := make([]entry, 0, len(snapshots))
	for sid, s := range snapshots {
		entries = append(entries, entry{sid, s})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].sid < entries[j].sid })

	allOK := true
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			ctx := diff.CompareContext{
				AnchorBlock: anchor,
				ReflectedA:  entries[i].snap.ReflectedBlock,
				ReflectedB:  entries[j].snap.ReflectedBlock,
			}
			ok, discard := tol.Judge(entries[i].snap, entries[j].snap, m, ctx)
			if discard {
				return toleranceDiscard
			}
			if !ok {
				allOK = false
			}
		}
	}
	if allOK {
		return toleranceAgree
	}
	return toleranceDisagree
}

// --- Metric filters --------------------------------------------------

// filterByCategory returns the subset of metrics whose Category
// matches. Order is preserved.
func filterByCategory(metrics []verification.Metric, c verification.MetricCategory) []verification.Metric {
	out := make([]verification.Metric, 0, len(metrics))
	for _, m := range metrics {
		if m.Category == c {
			out = append(out, m)
		}
	}
	return out
}

// filterByCapability returns the subset of metrics whose Capability
// matches. Used by passes that share a Category with other passes
// (e.g., ERC20 holdings vs plain balance — both CatAddressLatest)
// and therefore cannot rely on a category filter alone.
func filterByCapability(metrics []verification.Metric, c source.Capability) []verification.Metric {
	out := make([]verification.Metric, 0, len(metrics))
	for _, m := range metrics {
		if m.Capability == c {
			out = append(out, m)
		}
	}
	return out
}
