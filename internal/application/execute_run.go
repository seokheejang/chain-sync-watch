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
// Scope in Phase 7C.3:
//
//   - BlockImmutable metrics: fetched once per block, compared
//     across every Source that supports the Capability.
//   - AddressLatest metrics (Phase 7C.3): when the Run carries at
//     least one AddressSamplingPlan AND an AddressSampler is
//     injected, ExecuteRun resolves every plan, deduplicates the
//     merged address set, and fans out FetchAddressLatest across
//     the Sources. ValueSnapshot.ReflectedBlock is populated so
//     AnchorWindowed tolerance can discard stale samples.
//   - AddressAtBlock / ERC20 / Snapshot: not yet wired.
//   - Tolerance is resolved per Metric via the ToleranceResolver
//     port (nil falls back to ExactMatch across all categories).
//     Pair-wise Tolerance outcomes collapse to whole-sample
//     decisions: any pair signalling needDiscard drops the sample;
//     otherwise disagreement happens when any pair says ok=false.
//   - DiffRepository.Save receives SaveDiffMeta (Tier derived from
//     Capability, AnchorBlock from ChainHead.Finalized).
//   - Per-block and per-address fetches run in parallel across
//     Sources; the outer loops (blocks, addresses) are sequential.
//   - Budget integration (7C.3): when Budget is non-nil, every
//     AddressLatest fetch reserves one unit keyed by Source ID
//     before the call and refunds on error. Block fetches are not
//     yet budget-gated (low call volume; follow-up).
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
// via logs (Phase 10 observability) rather than failing the whole
// pass.
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

	blocks := run.Strategy().Blocks(verification.SamplingContext{
		TipBlock: tip,
		Now:      uc.Clock.Now(),
	})

	blockMetrics := filterByCategory(run.Metrics(), verification.CatBlockImmutable)
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(blockMetrics) == 0 {
			break
		}
		if err := uc.compareBlock(ctx, run, block, anchor, sources, blockMetrics); err != nil {
			return err
		}
	}

	return uc.runAddressLatestPass(ctx, run, anchor, sources)
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
func (uc ExecuteRun) runAddressLatestPass(
	ctx context.Context,
	run *verification.Run,
	anchor chain.BlockNumber,
	sources []source.Source,
) error {
	plans := run.AddressPlans()
	if len(plans) == 0 || uc.Addresses == nil {
		return nil
	}
	addressMetrics := filterByCategory(run.Metrics(), verification.CatAddressLatest)
	if len(addressMetrics) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), plans, anchor)
	if err != nil {
		return fmt.Errorf("execute run: address sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := uc.compareAddressLatest(ctx, run, addr, anchor, sources, addressMetrics); err != nil {
			return err
		}
	}
	return nil
}

