package probe_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func validTarget() probe.ProbeTarget {
	return probe.ProbeTarget{
		Kind:   probe.TargetHTTP,
		URL:    "https://api.example.com/health",
		Method: "GET",
	}
}

func validSchedule() probe.ProbeSchedule {
	return probe.ProbeSchedule{Interval: 30 * time.Second}
}

func validThresholds() []probe.Threshold {
	return []probe.Threshold{
		{Metric: probe.ThresholdLatencyP95Ms, Value: 2000, WindowSec: 300},
		{Metric: probe.ThresholdErrorRatePct, Value: 1, WindowSec: 300},
	}
}

func TestTargetKind_Valid(t *testing.T) {
	require.True(t, probe.TargetHTTP.Valid())
	require.True(t, probe.TargetRPC.Valid())
	require.True(t, probe.TargetGraphQL.Valid())
	require.False(t, probe.TargetKind("bogus").Valid())
}

func TestProbeTarget_Validate(t *testing.T) {
	t.Run("valid http", func(t *testing.T) {
		require.NoError(t, validTarget().Validate())
	})
	t.Run("invalid kind", func(t *testing.T) {
		tgt := validTarget()
		tgt.Kind = "graphql_v2"
		require.Error(t, tgt.Validate())
	})
	t.Run("empty url", func(t *testing.T) {
		tgt := validTarget()
		tgt.URL = ""
		require.Error(t, tgt.Validate())
	})
	t.Run("malformed url", func(t *testing.T) {
		tgt := validTarget()
		tgt.URL = "ht!tp://bad"
		require.Error(t, tgt.Validate())
	})
	t.Run("non-http scheme", func(t *testing.T) {
		tgt := validTarget()
		tgt.URL = "ftp://example.com/file"
		require.Error(t, tgt.Validate())
	})
	t.Run("missing host", func(t *testing.T) {
		tgt := validTarget()
		tgt.URL = "https://"
		require.Error(t, tgt.Validate())
	})
	t.Run("http kind requires method", func(t *testing.T) {
		tgt := validTarget()
		tgt.Method = ""
		require.Error(t, tgt.Validate())
	})
	t.Run("rpc kind requires method name", func(t *testing.T) {
		tgt := probe.ProbeTarget{
			Kind:   probe.TargetRPC,
			URL:    "https://rpc.example.com",
			Method: "",
		}
		require.Error(t, tgt.Validate())
	})
}

func TestProbeSchedule_Validate(t *testing.T) {
	t.Run("interval ok", func(t *testing.T) {
		require.NoError(t, probe.ProbeSchedule{Interval: time.Minute}.Validate())
	})
	t.Run("cron ok", func(t *testing.T) {
		require.NoError(t, probe.ProbeSchedule{CronExpr: "*/5 * * * *"}.Validate())
	})
	t.Run("both set is rejected", func(t *testing.T) {
		require.Error(t, probe.ProbeSchedule{
			CronExpr: "*/5 * * * *",
			Interval: time.Minute,
		}.Validate())
	})
	t.Run("neither set is rejected", func(t *testing.T) {
		require.Error(t, probe.ProbeSchedule{}.Validate())
	})
	t.Run("interval below 1s", func(t *testing.T) {
		require.Error(t, probe.ProbeSchedule{Interval: 500 * time.Millisecond}.Validate())
	})
}

