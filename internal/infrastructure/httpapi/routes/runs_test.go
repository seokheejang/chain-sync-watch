package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// fakeDispatcher implements application.JobDispatcher with recorded
// calls; no real asynq round-trip.
type fakeDispatcher struct {
	enqueued []verification.RunID
}

func (f *fakeDispatcher) EnqueueRunExecution(_ context.Context, id verification.RunID) error {
	f.enqueued = append(f.enqueued, id)
	return nil
}

func (f *fakeDispatcher) ScheduleRecurring(_ context.Context, _ verification.Schedule, _ application.SchedulePayload) (application.JobID, error) {
	return "job-1", nil
}

func (f *fakeDispatcher) CancelScheduled(_ context.Context, _ application.JobID) error {
	return nil
}

// --- fixture ---------------------------------------------------------

type runsFixture struct {
	ts         *httptest.Server
	runs       *testsupport.FakeRunRepo
	dispatcher *fakeDispatcher
	clock      *testsupport.FakeClock
}

func newRunsFixture(t *testing.T) *runsFixture {
	t.Helper()
	runs := testsupport.NewFakeRunRepo()
	dispatcher := &fakeDispatcher{}
	clock := testsupport.NewFakeClock(time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC))

	schedule := &application.ScheduleRun{Runs: runs, Dispatcher: dispatcher, Clock: clock}
	query := application.QueryRuns{Runs: runs}
	cancel := &application.CancelRun{Runs: runs, Clock: clock}

	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Runs: routes.RunsDeps{Schedule: schedule, Query: query, Cancel: cancel},
	})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	return &runsFixture{ts: ts, runs: runs, dispatcher: dispatcher, clock: clock}
}

func (f *runsFixture) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(f.ts.URL+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

func (f *runsFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(f.ts.URL + path)
	require.NoError(t, err)
	return resp
}

func validCreateBody() map[string]any {
	return map[string]any{
		"chain_id": 10,
		"metrics":  []string{"block.hash"},
		"sampling": map[string]any{
			"kind":       "fixed_list",
			"fixed_list": map[string]any{"numbers": []uint64{100, 101}},
		},
		"trigger": map[string]any{
			"kind": "manual",
			"user": "alice",
		},
	}
}

// --- POST /runs -----------------------------------------------------

func TestCreateRun_Manual_Success(t *testing.T) {
	f := newRunsFixture(t)
	resp := f.post(t, "/runs", validCreateBody())
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		RunID string  `json:"run_id"`
		JobID *string `json:"job_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.RunID)
	require.Nil(t, out.JobID, "manual triggers have no JobID")

	require.Len(t, f.dispatcher.enqueued, 1)
	require.Equal(t, verification.RunID(out.RunID), f.dispatcher.enqueued[0])
}

func TestCreateRun_UnknownMetric_Returns400(t *testing.T) {
	f := newRunsFixture(t)
	body := validCreateBody()
	body["metrics"] = []string{"block.totally_made_up"}
	resp := f.post(t, "/runs", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateRun_UnknownSamplingKind_Returns422(t *testing.T) {
	// huma's schema enum validator rejects unknown kinds before the
	// DTO mapper runs. 422 Unprocessable Entity is the OpenAPI-
	// conventional status for schema-valid JSON that fails value
	// constraints.
	f := newRunsFixture(t)
	body := validCreateBody()
	body["sampling"] = map[string]any{"kind": "nope"}
	resp := f.post(t, "/runs", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

// --- GET /runs/{id} -------------------------------------------------

func TestGetRun_Returns200WithView(t *testing.T) {
	f := newRunsFixture(t)
	resp := f.post(t, "/runs", validCreateBody())
	defer resp.Body.Close()
	var created struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))

	getResp := f.get(t, "/runs/"+created.RunID)
	defer getResp.Body.Close()
	require.Equal(t, http.StatusOK, getResp.StatusCode)

	var view struct {
		ID           string   `json:"id"`
		ChainID      uint64   `json:"chain_id"`
		Status       string   `json:"status"`
		StrategyKind string   `json:"strategy_kind"`
		TriggerKind  string   `json:"trigger_kind"`
		Metrics      []string `json:"metrics"`
	}
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&view))
	require.Equal(t, created.RunID, view.ID)
	require.Equal(t, uint64(10), view.ChainID)
	require.Equal(t, "pending", view.Status)
	require.Equal(t, verification.KindFixedList, view.StrategyKind)
	require.Equal(t, verification.TriggerKindManual, view.TriggerKind)
	require.Equal(t, []string{"block.hash"}, view.Metrics)
}

func TestGetRun_Missing_Returns404(t *testing.T) {
	f := newRunsFixture(t)
	resp := f.get(t, "/runs/no-such-run")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- GET /runs ------------------------------------------------------

func TestListRuns_Empty(t *testing.T) {
	f := newRunsFixture(t)
	resp := f.get(t, "/runs")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 0, body.Total)
	require.Empty(t, body.Items)
}

func TestListRuns_ReturnsSeededRuns(t *testing.T) {
	f := newRunsFixture(t)
	// Seed three runs via the create endpoint.
	for i := 0; i < 3; i++ {
		resp := f.post(t, "/runs", validCreateBody())
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	resp := f.get(t, "/runs")
	defer resp.Body.Close()
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 3, body.Total)
	require.Len(t, body.Items, 3)
}

// --- POST /runs/{id}/cancel -----------------------------------------

func TestCancelRun_Returns204(t *testing.T) {
	f := newRunsFixture(t)
	createResp := f.post(t, "/runs", validCreateBody())
	var created struct {
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	createResp.Body.Close()
	require.NotEmpty(t, created.RunID)

	cancelResp := f.post(t, "/runs/"+created.RunID+"/cancel", nil)
	defer cancelResp.Body.Close()
	require.Equal(t, http.StatusNoContent, cancelResp.StatusCode)

	r, err := f.runs.FindByID(context.Background(), verification.RunID(created.RunID))
	require.NoError(t, err)
	require.Equal(t, verification.StatusCancelled, r.Status())
}

func TestCancelRun_Missing_Returns404(t *testing.T) {
	f := newRunsFixture(t)
	resp := f.post(t, "/runs/missing/cancel", nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCancelRun_Terminal_Returns400(t *testing.T) {
	f := newRunsFixture(t)
	// Seed a run then drive it to completed via the aggregate.
	r, err := verification.NewRun(
		"r-terminal",
		chain.OptimismMainnet,
		verification.FixedList{Numbers: []chain.BlockNumber{100}},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		time.Now(),
	)
	require.NoError(t, err)
	require.NoError(t, r.Start(time.Now()))
	require.NoError(t, r.Complete(time.Now()))
	require.NoError(t, f.runs.Save(context.Background(), r))

	resp := f.post(t, "/runs/r-terminal/cancel", nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "Run.Cancel rejection maps to 400 via ErrInvalidRun")
}
