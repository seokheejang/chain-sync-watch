// Package dto hosts the HTTP-layer request/response shapes. They
// are deliberately separated from the domain types so the wire
// format can evolve without rippling through the application and
// domain layers.
//
// Each file follows the same pattern:
//
//   - Typed request/response structs with huma/JSON tags.
//   - Mapper functions converting between the DTOs and the
//     application-layer inputs/outputs.
//   - Mapper errors are returned as plain errors; routes wrap them
//     through httpapi.MapError into huma HTTP errors.
package dto

import (
	"errors"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// --- Sampling strategy --------------------------------------------------

// SamplingInput is a discriminated-union encoding of the four
// SamplingStrategy shapes the verification domain supports. Only
// the sub-struct matching Kind is expected to be populated; any
// other fields on the parent are ignored. This is the JSON shape
// that shows up in the OpenAPI schema and the frontend payloads.
type SamplingInput struct {
	Kind        string         `json:"kind" enum:"fixed_list,latest_n,random,sparse_steps" example:"latest_n" doc:"Sampling strategy discriminator"`
	FixedList   *FixedListIn   `json:"fixed_list,omitempty"`
	LatestN     *LatestNIn     `json:"latest_n,omitempty"`
	Random      *RandomIn      `json:"random,omitempty"`
	SparseSteps *SparseStepsIn `json:"sparse_steps,omitempty"`
}

// FixedListIn carries an explicit list of block numbers.
type FixedListIn struct {
	Numbers []uint64 `json:"numbers" minItems:"1" doc:"Block heights to verify"`
}

// LatestNIn samples the last N blocks up to the tip.
type LatestNIn struct {
	N uint `json:"n" minimum:"1" doc:"How many trailing blocks from tip"`
}

// RandomIn samples Count distinct blocks inside [Start, End] with
// the given Seed for reproducibility.
type RandomIn struct {
	Start uint64 `json:"start" doc:"Inclusive range start"`
	End   uint64 `json:"end" doc:"Inclusive range end"`
	Count uint   `json:"count" minimum:"1"`
	Seed  int64  `json:"seed"`
}

// SparseStepsIn samples every Step blocks inside [Start, End].
type SparseStepsIn struct {
	Start uint64 `json:"start" doc:"Inclusive range start"`
	End   uint64 `json:"end" doc:"Inclusive range end"`
	Step  uint64 `json:"step" minimum:"1"`
}

// ToDomain converts the DTO to its domain strategy. Returns a plain
// error when the discriminator is unknown or a required sub-struct
// is missing.
func (s SamplingInput) ToDomain() (verification.SamplingStrategy, error) {
	switch s.Kind {
	case verification.KindFixedList:
		if s.FixedList == nil {
			return nil, errors.New("sampling: fixed_list body is missing")
		}
		nums := make([]chain.BlockNumber, len(s.FixedList.Numbers))
		for i, n := range s.FixedList.Numbers {
			nums[i] = chain.BlockNumber(n)
		}
		return verification.FixedList{Numbers: nums}, nil
	case verification.KindLatestN:
		if s.LatestN == nil {
			return nil, errors.New("sampling: latest_n body is missing")
		}
		return verification.LatestN{N: s.LatestN.N}, nil
	case verification.KindRandom:
		if s.Random == nil {
			return nil, errors.New("sampling: random body is missing")
		}
		rng, err := chain.NewBlockRange(chain.BlockNumber(s.Random.Start), chain.BlockNumber(s.Random.End))
		if err != nil {
			return nil, fmt.Errorf("sampling: random range: %w", err)
		}
		return verification.Random{Range: rng, Count: s.Random.Count, Seed: s.Random.Seed}, nil
	case verification.KindSparseSteps:
		if s.SparseSteps == nil {
			return nil, errors.New("sampling: sparse_steps body is missing")
		}
		rng, err := chain.NewBlockRange(chain.BlockNumber(s.SparseSteps.Start), chain.BlockNumber(s.SparseSteps.End))
		if err != nil {
			return nil, fmt.Errorf("sampling: sparse_steps range: %w", err)
		}
		return verification.SparseSteps{Range: rng, Step: s.SparseSteps.Step}, nil
	default:
		return nil, fmt.Errorf("sampling: unknown kind %q", s.Kind)
	}
}

// --- Trigger ------------------------------------------------------------

// TriggerInput is the discriminated-union encoding of the three
// Trigger kinds. Schedule is attached at the top-level CreateRun
// payload, not here — ScheduleRunInput keeps them separate too.
type TriggerInput struct {
	Kind     string `json:"kind" enum:"manual,scheduled,realtime" example:"manual"`
	User     string `json:"user,omitempty" doc:"Operator identifier for manual triggers"`
	CronExpr string `json:"cron_expr,omitempty" doc:"Cron expression for scheduled triggers"`
	// RealtimeBlock is the height that fired a realtime trigger. Zero
	// on the create path (the scheduler fills it).
	RealtimeBlock uint64 `json:"realtime_block,omitempty"`
}

// ToDomain converts the DTO to the concrete Trigger value.
func (t TriggerInput) ToDomain() (verification.Trigger, error) {
	switch t.Kind {
	case verification.TriggerKindManual:
		return verification.ManualTrigger{User: t.User}, nil
	case verification.TriggerKindScheduled:
		return verification.ScheduledTrigger{CronExpr: t.CronExpr}, nil
	case verification.TriggerKindRealtime:
		return verification.RealtimeTrigger{BlockNumber: chain.BlockNumber(t.RealtimeBlock)}, nil
	default:
		return nil, fmt.Errorf("trigger: unknown kind %q", t.Kind)
	}
}

// --- Schedule -----------------------------------------------------------

// ScheduleInput carries a cron expression + timezone. Top-level on
// the CreateRun payload because ScheduleRunInput keeps it separate
// from Trigger.
type ScheduleInput struct {
	CronExpr string `json:"cron_expr" example:"0 */5 * * * *" doc:"Cron expression for scheduled runs"`
	Timezone string `json:"timezone,omitempty" example:"UTC" doc:"IANA timezone; empty defaults to UTC"`
}

// ToDomain returns the verification.Schedule value. Zero-value
// ScheduleInput returns a zero Schedule and a nil error so callers
// can detect "schedule not supplied" separately from a validation
// failure.
func (s ScheduleInput) ToDomain() (verification.Schedule, error) {
	if s.CronExpr == "" {
		return verification.Schedule{}, nil
	}
	return verification.NewSchedule(s.CronExpr, s.Timezone)
}

// --- Address plan -------------------------------------------------------

// AddressPlanInput is the discriminated-union encoding for the four
// AddressSamplingPlan stratums. The route layer mirrors the plan
// list straight from the payload into ScheduleRunInput.AddressPlans;
// the persistence layer serialises it through persistence.MarshalAddressPlans.
type AddressPlanInput struct {
	Kind           string             `json:"kind" enum:"known,top_n,random,recently_active"`
	Known          *KnownAddressesIn  `json:"known,omitempty"`
	TopN           *TopNHoldersIn     `json:"top_n,omitempty"`
	Random         *RandomAddressesIn `json:"random,omitempty"`
	RecentlyActive *RecentlyActiveIn  `json:"recently_active,omitempty"`
}

// KnownAddressesIn is a hand-picked address list.
type KnownAddressesIn struct {
	Addresses []string `json:"addresses" minItems:"1" doc:"0x-prefixed EIP-55 addresses"`
}

// TopNHoldersIn is the top-N-by-balance stratum.
type TopNHoldersIn struct {
	N uint `json:"n" minimum:"1"`
}

// RandomAddressesIn picks Count random addresses at Seed.
type RandomAddressesIn struct {
	Count uint  `json:"count" minimum:"1"`
	Seed  int64 `json:"seed"`
}

// RecentlyActiveIn derives addresses from tip-adjacent blocks.
type RecentlyActiveIn struct {
	RecentBlocks uint  `json:"recent_blocks" minimum:"1"`
	Count        uint  `json:"count" minimum:"1"`
	Seed         int64 `json:"seed"`
}

// ToDomain resolves the plan DTO to the domain plan value.
func (p AddressPlanInput) ToDomain() (verification.AddressSamplingPlan, error) {
	switch p.Kind {
	case verification.KindKnownAddresses:
		if p.Known == nil {
			return nil, errors.New("address_plan: known body missing")
		}
		addrs := make([]chain.Address, len(p.Known.Addresses))
		for i, s := range p.Known.Addresses {
			a, err := chain.NewAddress(s)
			if err != nil {
				return nil, fmt.Errorf("address_plan.known[%d]: %w", i, err)
			}
			addrs[i] = a
		}
		return verification.KnownAddresses{Addresses: addrs}, nil
	case verification.KindTopNHolders:
		if p.TopN == nil {
			return nil, errors.New("address_plan: top_n body missing")
		}
		return verification.TopNHolders{N: p.TopN.N}, nil
	case verification.KindRandomAddresses:
		if p.Random == nil {
			return nil, errors.New("address_plan: random body missing")
		}
		return verification.RandomAddresses{Count: p.Random.Count, Seed: p.Random.Seed}, nil
	case verification.KindRecentlyActive:
		if p.RecentlyActive == nil {
			return nil, errors.New("address_plan: recently_active body missing")
		}
		return verification.RecentlyActive{
			RecentBlocks: p.RecentlyActive.RecentBlocks,
			Count:        p.RecentlyActive.Count,
			Seed:         p.RecentlyActive.Seed,
		}, nil
	default:
		return nil, fmt.Errorf("address_plan: unknown kind %q", p.Kind)
	}
}

// --- Token plan ---------------------------------------------------------

// TokenPlanInput is the discriminated-union encoding for the
// TokenSamplingPlan stratums. Only the Known stratum ships today;
// future stratums (top_n_tokens, random_tokens, from_holdings) slot
// in with additional sub-structs and case branches.
type TokenPlanInput struct {
	Kind  string         `json:"kind" enum:"known_tokens"`
	Known *KnownTokensIn `json:"known,omitempty"`
}

// KnownTokensIn is a hand-picked ERC-20 contract list.
type KnownTokensIn struct {
	Tokens []string `json:"tokens" minItems:"1" doc:"0x-prefixed EIP-55 ERC-20 contract addresses"`
}

// ToDomain resolves the token plan DTO to the domain plan value.
func (p TokenPlanInput) ToDomain() (verification.TokenSamplingPlan, error) {
	switch p.Kind {
	case verification.KindKnownTokens:
		if p.Known == nil {
			return nil, errors.New("token_plan: known body missing")
		}
		tokens := make([]chain.Address, len(p.Known.Tokens))
		for i, s := range p.Known.Tokens {
			a, err := chain.NewAddress(s)
			if err != nil {
				return nil, fmt.Errorf("token_plan.known[%d]: %w", i, err)
			}
			tokens[i] = a
		}
		return verification.KnownTokens{Tokens: tokens}, nil
	default:
		return nil, fmt.Errorf("token_plan: unknown kind %q", p.Kind)
	}
}

// --- Run (request / view) -----------------------------------------------

// CreateRunRequest is the POST /runs body.
type CreateRunRequest struct {
	ChainID      uint64             `json:"chain_id" minimum:"1" example:"10"`
	Metrics      []string           `json:"metrics" minItems:"1" doc:"Built-in metric keys, e.g. block.hash"`
	Sampling     SamplingInput      `json:"sampling"`
	Trigger      TriggerInput       `json:"trigger"`
	Schedule     *ScheduleInput     `json:"schedule,omitempty" doc:"Required when trigger.kind is scheduled"`
	AddressPlans []AddressPlanInput `json:"address_plans,omitempty"`
	TokenPlans   []TokenPlanInput   `json:"token_plans,omitempty"`
}

// ToUseCase folds the request into application.ScheduleRunInput,
// short-circuiting on the first mapping error so the caller gets a
// specific message back (mapped to HTTP 400 by errors.go).
func (r CreateRunRequest) ToUseCase() (application.ScheduleRunInput, error) {
	strategy, err := r.Sampling.ToDomain()
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	trigger, err := r.Trigger.ToDomain()
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	metrics, err := ResolveMetrics(r.Metrics)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	var sched verification.Schedule
	if r.Schedule != nil {
		sched, err = r.Schedule.ToDomain()
		if err != nil {
			return application.ScheduleRunInput{}, err
		}
	}
	plans, err := ResolveAddressPlans(r.AddressPlans)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	tokens, err := ResolveTokenPlans(r.TokenPlans)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	return application.ScheduleRunInput{
		ChainID:      chain.ChainID(r.ChainID),
		Strategy:     strategy,
		Metrics:      metrics,
		Trigger:      trigger,
		Schedule:     sched,
		AddressPlans: plans,
		TokenPlans:   tokens,
	}, nil
}

// ResolveMetrics maps metric keys to the registered verification.Metric
// entries, surfacing unknown keys as errors so both /runs and
// /schedules responders produce the same 400 message.
func ResolveMetrics(keys []string) ([]verification.Metric, error) {
	out := make([]verification.Metric, 0, len(keys))
	for _, key := range keys {
		m, ok := persistence.MetricByKey(key)
		if !ok {
			return nil, fmt.Errorf("metrics: unknown key %q", key)
		}
		out = append(out, m)
	}
	return out, nil
}

// ResolveAddressPlans maps AddressPlanInput entries to their domain
// equivalents, prefixing plan errors with their slice index.
func ResolveAddressPlans(inputs []AddressPlanInput) ([]verification.AddressSamplingPlan, error) {
	out := make([]verification.AddressSamplingPlan, 0, len(inputs))
	for i, p := range inputs {
		dp, err := p.ToDomain()
		if err != nil {
			return nil, fmt.Errorf("address_plans[%d]: %w", i, err)
		}
		out = append(out, dp)
	}
	return out, nil
}

// ResolveTokenPlans maps TokenPlanInput entries to their domain
// equivalents, prefixing plan errors with their slice index.
func ResolveTokenPlans(inputs []TokenPlanInput) ([]verification.TokenSamplingPlan, error) {
	out := make([]verification.TokenSamplingPlan, 0, len(inputs))
	for i, p := range inputs {
		dp, err := p.ToDomain()
		if err != nil {
			return nil, fmt.Errorf("token_plans[%d]: %w", i, err)
		}
		out = append(out, dp)
	}
	return out, nil
}

// CreateRunResponse is the POST /runs body.
type CreateRunResponse struct {
	RunID string  `json:"run_id"`
	JobID *string `json:"job_id,omitempty" doc:"Populated only for scheduled triggers"`
}

// RunView is the canonical GET representation of a Run. The shape
// is stable across GET /runs/{id} and the list endpoint's entries.
type RunView struct {
	ID               string     `json:"id"`
	ChainID          uint64     `json:"chain_id"`
	Status           string     `json:"status"`
	StrategyKind     string     `json:"strategy_kind"`
	Metrics          []string   `json:"metrics"`
	TriggerKind      string     `json:"trigger_kind"`
	AddressPlanKinds []string   `json:"address_plan_kinds,omitempty"`
	TokenPlanKinds   []string   `json:"token_plan_kinds,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
}

// ToRunView renders the domain aggregate into the wire shape.
func ToRunView(r *verification.Run) RunView {
	metrics := r.Metrics()
	mkeys := make([]string, len(metrics))
	for i, m := range metrics {
		mkeys[i] = m.Key
	}
	addressPlans := r.AddressPlans()
	var akinds []string
	if len(addressPlans) > 0 {
		akinds = make([]string, len(addressPlans))
		for i, p := range addressPlans {
			akinds[i] = p.Kind()
		}
	}
	tokenPlans := r.TokenPlans()
	var tkinds []string
	if len(tokenPlans) > 0 {
		tkinds = make([]string, len(tokenPlans))
		for i, p := range tokenPlans {
			tkinds[i] = p.Kind()
		}
	}
	return RunView{
		ID:               string(r.ID()),
		ChainID:          uint64(r.ChainID()),
		Status:           string(r.Status()),
		StrategyKind:     r.Strategy().Kind(),
		Metrics:          mkeys,
		TriggerKind:      r.Trigger().Kind(),
		AddressPlanKinds: akinds,
		TokenPlanKinds:   tkinds,
		CreatedAt:        r.CreatedAt(),
		StartedAt:        r.StartedAt(),
		FinishedAt:       r.FinishedAt(),
		ErrorMessage:     r.ErrorMessage(),
	}
}

// ListRunsResponse is the paginated GET /runs body.
type ListRunsResponse struct {
	Items  []RunView `json:"items"`
	Total  int       `json:"total" doc:"Total rows matching the filter (not just this page)"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}
