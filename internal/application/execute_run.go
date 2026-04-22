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
// Scope in Phase 7H:
//
//   - BlockImmutable metrics: fetched once per block, compared
//     across every Source that supports the Capability.
//   - AddressLatest metrics (Phase 7C.3): when the Run carries at
//     least one AddressSamplingPlan AND an AddressSampler is
//     injected, ExecuteRun resolves every plan, deduplicates the
//     merged address set, and fans out FetchAddressLatest across
//     the Sources. ValueSnapshot.ReflectedBlock is populated so
//     AnchorWindowed tolerance can discard stale samples.
//   - AddressAtBlock metrics (Phase 7G): archive-backed historical
//     state. Same AddressSamplingPlan set as AddressLatest, but the
//     fan-out is (address × block) cartesian over the Run's block
//     sampling strategy. Sources that don't support archive reads
//     return ErrUnsupported and are skipped.
//   - ERC20 holdings (Phase 7H): per-address FetchERC20Holdings with
//     a canonical sorted "contract=balance" serialization, driven by
//     the same AddressSamplingPlan set. Tier B metric — Budget
//     reserve/refund is applied.
//   - ERC20 balance (Phase 7I): (address × token) cartesian over
//     AddressSamplingPlan × TokenSamplingPlan sets. Tokens are
//     resolved through the TokenSampler port (mirrors the Addresses
//     path). Tier C metric — Budget applied per fetch.
//   - Snapshot: not yet wired.
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

	if err := uc.runAddressLatestPass(ctx, run, anchor, sources); err != nil {
		return err
	}
	if err := uc.runAddressAtBlockPass(ctx, run, blocks, anchor, sources); err != nil {
		return err
	}
	if err := uc.runERC20HoldingsLatestPass(ctx, run, anchor, sources); err != nil {
		return err
	}
	return uc.runERC20BalanceLatestPass(ctx, run, anchor, sources)
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
// from FetchAddressAtBlock; compareAddressAtBlock skips those
// fetches like any other per-call error.
func (uc ExecuteRun) runAddressAtBlockPass(
	ctx context.Context,
	run *verification.Run,
	blocks []chain.BlockNumber,
	anchor chain.BlockNumber,
	sources []source.Source,
) error {
	plans := run.AddressPlans()
	if len(plans) == 0 || uc.Addresses == nil {
		return nil
	}
	addressMetrics := filterByCategory(run.Metrics(), verification.CatAddressAtBlock)
	if len(addressMetrics) == 0 {
		return nil
	}
	if len(blocks) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), plans, anchor)
	if err != nil {
		return fmt.Errorf("execute run: address-at-block sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, block := range blocks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := uc.compareAddressAtBlock(ctx, run, addr, block, anchor, sources, addressMetrics); err != nil {
				return err
			}
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
func (uc ExecuteRun) runERC20BalanceLatestPass(
	ctx context.Context,
	run *verification.Run,
	anchor chain.BlockNumber,
	sources []source.Source,
) error {
	addressPlans := run.AddressPlans()
	tokenPlans := run.TokenPlans()
	if len(addressPlans) == 0 || len(tokenPlans) == 0 {
		return nil
	}
	if uc.Addresses == nil || uc.Tokens == nil {
		return nil
	}
	metrics := filterByCapability(run.Metrics(), source.CapERC20BalanceAtLatest)
	if len(metrics) == 0 {
		return nil
	}

	addrs, err := uc.collectAddresses(ctx, run.ChainID(), addressPlans, anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-balance address sampling: %w", err)
	}
	tokens, err := uc.collectTokens(ctx, run.ChainID(), tokenPlans, anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-balance token sampling: %w", err)
	}

	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, tok := range tokens {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := uc.compareERC20BalanceLatest(ctx, run, addr, tok, anchor, sources, metrics); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectTokens is the token counterpart to collectAddresses — same
// dedup + byte-sort discipline so the (address × token) cartesian
// is deterministic across runs with the same inputs.
func (uc ExecuteRun) collectTokens(
	ctx context.Context,
	chainID chain.ChainID,
	plans []verification.TokenSamplingPlan,
	anchor chain.BlockNumber,
) ([]chain.Address, error) {
	seen := map[chain.Address]struct{}{}
	var out []chain.Address
	for _, p := range plans {
		got, err := uc.Tokens.Sample(ctx, chainID, p, anchor)
		if err != nil {
			return nil, fmt.Errorf("plan %q: %w", p.Kind(), err)
		}
		for _, t := range got {
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].Bytes(), out[j].Bytes()) < 0
	})
	return out, nil
}

// compareERC20BalanceLatest fetches (addr, token) balance from every
// Source in parallel. Discrepancy.Block is the Run's anchor —
// "latest" observations reuse the AddressLatest convention. The
// persisted Subject stays SubjectAddress keyed by `addr`; the token
// contract is NOT encoded in the Discrepancy key, so callers that
// want per-(address, token) DiffRecord lookup must read the
// Metric.Key and the ValueSnapshot.Raw together. A future schema
// extension can add a SubjectToken field if operators ask for it.
func (uc ExecuteRun) compareERC20BalanceLatest(
	ctx context.Context,
	run *verification.Run,
	addr chain.Address,
	token chain.Address,
	anchor chain.BlockNumber,
	sources []source.Source,
	metrics []verification.Metric,
) error {
	results := uc.fetchERC20BalanceAll(ctx, sources, addr, token)
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
			raw, ok := extractERC20BalanceField(m.Capability, fr.result)
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
				return fmt.Errorf("execute run: save diff (erc20-balance): %w", err)
			}
		}
	}
	return nil
}

