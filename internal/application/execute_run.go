package application

import (
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
// Scope in Phase 7C.1:
//
//   - BlockImmutable metrics only. AddressLatest / AddressAtBlock
//     require an address-sampling stage not yet wired up
//     (Phase 7C.2). Snapshot comparison is Observational and
//     produces no diffs in the current engine.
//   - Tolerance is resolved per Metric via the ToleranceResolver
//     port (nil falls back to ExactMatch across all categories).
//     Pair-wise Tolerance outcomes collapse to whole-sample
//     decisions: any pair signalling needDiscard drops the sample;
//     otherwise disagreement happens when any pair says ok=false.
//   - DiffRepository.Save receives SaveDiffMeta (Tier derived from
//     Capability, AnchorBlock from ChainHead.Finalized).
//   - Per-block fetches run in parallel across Sources; blocks
//     themselves are processed sequentially.
//   - No RateLimitBudget interaction — Tier A only, budget is
//     Phase 7C.2/7C.3 (Tier B sampling).
//
// Terminal states:
//
//   - run.Complete — reached the end of the block list without a
//     fatal error.
//   - run.Fail     — anchor resolution, tip resolution, or source
//     enumeration failed; OR fewer than two Sources were
//     configured for the chain.
//
// Per-(Source, Block, Metric) failures are non-fatal — ExecuteRun
// skips them and continues. The run still finishes "completed"
// because partial coverage is useful; failures surface via logs
// (Phase 10 observability) rather than failing the whole pass.
type ExecuteRun struct {
	Runs      RunRepository
	Diffs     DiffRepository
	Sources   SourceGateway
	ChainHead ChainHead
	Clock     Clock
	Policy    diff.JudgementPolicy
	// Tolerance is optional; nil means DefaultToleranceResolver{}.
	Tolerance ToleranceResolver
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
	if len(blockMetrics) == 0 {
		// Nothing the MVP engine can compare — a Run of only
		// AddressLatest / Snapshot metrics is legal but currently
		// produces zero diffs. Phase 7 wires those up.
		return nil
	}

	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := uc.compareBlock(ctx, run, block, anchor, sources, blockMetrics); err != nil {
			return err
		}
	}
	return nil
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