func TestThreshold_Validate(t *testing.T) {
	t.Run("p95 ok", func(t *testing.T) {
		require.NoError(t, probe.Threshold{
			Metric:    probe.ThresholdLatencyP95Ms,
			Value:     2000,
			WindowSec: 300,
		}.Validate())
	})
	t.Run("latency value zero rejected", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdLatencyP95Ms, Value: 0, WindowSec: 60,
		}.Validate())
	})
	t.Run("latency missing window", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdLatencyP95Ms, Value: 1000,
		}.Validate())
	})
	t.Run("error_rate over 100", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdErrorRatePct, Value: 150, WindowSec: 60,
		}.Validate())
	})
	t.Run("consecutive failures non-integer rejected", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdConsecutiveFailures, Value: 2.5,
		}.Validate())
	})
	t.Run("consecutive failures zero rejected", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdConsecutiveFailures, Value: 0,
		}.Validate())
	})
	t.Run("invalid metric", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdMetric("rps"), Value: 100, WindowSec: 60,
		}.Validate())
	})
	t.Run("negative value", func(t *testing.T) {
		require.Error(t, probe.Threshold{
			Metric: probe.ThresholdLatencyP95Ms, Value: -1, WindowSec: 60,
		}.Validate())
	})
}

func TestNewProbe_Success(t *testing.T) {
	p, err := probe.NewProbe("indexer-health", validTarget(), validSchedule(), validThresholds(), true)
	require.NoError(t, err)
	require.Equal(t, probe.ProbeID("indexer-health"), p.ID())
	require.True(t, p.Enabled())
	require.Equal(t, validTarget().URL, p.Target().URL)
	require.Len(t, p.Thresholds(), 2)
}

func TestNewProbe_RejectsInvariantBreaks(t *testing.T) {
	t.Run("empty id", func(t *testing.T) {
		_, err := probe.NewProbe("", validTarget(), validSchedule(), validThresholds(), true)
		require.Error(t, err)
	})
	t.Run("invalid target", func(t *testing.T) {
		bad := validTarget()
		bad.URL = ""
		_, err := probe.NewProbe("p", bad, validSchedule(), validThresholds(), true)
		require.Error(t, err)
	})
	t.Run("invalid schedule", func(t *testing.T) {
		_, err := probe.NewProbe("p", validTarget(), probe.ProbeSchedule{}, validThresholds(), true)
		require.Error(t, err)
	})
	t.Run("no thresholds", func(t *testing.T) {
		_, err := probe.NewProbe("p", validTarget(), validSchedule(), nil, true)
		require.Error(t, err)
	})
	t.Run("duplicate threshold metric", func(t *testing.T) {
		dup := []probe.Threshold{
			{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 60},
			{Metric: probe.ThresholdLatencyP95Ms, Value: 2000, WindowSec: 60},
		}
		_, err := probe.NewProbe("p", validTarget(), validSchedule(), dup, true)
		require.Error(t, err)
	})
}

func TestProbe_DefensiveCopy(t *testing.T) {
	headers := map[string]string{"X-Token": "secret"}
	body := []byte("payload")
	tgt := probe.ProbeTarget{
		Kind:    probe.TargetHTTP,
		URL:     "https://api.example.com/x",
		Method:  "POST",
		Headers: headers,
		Body:    body,
	}
	p, err := probe.NewProbe("p1", tgt, validSchedule(), validThresholds(), true)
	require.NoError(t, err)

	// Mutating caller-side maps/slices must not reach the aggregate.
	headers["X-Token"] = "tampered"
	body[0] = 'X'

	got := p.Target()
	require.Equal(t, "secret", got.Headers["X-Token"])
	require.Equal(t, byte('p'), got.Body[0])

	// Mutating the returned copies must not reach the aggregate either.
	got.Headers["X-Token"] = "tampered2"
	got.Body[0] = 'Z'

	got2 := p.Target()
	require.Equal(t, "secret", got2.Headers["X-Token"])
	require.Equal(t, byte('p'), got2.Body[0])
}

func TestRehydrate_SkipsValidation(t *testing.T) {
	// A persisted record with an empty URL would fail NewProbe but
	// must round-trip through Rehydrate — the mapper alone owns
	// storage consistency.
	bad := probe.ProbeTarget{Kind: probe.TargetHTTP, URL: "", Method: "GET"}
	p := probe.Rehydrate("legacy", bad, validSchedule(), validThresholds(), false)
	require.Equal(t, probe.ProbeID("legacy"), p.ID())
	require.False(t, p.Enabled())
	require.Equal(t, "", p.Target().URL)
}
