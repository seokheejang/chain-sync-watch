package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func newCancelFixture(t *testing.T) (application.CancelRun, *testsupport.FakeRunRepo, time.Time) {
	t.Helper()
	base := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	runs := testsupport.NewFakeRunRepo()
	clock := testsupport.NewFakeClock(base)
	return application.CancelRun{Runs: runs, Clock: clock}, runs, base
}

func seedCancelRun(t *testing.T, runs *testsupport.FakeRunRepo, base time.Time) verification.RunID {
	t.Helper()
	r, err := verification.NewRun(
		"r1",
		chain.OptimismMainnet,
		verification.FixedList{Numbers: []chain.BlockNumber{100}},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		base,
	)
	require.NoError(t, err)
	require.NoError(t, runs.Save(context.Background(), r))
	return r.ID()
}

func TestCancelRun_PendingToCancelled(t *testing.T) {
	uc, runs, base := newCancelFixture(t)
	id := seedCancelRun(t, runs, base)

	require.NoError(t, uc.Execute(context.Background(), id))

	r, err := runs.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, verification.StatusCancelled, r.Status())
	require.NotNil(t, r.FinishedAt())
}

func TestCancelRun_NotFoundPropagates(t *testing.T) {
	uc, _, _ := newCancelFixture(t)
	err := uc.Execute(context.Background(), "missing")
	require.ErrorIs(t, err, application.ErrRunNotFound)
}

func TestCancelRun_TerminalRunReturnsInvalidRun(t *testing.T) {
	uc, runs, base := newCancelFixture(t)
	id := seedCancelRun(t, runs, base)
	r, _ := runs.FindByID(context.Background(), id)
	require.NoError(t, r.Start(base))
	require.NoError(t, r.Complete(base))
	require.NoError(t, runs.Save(context.Background(), r))

	err := uc.Execute(context.Background(), id)
	require.ErrorIs(t, err, application.ErrInvalidRun)
}

func TestCancelRun_SaveErrorPropagates(t *testing.T) {
	uc, runs, base := newCancelFixture(t)
	id := seedCancelRun(t, runs, base)

	runs.SaveErr = errors.New("db down")
	err := uc.Execute(context.Background(), id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "save")
}
