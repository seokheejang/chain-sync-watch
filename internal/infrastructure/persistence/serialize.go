package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// --- Trigger serialisation ---------------------------------------

type manualTriggerJSON struct {
	User string `json:"user"`
}

type scheduledTriggerJSON struct {
	CronExpr string `json:"cron_expr"`
}

type realtimeTriggerJSON struct {
	BlockNumber chain.BlockNumber `json:"block_number"`
}

func marshalTrigger(t verification.Trigger) ([]byte, error) {
	switch v := t.(type) {
	case verification.ManualTrigger:
		return json.Marshal(manualTriggerJSON{User: v.User})
	case verification.ScheduledTrigger:
		return json.Marshal(scheduledTriggerJSON{CronExpr: v.CronExpr})
	case verification.RealtimeTrigger:
		return json.Marshal(realtimeTriggerJSON{BlockNumber: v.BlockNumber})
	}
	return nil, fmt.Errorf("persistence: unknown trigger type %T", t)
}

func unmarshalTrigger(kind string, data []byte) (verification.Trigger, error) {
	switch kind {
	case verification.TriggerKindManual:
		var v manualTriggerJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode manual trigger: %w", err)
		}
		return verification.ManualTrigger{User: v.User}, nil
	case verification.TriggerKindScheduled:
		var v scheduledTriggerJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode scheduled trigger: %w", err)
		}
		return verification.ScheduledTrigger{CronExpr: v.CronExpr}, nil
	case verification.TriggerKindRealtime:
		var v realtimeTriggerJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode realtime trigger: %w", err)
		}
		return verification.RealtimeTrigger{BlockNumber: v.BlockNumber}, nil
	}
	return nil, fmt.Errorf("persistence: unknown trigger kind %q", kind)
}

// --- Strategy serialisation ---------------------------------------

type rangeJSON struct {
	Start chain.BlockNumber `json:"start"`
	End   chain.BlockNumber `json:"end"`
}

type fixedListJSON struct {
	Numbers []chain.BlockNumber `json:"numbers"`
}

type latestNJSON struct {
	N uint `json:"n"`
}

type randomJSON struct {
	Range rangeJSON `json:"range"`
	Count uint      `json:"count"`
	Seed  int64     `json:"seed"`
}

type sparseStepsJSON struct {
	Range rangeJSON `json:"range"`
	Step  uint64    `json:"step"`
}

func marshalStrategy(s verification.SamplingStrategy) ([]byte, error) {
	switch v := s.(type) {
	case verification.FixedList:
		return json.Marshal(fixedListJSON{Numbers: v.Numbers})
	case verification.LatestN:
		return json.Marshal(latestNJSON{N: v.N})
	case verification.Random:
		return json.Marshal(randomJSON{
			Range: rangeJSON{Start: v.Range.Start, End: v.Range.End},
			Count: v.Count,
			Seed:  v.Seed,
		})
	case verification.SparseSteps:
		return json.Marshal(sparseStepsJSON{
			Range: rangeJSON{Start: v.Range.Start, End: v.Range.End},
			Step:  v.Step,
		})
	}
	return nil, fmt.Errorf("persistence: unknown strategy type %T", s)
}

// MarshalStrategy exposes the strategy marshaller so other
// infrastructure packages (notably queue.Dispatcher, which embeds
// a strategy into a scheduled-run payload) stay on the same wire
// format as the runs.strategy_data column. Without this shared
// path, the handler that decodes a scheduled-run payload would
// need a second marshaller that drifted the moment we added a new
// strategy kind.
func MarshalStrategy(s verification.SamplingStrategy) ([]byte, error) {
	return marshalStrategy(s)
}

// UnmarshalStrategy is the inverse: infrastructure packages that
// receive a strategy blob (handlers, HTTP layer) can decode it
// without re-implementing the kind switch.
func UnmarshalStrategy(kind string, data []byte) (verification.SamplingStrategy, error) {
	return unmarshalStrategy(kind, data)
}

