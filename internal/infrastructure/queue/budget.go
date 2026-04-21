package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// BudgetPolicy caps the number of reservations a Source may make
// inside a single fixed time window. Limit and Window must both be
// positive; a policy with either zero field is treated as "no
// policy configured" by RedisBudget (the Source becomes unlimited).
//
// Windows pin to Unix-epoch boundaries: the key for a 1h window
// bucket always resets on the hour, never at the moment a Source
// first called Reserve. This makes the behaviour predictable across
// worker restarts.
type BudgetPolicy struct {
	Window time.Duration
	Limit  int
}

// IsZero reports whether the policy is uninitialised. Call sites
// use this to distinguish "no policy configured, allow everything"
// from "policy with zero limit, reject everything".
func (p BudgetPolicy) IsZero() bool {
	return p.Window == 0 && p.Limit == 0
}

// BudgetConfig maps each SourceID to its policy. A missing entry
// means "no per-source cap" — Reserve short-circuits and returns
// nil without touching Redis.
type BudgetConfig struct {
	Policies map[source.SourceID]BudgetPolicy
}

// policyFor returns the configured policy plus ok=false when the
// Source is unbounded.
func (c BudgetConfig) policyFor(sid source.SourceID) (BudgetPolicy, bool) {
	p, ok := c.Policies[sid]
	if !ok || p.IsZero() {
		return BudgetPolicy{}, false
	}
	return p, true
}

// RedisBudget implements application.RateLimitBudget against Redis
// using a fixed-window counter. Each (source, window) bucket is a
// single integer key with a TTL slightly longer than the window
// size, so buckets expire naturally and do not leak.
//
// Reserve and Refund are atomic at the Redis level: both operations
// run as Lua scripts so the check-then-increment and
// decrement-with-floor sequences are not interleaved by concurrent
// workers.
type RedisBudget struct {
	client    redis.UniversalClient
	config    BudgetConfig
	keyPrefix string
	reserveFn *redis.Script
	refundFn  *redis.Script
}

// NewRedisBudget constructs a RedisBudget around an already-opened
// Redis client. Ownership of the client stays with the caller —
// RedisBudget does not close it.
func NewRedisBudget(client redis.UniversalClient, cfg BudgetConfig) *RedisBudget {
	return &RedisBudget{
		client:    client,
		config:    cfg,
		keyPrefix: "csw:budget:",
		reserveFn: redis.NewScript(reserveScript),
		refundFn:  redis.NewScript(refundScript),
	}
}

// WithKeyPrefix overrides the default key prefix. Primarily useful
// for multi-tenant deployments sharing one Redis.
func (b *RedisBudget) WithKeyPrefix(prefix string) *RedisBudget {
	b.keyPrefix = prefix
	return b
}

// Reserve checks whether sourceID has n units of budget remaining
// in its current window and atomically deducts them when it does.
// Returns application.ErrBudgetExhausted when the window's limit
// would be exceeded. Sources without a configured policy return
// nil without touching Redis.
func (b *RedisBudget) Reserve(ctx context.Context, sourceID source.SourceID, n int) error {
	if n <= 0 {
		return nil
	}
	if sourceID == "" {
		return errors.New("budget reserve: empty source id")
	}
	policy, ok := b.config.policyFor(sourceID)
	if !ok {
		return nil
	}

	key := b.currentKey(sourceID, policy.Window)
	ttlMillis := policy.Window.Milliseconds() + 1000 // 1s grace so clock skew doesn't zero the counter early

	result, err := b.reserveFn.Run(
		ctx, b.client,
		[]string{key},
		n, policy.Limit, ttlMillis,
	).Int()
	if err != nil {
		return fmt.Errorf("budget reserve: %w", err)
	}
	if result == -1 {
		return application.ErrBudgetExhausted
	}
	return nil
}

// Refund returns n units to sourceID's current window, floored at
// zero. Refunds against an expired window are no-ops (the key is
// gone) — this matches the typical "remote call errored before
// actually hitting the quota" case where the window may already
// have rolled over.
func (b *RedisBudget) Refund(ctx context.Context, sourceID source.SourceID, n int) error {
	if n <= 0 {
		return nil
	}
	if sourceID == "" {
		return errors.New("budget refund: empty source id")
	}
	policy, ok := b.config.policyFor(sourceID)
	if !ok {
		return nil
	}

	key := b.currentKey(sourceID, policy.Window)
	if _, err := b.refundFn.Run(ctx, b.client, []string{key}, n).Int(); err != nil {
		return fmt.Errorf("budget refund: %w", err)
	}
	return nil
}

// Remaining is a test helper that reports the current usage against
// a source's policy. Not part of the RateLimitBudget port because
// the production path should not branch on advisory reads — use
// Reserve and let it decide.
func (b *RedisBudget) Remaining(ctx context.Context, sourceID source.SourceID) (int, error) {
	policy, ok := b.config.policyFor(sourceID)
	if !ok {
		return -1, nil
	}
	key := b.currentKey(sourceID, policy.Window)
	val, err := b.client.Get(ctx, key).Int()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return policy.Limit, nil
		}
		return 0, err
	}
	return policy.Limit - val, nil
}

func (b *RedisBudget) currentKey(sid source.SourceID, window time.Duration) string {
	windowStart := time.Now().UTC().Truncate(window).Unix()
	return fmt.Sprintf("%s%s:%d", b.keyPrefix, sid, windowStart)
}

// reserveScript atomically checks `GET key + n > limit` and either
// returns -1 (exhausted) or INCRBY + PEXPIRE. TTL is only set when
// the key is first created; re-INCRs keep the original expiry so a
// burst near the window boundary cannot stretch the window.
const reserveScript = `
local current = tonumber(redis.call('GET', KEYS[1]) or 0)
local n = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
if current + n > limit then
    return -1
end
local newval = redis.call('INCRBY', KEYS[1], n)
if current == 0 then
    redis.call('PEXPIRE', KEYS[1], ttl)
end
return newval
`

// refundScript atomically decrements but floors at zero. When the
// counter would reach zero the key is deleted so the next window's
// first Reserve gets a fresh TTL.
const refundScript = `
local current = tonumber(redis.call('GET', KEYS[1]) or 0)
local n = tonumber(ARGV[1])
local newval = current - n
if newval <= 0 then
    redis.call('DEL', KEYS[1])
    return 0
end
redis.call('SET', KEYS[1], newval, 'KEEPTTL')
return newval
`

// Compile-time assertion.
var _ application.RateLimitBudget = (*RedisBudget)(nil)
