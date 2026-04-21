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

type scheduleFixture struct {
	runs     *testsupport.FakeRunRepo
	disp     *testsupport.FakeDispatcher
	clock    *testsupport.FakeClock
	useCase  application.ScheduleRun
	baseTime time.Time
}

func newScheduleFixture() *scheduleFixture {
	baseTime := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	runs := testsupport.NewFakeRunRepo()
	disp := testsupport.NewFakeDispatcher()
	clock := testsupport.NewFakeClock(baseTime)
	return &scheduleFixture{
		runs:  runs,
		disp:  disp,
		clock: clock,
		useCase: application.ScheduleRun{
			Runs:       runs,
			Dispatcher: disp,
			Clock:      clock,
		},
		baseTime: baseTime,
	}
}

func validInput() application.ScheduleRunInput {
	return application.ScheduleRunInput{
		ChainID:  chain.OptimismMainnet,
		Strategy: verification.LatestN{N: 5},
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Trigger:  verification.ManualTrigger{User: "alice"},
	}
}

func TestScheduleRun_ManualTriggerEnqueues(t *testing.T) {
	f := newScheduleFixture()
	res, err := f.useCase.Execute(context.Background(), validInput())
	require.NoError(t, err)
	require.NotEmpty(t, res.RunID)
	require.Nil(t, res.JobID)

	r, err := f.runs.FindByID(context.Background(), res.RunID)
	require.NoError(t, err)
	require.Equal(t, verification.StatusPending, r.Status())
	require.Equal(t, f.baseTime, r.CreatedAt())

	enq := f.disp.Enqueued()
	require.Len(t, enq, 1)
	require.Equal(t, res.RunID, enq[0].RunID)

	require.Empty(t, f.disp.Scheduled(), "manual trigger must not register a recurring job")
}

func TestScheduleRun_ScheduledTriggerCreatesRecurring(t *testing.T) {
	f := newScheduleFixture()
	schedule, err := verification.NewSchedule("0 */6 * * *", "UTC")
	require.NoError(t, err)

	in := validInput()
	in.Trigger = verification.ScheduledTrigger{CronExpr: "0 */6 * * *"}
	in.Schedule = schedule

	res, err := f.useCase.Execute(context.Background(), in)
	require.NoError(t, err)
	require.NotNil(t, res.JobID)

	scheduled := f.disp.Scheduled()
	require.Len(t, scheduled, 1)
	require.Equal(t, *res.JobID, scheduled[0].JobID)
	require.Equal(t, chain.OptimismMainnet, scheduled[0].Payload.ChainID)
	require.Equal(t, schedule, scheduled[0].Schedule)

	require.Empty(t, f.disp.Enqueued(), "scheduled trigger must not immediately enqueue")
}

func TestScheduleRun_ScheduledTriggerRequiresSchedule(t *testing.T) {
	f := newScheduleFixture()
	in := validInput()
	in.Trigger = verification.ScheduledTrigger{CronExpr: "* * * * *"}
	// Schedule deliberately left zero.

	_, err := f.useCase.Execute(context.Background(), in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires a Schedule")
	// Run must not have been persisted.
	require.Zero(t, f.runs.Count())
}

func TestScheduleRun_RealtimeTriggerPersistsWithoutDispatch(t *testing.T) {
	f := newScheduleFixture()
	in := validInput()
	in.Trigger = verification.RealtimeTrigger{BlockNumber: 42}

	res, err := f.useCase.Execute(context.Background(), in)
	require.NoError(t, err)
	require.NotEmpty(t, res.RunID)
	require.Nil(t, res.JobID)

	require.Equal(t, 1, f.runs.Count())
	require.Empty(t, f.disp.Enqueued())
	require.Empty(t, f.disp.Scheduled())
}

func TestScheduleRun_ValidationFailuresAreAtomic(t *testing.T) {
	// No Save, no Enqueue on validation failure (empty metrics).
	f := newScheduleFixture()
	in := validInput()
	in.Metrics = nil

	_, err := f.useCase.Execute(context.Background(), in)
	require.Error(t, err)
	require.ErrorIs(t, err, application.ErrInvalidRun)

	require.Zero(t, f.runs.Count())
	require.Empty(t, f.disp.Enqueued())
}

func TestScheduleRun_DuplicateRunIDIsRejected(t *testing.T) {
	f := newScheduleFixture()
	in := validInput()
	in.ID = "explicit-rid"

	_, err := f.useCase.Execute(context.Background(), in)
	require.NoError(t, err)

	// Second call with the same explicit id must fail with
	// ErrDuplicateRun and must NOT enqueue again.
	_, err = f.useCase.Execute(context.Background(), in)
	require.ErrorIs(t, err, application.ErrDuplicateRun)
	require.Len(t, f.disp.Enqueued(), 1)
}

func TestScheduleRun_UsesProvidedIDWhenSet(t *testing.T) {
	f := newScheduleFixture()
	in := validInput()
	in.ID = "caller-rid"

	res, err := f.useCase.Execute(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, verification.RunID("caller-rid"), res.RunID)
}

func TestScheduleRun_GeneratesIDWhenEmpty(t *testing.T) {
	f := newScheduleFixture()
	res, err := f.useCase.Execute(context.Background(), validInput())
	require.NoError(t, err)
	// NewRunID returns 32 hex chars.
	require.Len(t, string(res.RunID), 32)
}

func TestScheduleRun_DispatcherFailureSurfaces(t *testing.T) {
	f := newScheduleFixture()
	f.disp.EnqueueErr = errors.New("queue down")

	_, err := f.useCase.Execute(context.Background(), validInput())
	require.Error(t, err)
	require.Contains(t, err.Error(), "queue down")

	// The Run is still persisted in pending state so operators
	// can retry ExecuteRun later. This matches how real queue
	// systems behave on transient outages.
	require.Equal(t, 1, f.runs.Count())
}
