package persistence

import (
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// metricByKey indexes the built-in metric catalog by Key so the
// mapper can resolve a TEXT[] column back into []Metric. User-
// defined metrics (constructed outside verification.AllMetrics())
// are currently not round-trippable — rehydration rejects unknown
// keys. Extending to user metrics requires persisting Category +
// Capability alongside the key; we defer that until the pattern
// shows up in practice.
var metricByKey = buildMetricIndex()

func buildMetricIndex() map[string]verification.Metric {
	out := map[string]verification.Metric{}
	for _, m := range verification.AllMetrics() {
		out[m.Key] = m
	}
	return out
}

// --- Run ------------------------------------------------------------

func toRunModel(r *verification.Run) (runModel, error) {
	trigData, err := marshalTrigger(r.Trigger())
	if err != nil {
		return runModel{}, err
	}
	stratData, err := marshalStrategy(r.Strategy())
	if err != nil {
		return runModel{}, err
	}

	keys := make([]string, 0, len(r.Metrics()))
	for _, m := range r.Metrics() {
		keys = append(keys, m.Key)
	}

	return runModel{
		ID:           string(r.ID()),
		ChainID:      r.ChainID().Uint64(),
		Status:       string(r.Status()),
		TriggerType:  r.Trigger().Kind(),
		TriggerData:  trigData,
		StrategyKind: r.Strategy().Kind(),
		StrategyData: stratData,
		Metrics:      keys,
		ErrorMsg:     r.ErrorMessage(),
		CreatedAt:    r.CreatedAt(),
		StartedAt:    r.StartedAt(),
		FinishedAt:   r.FinishedAt(),
	}, nil
}

func toRun(m runModel) (*verification.Run, error) {
	trigger, err := unmarshalTrigger(m.TriggerType, m.TriggerData)
	if err != nil {
		return nil, err
	}
	strategy, err := unmarshalStrategy(m.StrategyKind, m.StrategyData)
	if err != nil {
		return nil, err
	}
	metrics := make([]verification.Metric, 0, len(m.Metrics))
	for _, k := range m.Metrics {
		met, ok := metricByKey[k]
		if !ok {
			return nil, fmt.Errorf("persistence: unknown metric key %q", k)
		}
		metrics = append(metrics, met)
	}

	return verification.Rehydrate(
		verification.RunID(m.ID),
		chain.ChainID(m.ChainID),
		strategy,
		metrics,
		trigger,
		verification.Status(m.Status),
		m.CreatedAt,
		m.StartedAt,
		m.FinishedAt,
		m.ErrorMsg,
	)
}

// --- Diff -----------------------------------------------------------

func toDiffModel(d *diff.Discrepancy, j diff.Judgement, meta application.SaveDiffMeta) (diffModel, error) {
	values, err := marshalValues(d.Values)
	if err != nil {
		return diffModel{}, err
	}

	var subjectAddr []byte
	if d.Subject.Address != nil {
		addr := d.Subject.Address.Bytes()
		subjectAddr = addr
	}

	// Prefer meta.Tier when the caller supplied one; fall back to
	// deriving from the metric's Capability so older call sites
	// (and direct fake-repo use) still get a sensible Tier column.
	tier := int16(meta.Tier)
	if tier == 0 {
		tier = int16(d.Metric.Capability.Tier())
	}
	var tierPtr *int16
	if tier != 0 {
		t := tier
		tierPtr = &t
	}

	var anchorPtr *int64
	if meta.AnchorBlock != 0 {
		//nolint:gosec // G115: block heights stay within int64.
		a := int64(meta.AnchorBlock.Uint64())
		anchorPtr = &a
	}

	trustedStrings := make([]string, len(j.TrustedSources))
	for i, s := range j.TrustedSources {
		trustedStrings[i] = string(s)
	}

	return diffModel{
		RunID:          string(d.RunID),
		MetricKey:      d.Metric.Key,
		MetricCategory: string(d.Metric.Category),
		//nolint:gosec // G115: block heights stay within int64.
		BlockNumber:    int64(d.Block.Uint64()),
		SubjectType:    string(d.Subject.Type),
		SubjectAddr:    subjectAddr,
		Values:         values,
		Severity:       string(j.Severity),
		TrustedSources: trustedStrings,
		Reasoning:      j.Reasoning,
		DetectedAt:     d.DetectedAt,
		Tier:           tierPtr,
		AnchorBlock:    anchorPtr,
		SamplingSeed:   meta.SamplingSeed,
	}, nil
}

func toDiffRecord(m diffModel) (application.DiffRecord, error) {
	values, err := unmarshalValues(m.Values)
	if err != nil {
		return application.DiffRecord{}, err
	}

	var subject diff.Subject
	subject.Type = diff.SubjectType(m.SubjectType)
	if len(m.SubjectAddr) == 20 {
		var a chain.Address
		copy(a[:], m.SubjectAddr)
		subject.Address = &a
	}

	metric, ok := metricByKey[m.MetricKey]
	if !ok {
		return application.DiffRecord{}, fmt.Errorf(
			"persistence: unknown metric key %q", m.MetricKey,
		)
	}

	d := diff.Discrepancy{
		RunID:      verification.RunID(m.RunID),
		Metric:     metric,
		Block:      chain.BlockNumber(m.BlockNumber), //nolint:gosec // G115: DB-side invariant keeps block_number non-negative.
		Subject:    subject,
		Values:     values,
		DetectedAt: m.DetectedAt,
	}

	trusted := make([]source.SourceID, len(m.TrustedSources))
	for i, s := range m.TrustedSources {
		trusted[i] = source.SourceID(s)
	}
	j := diff.Judgement{
		Severity:       diff.Severity(m.Severity),
		TrustedSources: trusted,
		Reasoning:      m.Reasoning,
	}

	rec := application.DiffRecord{
		ID:          application.DiffID(fmt.Sprintf("%d", m.ID)),
		Discrepancy: d,
		Judgement:   j,
		Resolved:    m.Resolved,
		ResolvedAt:  m.ResolvedAt,
	}
	if m.Tier != nil {
		// Tier column is SMALLINT (int16) on the DB side; source.Tier
		// is uint8 with values 0..3. Clamp defensively so a stray
		// negative or out-of-range value surfaces as TierUnknown
		// rather than wrapping.
		t := *m.Tier
		if t >= 0 && t <= 255 {
			rec.Tier = source.Tier(t) //nolint:gosec // G115: bound-checked above.
		}
	}
	if m.AnchorBlock != nil {
		rec.AnchorBlock = chain.BlockNumber(*m.AnchorBlock) //nolint:gosec // G115: DB-side invariant.
	}
	if m.SamplingSeed != nil {
		rec.SamplingSeed = m.SamplingSeed
	}
	return rec, nil
}