// erc20BalanceFetchResult pairs a Source with its FetchERC20Balance
// outcome.
type erc20BalanceFetchResult struct {
	source source.Source
	result source.ERC20BalanceResult
	err    error
}

// fetchERC20BalanceAll fans out FetchERC20Balance across every
// Source in parallel with the same Budget reserve/refund semantics
// as fetchAddressLatestAll. ERC20 Balance is Tier C — both RPC
// (via eth_call) and indexers serve it, but call volume is the
// (address × token) product, so Budget gating is important.
func (uc ExecuteRun) fetchERC20BalanceAll(
	ctx context.Context,
	sources []source.Source,
	addr chain.Address,
	token chain.Address,
) []erc20BalanceFetchResult {
	results := make([]erc20BalanceFetchResult, len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			if uc.Budget != nil {
				if err := uc.Budget.Reserve(ctx, s.ID(), 1); err != nil {
					results[i] = erc20BalanceFetchResult{source: s, err: err}
					return
				}
			}
			r, err := s.FetchERC20Balance(ctx, source.ERC20BalanceQuery{Address: addr, Token: token})
			if err != nil && uc.Budget != nil {
				_ = uc.Budget.Refund(ctx, s.ID(), 1)
			}
			results[i] = erc20BalanceFetchResult{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
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
func (uc ExecuteRun) runERC20HoldingsLatestPass(
	ctx context.Context,
	run *verification.Run,
	anchor chain.BlockNumber,
	sources []source.Source,
) error {
	plans := run.AddressPlans()
	if len(plans) == 0 || uc.Addresses == nil {
		return nil
	}
	metrics := filterByCapability(run.Metrics(), source.CapERC20HoldingsAtLatest)
	if len(metrics) == 0 {
		return nil
	}
	addrs, err := uc.collectAddresses(ctx, run.ChainID(), plans, anchor)
	if err != nil {
		return fmt.Errorf("execute run: erc20-holdings sampling: %w", err)
	}
	for _, addr := range addrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := uc.compareERC20HoldingsLatest(ctx, run, addr, anchor, sources, metrics); err != nil {
			return err
		}
	}
	return nil
}

// compareERC20HoldingsLatest fetches the holdings for `addr` from
// every Source in parallel and emits a DiffRecord whenever the
// canonical serializations disagree. Discrepancy.Block is the
// Run's finalized anchor — "latest" observations are ambient rather
// than keyed to a specific queried height, so we use anchor as a
// stable identifier (same convention compareAddressLatest uses).
func (uc ExecuteRun) compareERC20HoldingsLatest(
	ctx context.Context,
	run *verification.Run,
	addr chain.Address,
	anchor chain.BlockNumber,
	sources []source.Source,
	metrics []verification.Metric,
) error {
	results := uc.fetchERC20HoldingsAll(ctx, sources, addr)
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
			raw, ok := extractERC20HoldingsField(m.Capability, fr.result)
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
				return fmt.Errorf("execute run: save diff (erc20-holdings): %w", err)
			}
		}
	}
	return nil
}

