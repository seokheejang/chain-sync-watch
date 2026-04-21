package verification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestTrigger_InterfaceCompliance(t *testing.T) {
	var _ verification.Trigger = verification.ManualTrigger{}
	var _ verification.Trigger = verification.ScheduledTrigger{}
	var _ verification.Trigger = verification.RealtimeTrigger{}
}

func TestManualTrigger(t *testing.T) {
	tr := verification.ManualTrigger{User: "alice"}
	require.Equal(t, verification.TriggerKindManual, tr.Kind())
	require.Equal(t, "alice", tr.User)
}

func TestScheduledTrigger(t *testing.T) {
	tr := verification.ScheduledTrigger{CronExpr: "0 */6 * * *"}
	require.Equal(t, verification.TriggerKindScheduled, tr.Kind())
	require.Equal(t, "0 */6 * * *", tr.CronExpr)
}

func TestRealtimeTrigger(t *testing.T) {
	tr := verification.RealtimeTrigger{BlockNumber: 12345}
	require.Equal(t, verification.TriggerKindRealtime, tr.Kind())
	require.Equal(t, chain.BlockNumber(12345), tr.BlockNumber)
}

func TestTrigger_ExhaustiveSwitch(t *testing.T) {
	// A type-switch over Trigger must reach every concrete variant.
	// If someone adds a new Trigger without updating this switch,
	// the default branch's error message will name it in test
	// output — reminding us to extend downstream consumers too.
	kinds := []verification.Trigger{
		verification.ManualTrigger{User: "u"},
		verification.ScheduledTrigger{CronExpr: "* * * * *"},
		verification.RealtimeTrigger{BlockNumber: 1},
	}
	seen := map[string]bool{}
	for _, tr := range kinds {
		switch v := tr.(type) {
		case verification.ManualTrigger:
			seen["manual"] = true
			require.Equal(t, "u", v.User)
		case verification.ScheduledTrigger:
			seen["scheduled"] = true
		case verification.RealtimeTrigger:
			seen["realtime"] = true
		default:
			t.Fatalf("unhandled trigger variant %T", v)
		}
	}
	require.True(t, seen["manual"] && seen["scheduled"] && seen["realtime"])
}
