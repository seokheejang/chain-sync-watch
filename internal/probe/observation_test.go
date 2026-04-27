package probe_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func TestErrorClass_StringRoundTrip(t *testing.T) {
	classes := []probe.ErrorClass{
		probe.ErrorNone, probe.ErrorNetwork, probe.ErrorTimeout,
		probe.ErrorHTTP4xx, probe.ErrorHTTP5xx, probe.ErrorProtocol,
	}
	for _, c := range classes {
		got, err := probe.ParseErrorClass(c.String())
		require.NoError(t, err, "round-trip %q", c)
		require.Equal(t, c, got)
	}
}

func TestParseErrorClass_Unknown(t *testing.T) {
	_, err := probe.ParseErrorClass("rpc_error")
	require.Error(t, err)
}

func TestErrorClass_IsError(t *testing.T) {
	require.False(t, probe.ErrorNone.IsError())
	require.True(t, probe.ErrorNetwork.IsError())
	require.True(t, probe.ErrorTimeout.IsError())
	require.True(t, probe.ErrorHTTP4xx.IsError())
	require.True(t, probe.ErrorHTTP5xx.IsError())
	require.True(t, probe.ErrorProtocol.IsError())
}

func TestNewObservation_Success(t *testing.T) {
	at := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	o, err := probe.NewObservation("p1", at, 123, 200, probe.ErrorNone, "")
	require.NoError(t, err)
	require.Equal(t, probe.ProbeID("p1"), o.ProbeID)
	require.Equal(t, at, o.At)
	require.EqualValues(t, 123, o.ElapsedMS)
	require.Equal(t, 200, o.StatusCode)
	require.Equal(t, probe.ErrorNone, o.ErrorClass)
}

func TestNewObservation_RejectsInvariantBreaks(t *testing.T) {
	at := time.Now()
	t.Run("empty probe id", func(t *testing.T) {
		_, err := probe.NewObservation("", at, 0, 200, probe.ErrorNone, "")
		require.Error(t, err)
	})
	t.Run("zero timestamp", func(t *testing.T) {
		_, err := probe.NewObservation("p1", time.Time{}, 0, 200, probe.ErrorNone, "")
		require.Error(t, err)
	})
	t.Run("negative elapsed", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, -1, 200, probe.ErrorNone, "")
		require.Error(t, err)
	})
	t.Run("status out of range", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 700, probe.ErrorNone, "")
		require.Error(t, err)
	})
	t.Run("http_5xx with 200 status", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 200, probe.ErrorHTTP5xx, "")
		require.Error(t, err)
	})
	t.Run("http_4xx with 5xx status", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 503, probe.ErrorHTTP4xx, "")
		require.Error(t, err)
	})
	t.Run("network error with non-zero status", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 200, probe.ErrorNetwork, "DNS lookup failed")
		require.Error(t, err)
	})
	t.Run("timeout with non-zero status", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 504, probe.ErrorTimeout, "deadline exceeded")
		require.Error(t, err)
	})
	t.Run("protocol error allowed with 200", func(t *testing.T) {
		_, err := probe.NewObservation("p1", at, 0, 200, probe.ErrorProtocol, "GraphQL error")
		require.NoError(t, err)
	})
}

func TestNewObservation_TruncatesErrMsg(t *testing.T) {
	at := time.Now()
	long := strings.Repeat("x", probe.ErrMsgMaxLen+200)
	o, err := probe.NewObservation("p1", at, 0, 0, probe.ErrorNetwork, long)
	require.NoError(t, err)
	require.Len(t, o.ErrorMsg, probe.ErrMsgMaxLen)
}
