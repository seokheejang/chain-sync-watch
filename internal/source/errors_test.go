package source_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Sentinels must be distinguishable via errors.Is so callers can branch
// on transient vs permanent failures. Wrapping with fmt.Errorf("%w",
// sentinel) must preserve the identity.
func TestErrors_SentinelIdentity(t *testing.T) {
	sentinels := []error{
		source.ErrUnsupported,
		source.ErrRateLimited,
		source.ErrSourceUnavailable,
		source.ErrNotFound,
		source.ErrInvalidResponse,
	}
	for _, base := range sentinels {
		t.Run(base.Error(), func(t *testing.T) {
			wrapped := fmt.Errorf("adapter X: %w", base)
			require.ErrorIs(t, wrapped, base,
				"wrapped error must still match the sentinel via errors.Is")

			// Two sentinels must not alias each other.
			for _, other := range sentinels {
				if other == base {
					continue
				}
				require.False(t, errors.Is(wrapped, other),
					"%v should not match %v", base, other)
			}
		})
	}
}

func TestErrors_DistinctMessages(t *testing.T) {
	// Crude check that each sentinel carries a unique, namespaced message
	// so log greps ("source: rate limited") find what you expect.
	seen := make(map[string]bool)
	for _, e := range []error{
		source.ErrUnsupported,
		source.ErrRateLimited,
		source.ErrSourceUnavailable,
		source.ErrNotFound,
		source.ErrInvalidResponse,
	} {
		msg := e.Error()
		require.Contains(t, msg, "source:", "sentinel must be namespaced: %q", msg)
		require.False(t, seen[msg], "duplicate message: %q", msg)
		seen[msg] = true
	}
}
