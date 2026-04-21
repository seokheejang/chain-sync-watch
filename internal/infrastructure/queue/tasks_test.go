package queue_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
)

func TestExecuteRunPayload_RoundTrip(t *testing.T) {
	p := queue.ExecuteRunPayload{RunID: "rid-42"}
	b, err := p.Marshal()
	require.NoError(t, err)

	got, err := queue.UnmarshalExecuteRunPayload(b)
	require.NoError(t, err)
	require.Equal(t, p, got)
}

func TestExecuteRunPayload_RejectsEmptyRunID(t *testing.T) {
	// Payload that deserialises but fails the semantic check — the
	// handler must refuse to run a task missing a RunID.
	_, err := queue.UnmarshalExecuteRunPayload([]byte(`{"run_id":""}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing run_id")
}

func TestExecuteRunPayload_RejectsMalformedJSON(t *testing.T) {
	_, err := queue.UnmarshalExecuteRunPayload([]byte(`not json`))
	require.Error(t, err)
}
