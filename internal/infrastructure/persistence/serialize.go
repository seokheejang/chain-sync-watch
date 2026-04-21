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