// MetricByKey resolves a built-in metric key to its catalog entry.
// Returns ok=false for keys that are not in verification.AllMetrics()
// — callers treat that as a permanent "payload is corrupt" signal
// (asynq.SkipRetry, HTTP 4xx, etc.).
func MetricByKey(key string) (verification.Metric, bool) {
	m, ok := metricByKey[key]
	return m, ok
}

// MarshalAddressPlans / UnmarshalAddressPlans expose the plan
// array marshaller so queue.Dispatcher can ride the same JSONB
// format the runs.address_plans column uses. Without this, a
// scheduled-run payload would need a second encoder and the
// ScheduleRecord / Run persistence layers would drift the moment
// we add a new plan stratum.
func MarshalAddressPlans(plans []verification.AddressSamplingPlan) ([]byte, error) {
	return marshalAddressPlans(plans)
}

// UnmarshalAddressPlans is the inverse — decodes the JSONB array
// back to the domain slice. Both NULL and "[]" resolve to nil.
func UnmarshalAddressPlans(data []byte) ([]verification.AddressSamplingPlan, error) {
	return unmarshalAddressPlans(data)
}

func unmarshalStrategy(kind string, data []byte) (verification.SamplingStrategy, error) {
	switch kind {
	case verification.KindFixedList:
		var v fixedListJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode fixed_list: %w", err)
		}
		return verification.FixedList{Numbers: v.Numbers}, nil
	case verification.KindLatestN:
		var v latestNJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode latest_n: %w", err)
		}
		return verification.LatestN{N: v.N}, nil
	case verification.KindRandom:
		var v randomJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode random: %w", err)
		}
		br, err := chain.NewBlockRange(v.Range.Start, v.Range.End)
		if err != nil {
			return nil, fmt.Errorf("persistence: decode random range: %w", err)
		}
		return verification.Random{Range: br, Count: v.Count, Seed: v.Seed}, nil
	case verification.KindSparseSteps:
		var v sparseStepsJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode sparse_steps: %w", err)
		}
		br, err := chain.NewBlockRange(v.Range.Start, v.Range.End)
		if err != nil {
			return nil, fmt.Errorf("persistence: decode sparse_steps range: %w", err)
		}
		return verification.SparseSteps{Range: br, Step: v.Step}, nil
	}
	return nil, fmt.Errorf("persistence: unknown strategy kind %q", kind)
}

// --- AddressSamplingPlan serialisation -----------------------------
//
// The runs.address_plans column is a JSONB array of tagged envelopes
// — one envelope per plan — so a single Run can carry multiple
// stratums (typically known + top_n + recently_active). The envelope
// shape mirrors the trigger/strategy pattern: a stable Kind string
// plus an opaque Data blob whose schema is the plan-specific JSON
// struct below. Keeping the wire format stable is a hard requirement
// — plan Kind strings are persisted verbatim and must survive code
// reorganisation.

type addressPlanEnvelope struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type knownAddressesJSON struct {
	Addresses []chain.Address `json:"addresses"`
}

type topNHoldersJSON struct {
	N uint `json:"n"`
}

type randomAddressesJSON struct {
	Count uint  `json:"count"`
	Seed  int64 `json:"seed"`
}

type recentlyActiveJSON struct {
	RecentBlocks uint  `json:"recent_blocks"`
	Count        uint  `json:"count"`
	Seed         int64 `json:"seed"`
}

// marshalAddressPlans encodes the full plan list into the JSONB
// array stored in runs.address_plans. An empty / nil plans slice
// becomes "[]" rather than "null" so the NOT NULL column default
// holds.
func marshalAddressPlans(plans []verification.AddressSamplingPlan) ([]byte, error) {
	if len(plans) == 0 {
		return []byte("[]"), nil
	}
	envelopes := make([]addressPlanEnvelope, len(plans))
	for i, p := range plans {
		data, err := marshalAddressPlanData(p)
		if err != nil {
			return nil, err
		}
		envelopes[i] = addressPlanEnvelope{Kind: p.Kind(), Data: data}
	}
	return json.Marshal(envelopes)
}