// collectAddresses resolves every plan through the sampler port and
// returns the deduplicated, byte-sorted union. An error from any
// plan fails the whole pass — partial address coverage would skew
// the comparison silently.
func (uc ExecuteRun) collectAddresses(
	ctx context.Context,
	chainID chain.ChainID,
	plans []verification.AddressSamplingPlan,
	anchor chain.BlockNumber,
) ([]chain.Address, error) {
	seen := map[chain.Address]struct{}{}
	var out []chain.Address
	for _, p := range plans {
		got, err := uc.Addresses.Sample(ctx, chainID, p, anchor)
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

// compareBlock fetches `block` from every Source in parallel and
// emits a DiffRecord for every BlockImmutable metric where the
// configured Tolerance says the sources disagree.
func (uc ExecuteRun) compareBlock(
	ctx context.Context,
	run *verification.Run,
	block chain.BlockNumber,
	anchor chain.BlockNumber,
	sources []source.Source,
	metrics []verification.Metric,
) error {
	results := fetchBlockAll(ctx, sources, block)
	resolver := uc.resolver()

	for _, m := range metrics {
		snapshots := map[source.SourceID]diff.ValueSnapshot{}
		for _, fr := range results {
			if fr.err != nil {
				continue
			}
			if !fr.source.Supports(m.Capability) {
				continue
			}
			raw, ok := extractBlockField(m.Capability, fr.result)
			if !ok {
				continue
			}
			snapshots[fr.source.ID()] = diff.ValueSnapshot{
				Raw:       raw,
				FetchedAt: fr.result.FetchedAt,
			}
		}
		if len(snapshots) < 2 {
			continue
		}
		outcome := applyTolerance(resolver.For(m), m, snapshots, anchor)
		switch outcome {
		case toleranceAgree, toleranceDiscard:
			continue
		case toleranceDisagree:
			d, err := diff.NewDiscrepancy(
				run.ID(),
				m,
				block,
				diff.Subject{Type: diff.SubjectBlock},
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
				return fmt.Errorf("execute run: save diff: %w", err)
			}
		}
	}
	return nil
}

// compareAddressLatest fetches address latest state from every
// Source in parallel and emits a DiffRecord for each AddressLatest
// metric where the configured Tolerance says the sources disagree.
// ValueSnapshot.ReflectedBlock is populated from the source
// response so AnchorWindowed tolerance can discard stale samples
// — critical for CatAddressLatest because different indexers race
// the tip by one or two blocks.
func (uc ExecuteRun) compareAddressLatest(
	ctx context.Context,
	run *verification.Run,
	addr chain.Address,
	anchor chain.BlockNumber,
	sources []source.Source,
	metrics []verification.Metric,
) error {
	results := uc.fetchAddressLatestAll(ctx, sources, addr)
	resolver := uc.resolver()

	for _, m := range metrics {
		snapshots := map[source.SourceID]diff.ValueSnapshot{}
		for _, fr := range results {
			if fr.err != nil {
				continue
			}
			if !fr.source.Supports(m.Capability) {
				continue
			}
			raw, ok := extractAddressLatestField(m.Capability, fr.result)
			if !ok {
				continue
			}
			snapshots[fr.source.ID()] = diff.ValueSnapshot{
				Raw:            raw,
				FetchedAt:      fr.result.FetchedAt,
				ReflectedBlock: fr.result.ReflectedBlock,
			}
		}
		if len(snapshots) < 2 {
			continue
		}
		outcome := applyTolerance(resolver.For(m), m, snapshots, anchor)
		switch outcome {
		case toleranceAgree, toleranceDiscard:
			continue
		case toleranceDisagree:
			a := addr
			d, err := diff.NewDiscrepancy(
				run.ID(),
				m,
				anchor,
				diff.Subject{Type: diff.SubjectAddress, Address: &a},
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
				return fmt.Errorf("execute run: save diff (address): %w", err)
			}
		}
	}
	return nil
}

// addrFetchResult pairs a Source with its FetchAddressLatest outcome.
type addrFetchResult struct {
	source source.Source
	result source.AddressLatestResult
	err    error
}

// fetchAddressLatestAll fans out FetchAddressLatest across every
// Source in parallel. When Budget is non-nil, each source reserves
// one unit keyed by Source ID before the call; a Reserve failure
// (ErrBudgetExhausted) records the error on that slot and skips the
// fetch. A fetch error after a successful Reserve triggers a Refund
// — the upstream never counted a call that failed in transit.
func (uc ExecuteRun) fetchAddressLatestAll(
	ctx context.Context,
	sources []source.Source,
	addr chain.Address,
) []addrFetchResult {
	results := make([]addrFetchResult, len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			if uc.Budget != nil {
				if err := uc.Budget.Reserve(ctx, s.ID(), 1); err != nil {
					results[i] = addrFetchResult{source: s, err: err}
					return
				}
			}
			r, err := s.FetchAddressLatest(ctx, source.AddressQuery{Address: addr})
			if err != nil && uc.Budget != nil {
				_ = uc.Budget.Refund(ctx, s.ID(), 1)
			}
			results[i] = addrFetchResult{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
}

// resolver returns the configured ToleranceResolver or
// DefaultToleranceResolver when the caller left it nil.
func (uc ExecuteRun) resolver() ToleranceResolver {
	if uc.Tolerance == nil {
		return DefaultToleranceResolver{}
	}
	return uc.Tolerance
}

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

// fetchResult pairs a Source with its FetchBlock outcome.
type fetchResult struct {
	source source.Source
	result source.BlockResult
	err    error
}

// fetchBlockAll fans out FetchBlock across every Source in
// parallel. Order of the returned slice mirrors the input sources
// slice, so callers retain positional stability for logs and
// metric labels.
func fetchBlockAll(ctx context.Context, sources []source.Source, block chain.BlockNumber) []fetchResult {
	results := make([]fetchResult, len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			r, err := s.FetchBlock(ctx, source.BlockQuery{Number: block})
			results[i] = fetchResult{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
}

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
