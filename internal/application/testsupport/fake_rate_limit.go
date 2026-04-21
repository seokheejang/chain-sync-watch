package testsupport

import (
	"context"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// FakeRateLimitBudget is an in-memory RateLimitBudget. Each Source
// has its own budget counter; Reserve returns ErrBudgetExhausted
// when the counter would drop below zero.
type FakeRateLimitBudget struct {
	mu      sync.Mutex
	budgets map[source.SourceID]int
	usage   map[source.SourceID]int
}

// NewFakeRateLimitBudget seeds each SourceID with its starting
// budget. An unseeded Source has budget 0 and will error on first
// Reserve.
func NewFakeRateLimitBudget(initial map[source.SourceID]int) *FakeRateLimitBudget {
	budgets := map[source.SourceID]int{}
	for k, v := range initial {
		budgets[k] = v
	}
	return &FakeRateLimitBudget{
		budgets: budgets,
		usage:   map[source.SourceID]int{},
	}
}

// Reserve deducts n units from sourceID's budget.
func (f *FakeRateLimitBudget) Reserve(_ context.Context, sourceID source.SourceID, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.budgets[sourceID]-f.usage[sourceID] < n {
		return application.ErrBudgetExhausted
	}
	f.usage[sourceID] += n
	return nil
}

// Refund returns n units to sourceID's budget.
func (f *FakeRateLimitBudget) Refund(_ context.Context, sourceID source.SourceID, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage[sourceID] -= n
	if f.usage[sourceID] < 0 {
		f.usage[sourceID] = 0
	}
	return nil
}

// Remaining returns sourceID's remaining budget. Test helper.
func (f *FakeRateLimitBudget) Remaining(sourceID source.SourceID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.budgets[sourceID] - f.usage[sourceID]
}
