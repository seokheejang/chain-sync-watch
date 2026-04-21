package verification

import "github.com/seokheejang/chain-sync-watch/internal/chain"

// Trigger identifies what kicked off a Run. It is a sealed sum type:
// the isTrigger marker method is unexported, so only the three
// concrete triggers declared in this package can satisfy it. That
// closure lets callers exhaustively switch on the concrete type
// without an "unknown trigger" escape hatch.
//
// Kind() is exported for logging and persistence — every Trigger
// serialises to a stable string label so downstream systems can
// filter Runs by trigger source without importing this package.
type Trigger interface {
	isTrigger()
	Kind() string
}

// Trigger kind labels. Stable strings; change them and you break
// anyone filtering historical Runs by trigger source.
const (
	TriggerKindManual    = "manual"
	TriggerKindScheduled = "scheduled"
	TriggerKindRealtime  = "realtime"
)

// ManualTrigger records a human-initiated Run. User is an opaque
// identifier (username, email, API token subject) — the domain does
// not interpret it; it is only stored for audit.
type ManualTrigger struct {
	User string
}

// Kind returns TriggerKindManual.
func (ManualTrigger) Kind() string { return TriggerKindManual }
func (ManualTrigger) isTrigger()   {}

// ScheduledTrigger records a cron-driven Run. CronExpr is the raw
// schedule expression at dispatch time; the actual scheduler
// (Phase 7) is the one that decided this is the right moment to
// fire.
type ScheduledTrigger struct {
	CronExpr string
}

// Kind returns TriggerKindScheduled.
func (ScheduledTrigger) Kind() string { return TriggerKindScheduled }
func (ScheduledTrigger) isTrigger()   {}

// RealtimeTrigger records a Run fired by the block-tip watcher
// (post-MVP but defined now so the type is not a breaking change
// when the streaming path lands). BlockNumber is the height that
// just appeared.
type RealtimeTrigger struct {
	BlockNumber chain.BlockNumber
}

// Kind returns TriggerKindRealtime.
func (RealtimeTrigger) Kind() string { return TriggerKindRealtime }
func (RealtimeTrigger) isTrigger()   {}