// erc20HoldingsFetchResult pairs a Source with its FetchERC20Holdings
// outcome.
type erc20HoldingsFetchResult struct {
	source source.Source
	result source.ERC20HoldingsResult
	err    error
}

// fetchERC20HoldingsAll fans out FetchERC20Holdings across every
// Source in parallel with the same Budget reserve/refund semantics
// as fetchAddressLatestAll. Holdings is Tier B (indexer-derived),
// so callers almost always run this with a Budget wired up.
func (uc ExecuteRun) fetchERC20HoldingsAll(
	ctx context.Context,
	sources []source.Source,
	addr chain.Address,
) []erc20HoldingsFetchResult {
	results := make([]erc20HoldingsFetchResult, len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			if uc.Budget != nil {
				if err := uc.Budget.Reserve(ctx, s.ID(), 1); err != nil {
					results[i] = erc20HoldingsFetchResult{source: s, err: err}
					return
				}
			}
			r, err := s.FetchERC20Holdings(ctx, source.ERC20HoldingsQuery{Address: addr})
			if err != nil && uc.Budget != nil {
				_ = uc.Budget.Refund(ctx, s.ID(), 1)
			}
			results[i] = erc20HoldingsFetchResult{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
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

// compareAddressAtBlock fetches (addr, block) from every Source in
// parallel and emits a DiffRecord for each AddressAtBlock metric
// where the sources disagree. The persisted Discrepancy.Block is
// `block` (the historical height under comparison), while
// SaveDiffMeta.AnchorBlock stays the Run's verification anchor —
// they are two different properties (one is the query key, the
// other is the Run's anchor-point for replay).
func (uc ExecuteRun) compareAddressAtBlock(
	ctx context.Context,
	run *verification.Run,
	addr chain.Address,
	block chain.BlockNumber,
	anchor chain.BlockNumber,
	sources []source.Source,
	metrics []verification.Metric,
) error {
	results := uc.fetchAddressAtBlockAll(ctx, sources, addr, block)
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
			raw, ok := extractAddressAtBlockField(m.Capability, fr.result)
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
				block,
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
				return fmt.Errorf("execute run: save diff (address-at-block): %w", err)
			}
		}
	}
	return nil
}

// addrAtBlockFetchResult pairs a Source with its FetchAddressAtBlock
// outcome.
type addrAtBlockFetchResult struct {
	source source.Source
	result source.AddressAtBlockResult
	err    error
}

// fetchAddressAtBlockAll fans out FetchAddressAtBlock across every
// Source in parallel with the same Budget reserve/refund semantics
// as fetchAddressLatestAll. Sources that don't serve archive reads
// surface ErrUnsupported here and the error short-circuits the
// per-source slot; the caller treats unsupported sources and
// transport errors identically (skip the snapshot).
func (uc ExecuteRun) fetchAddressAtBlockAll(
	ctx context.Context,
	sources []source.Source,
	addr chain.Address,
	block chain.BlockNumber,
) []addrAtBlockFetchResult {
	results := make([]addrAtBlockFetchResult, len(sources))
	var wg sync.WaitGroup
	wg.Add(len(sources))
	for i, s := range sources {
		go func() {
			defer wg.Done()
			if uc.Budget != nil {
				if err := uc.Budget.Reserve(ctx, s.ID(), 1); err != nil {
					results[i] = addrAtBlockFetchResult{source: s, err: err}
					return
				}
			}
			r, err := s.FetchAddressAtBlock(ctx, source.AddressAtBlockQuery{Address: addr, Block: block})
			if err != nil && uc.Budget != nil {
				_ = uc.Budget.Refund(ctx, s.ID(), 1)
			}
			results[i] = addrAtBlockFetchResult{source: s, result: r, err: err}
		}()
	}
	wg.Wait()
	return results
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
