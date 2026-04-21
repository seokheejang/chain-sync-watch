package testsupport

import (
	"sync"
	"time"
)

// FakeClock is a deterministic, advance-on-demand Clock.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a clock frozen at t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{now: t} }

// Now returns the current frozen time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// Set replaces the current time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}