func marshalAddressPlanData(p verification.AddressSamplingPlan) ([]byte, error) {
	switch v := p.(type) {
	case verification.KnownAddresses:
		return json.Marshal(knownAddressesJSON{Addresses: v.Addresses})
	case verification.TopNHolders:
		return json.Marshal(topNHoldersJSON{N: v.N})
	case verification.RandomAddresses:
		return json.Marshal(randomAddressesJSON{Count: v.Count, Seed: v.Seed})
	case verification.RecentlyActive:
		return json.Marshal(recentlyActiveJSON{
			RecentBlocks: v.RecentBlocks,
			Count:        v.Count,
			Seed:         v.Seed,
		})
	}
	return nil, fmt.Errorf("persistence: unknown address plan type %T", p)
}

// unmarshalAddressPlans decodes the JSONB array back into the
// domain slice. Both an empty array and literal NULL / empty bytes
// resolve to a nil slice — the zero value semantics the Run
// aggregate treats as "no address coverage".
func unmarshalAddressPlans(data []byte) ([]verification.AddressSamplingPlan, error) {
	if len(data) == 0 {
		return nil, nil
	}
	trimmed := string(data)
	if trimmed == "null" || trimmed == "[]" {
		return nil, nil
	}
	var envelopes []addressPlanEnvelope
	if err := json.Unmarshal(data, &envelopes); err != nil {
		return nil, fmt.Errorf("persistence: decode address plans: %w", err)
	}
	out := make([]verification.AddressSamplingPlan, 0, len(envelopes))
	for _, env := range envelopes {
		plan, err := unmarshalAddressPlan(env.Kind, env.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, nil
}

func unmarshalAddressPlan(kind string, data []byte) (verification.AddressSamplingPlan, error) {
	switch kind {
	case verification.KindKnownAddresses:
		var v knownAddressesJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode known addresses: %w", err)
		}
		return verification.KnownAddresses{Addresses: v.Addresses}, nil
	case verification.KindTopNHolders:
		var v topNHoldersJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode top_n_holders: %w", err)
		}
		return verification.TopNHolders{N: v.N}, nil
	case verification.KindRandomAddresses:
		var v randomAddressesJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode random_addresses: %w", err)
		}
		return verification.RandomAddresses{Count: v.Count, Seed: v.Seed}, nil
	case verification.KindRecentlyActive:
		var v recentlyActiveJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: decode recently_active: %w", err)
		}
		return verification.RecentlyActive{
			RecentBlocks: v.RecentBlocks,
			Count:        v.Count,
			Seed:         v.Seed,
		}, nil
	}
	return nil, fmt.Errorf("persistence: unknown address plan kind %q", kind)
}

// --- ValueSnapshot serialisation ----------------------------------

type valueSnapshotJSON struct {
	Raw            string             `json:"raw"`
	FetchedAt      time.Time          `json:"fetched_at"`
	ReflectedBlock *chain.BlockNumber `json:"reflected_block,omitempty"`
}

func marshalValues(values map[source.SourceID]diff.ValueSnapshot) ([]byte, error) {
	out := map[string]valueSnapshotJSON{}
	for sid, v := range values {
		snap := valueSnapshotJSON{
			Raw:       v.Raw,
			FetchedAt: v.FetchedAt,
		}
		if v.ReflectedBlock != nil {
			rb := *v.ReflectedBlock
			snap.ReflectedBlock = &rb
		}
		out[string(sid)] = snap
	}
	return json.Marshal(out)
}

func unmarshalValues(data []byte) (map[source.SourceID]diff.ValueSnapshot, error) {
	raw := map[string]valueSnapshotJSON{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("persistence: decode values: %w", err)
	}
	out := map[source.SourceID]diff.ValueSnapshot{}
	for sid, v := range raw {
		snap := diff.ValueSnapshot{
			Raw:       v.Raw,
			FetchedAt: v.FetchedAt,
		}
		if v.ReflectedBlock != nil {
			rb := *v.ReflectedBlock
			snap.ReflectedBlock = &rb
		}
		out[source.SourceID(sid)] = snap
	}
	return out, nil
}
