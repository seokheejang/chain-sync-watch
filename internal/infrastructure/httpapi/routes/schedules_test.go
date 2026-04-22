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

// scheduleDispatcher captures schedule and cancel calls on top of
// fakeDispatcher (defined in runs_test.go). Implements the narrow
// ScheduleCanceller plus JobDispatcher — the fixture threads it
// through ScheduleRun so the create path stays end-to-end.
type scheduleDispatcher struct {
	fakeDispatcher
	schedules []application.JobID
	cancelled []application.JobID
	records   map[application.JobID]application.ScheduleRecord
}

func (s *scheduleDispatcher) ScheduleRecurring(_ context.Context, sched verification.Schedule, payload application.SchedulePayload) (application.JobID, error) {
	id := application.JobID("job-" + string(rune('a'+len(s.schedules))))
	s.schedules = append(s.schedules, id)
	if s.records == nil {
		s.records = map[application.JobID]application.ScheduleRecord{}
	}
	s.records[id] = application.ScheduleRecord{
		JobID:        id,
		ChainID:      payload.ChainID,
		Schedule:     sched,
		Strategy:     payload.Strategy,
		Metrics:      payload.Metrics,
		AddressPlans: payload.AddressPlans,
		CreatedAt:    time.Now(),
		Active:       true,
	}
	return id, nil
}

func (s *scheduleDispatcher) CancelScheduled(_ context.Context, id application.JobID) error {
	s.cancelled = append(s.cancelled, id)
	if rec, ok := s.records[id]; ok {
		rec.Active = false
		s.records[id] = rec
	}
	return nil
}

// scheduleRepo is a minimal in-memory impl of ScheduleRepository
// backed by the dispatcher's records map, so list/read calls see
// the same data the dispatcher produced.
type scheduleRepo struct {
	dispatcher *scheduleDispatcher
}

func (r *scheduleRepo) Save(_ context.Context, _ application.ScheduleRecord) error {
	return nil
}

func (r *scheduleRepo) Deactivate(_ context.Context, _ application.JobID) error {
	return nil
}

func (r *scheduleRepo) ListActive(_ context.Context) ([]application.ScheduleRecord, error) {
	out := []application.ScheduleRecord{}
	for _, rec := range r.dispatcher.records {
		if rec.Active {
			out = append(out, rec)
		}
	}
	return out, nil
}

type schedulesFixture struct {
	ts         *httptest.Server
	dispatcher *scheduleDispatcher
	runs       *testsupport.FakeRunRepo
}

func newSchedulesFixture(t *testing.T) *schedulesFixture {
	t.Helper()
	runs := testsupport.NewFakeRunRepo()
	dispatcher := &scheduleDispatcher{}
	repo := &scheduleRepo{dispatcher: dispatcher}
	clock := testsupport.NewFakeClock(time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC))
	schedule := &application.ScheduleRun{Runs: runs, Dispatcher: dispatcher, Clock: clock}
	query := application.QuerySchedules{Schedules: repo}

	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Schedules: routes.SchedulesDeps{
			Schedule:   schedule,
			Query:      query,
			Dispatcher: dispatcher,
		},
	})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return &schedulesFixture{ts: ts, dispatcher: dispatcher, runs: runs}
}

func (f *schedulesFixture) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(f.ts.URL+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

func (f *schedulesFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(f.ts.URL + path)
	require.NoError(t, err)
	return resp
}

func (f *schedulesFixture) del(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, f.ts.URL+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func validCreateScheduleBody() map[string]any {
	return map[string]any{
		"chain_id": 10,
		"metrics":  []string{"block.hash"},
		"sampling": map[string]any{
			"kind":     "latest_n",
			"latest_n": map[string]any{"n": 10},
		},
		"schedule": map[string]any{
			"cron_expr": "*/5 * * * *",
			"timezone":  "UTC",
		},
	}
}

// --- POST /schedules --------------------------------------------------

func TestCreateSchedule_Success(t *testing.T) {
	f := newSchedulesFixture(t)
	resp := f.post(t, "/schedules", validCreateScheduleBody())
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var body struct {
		JobID string `json:"job_id"`
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.JobID)
	require.NotEmpty(t, body.RunID)

	require.Len(t, f.dispatcher.schedules, 1)
	require.Equal(t, application.JobID(body.JobID), f.dispatcher.schedules[0])
}

func TestCreateSchedule_MissingCron_Returns422(t *testing.T) {
	// huma schema requires non-zero strings on non-pointer fields;
	// missing cron_expr is rejected at the schema layer (422) before
	// ToDomain runs.
	f := newSchedulesFixture(t)
	body := validCreateScheduleBody()
	body["schedule"] = map[string]any{}
	resp := f.post(t, "/schedules", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateSchedule_InvalidCron_Returns400(t *testing.T) {
	f := newSchedulesFixture(t)
	body := validCreateScheduleBody()
	body["schedule"] = map[string]any{"cron_expr": "not a cron"}
	resp := f.post(t, "/schedules", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- GET /schedules ---------------------------------------------------

func TestListSchedules_ReflectsCreated(t *testing.T) {
	f := newSchedulesFixture(t)
	resp := f.post(t, "/schedules", validCreateScheduleBody())
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	listResp := f.get(t, "/schedules")
	defer listResp.Body.Close()
	var body struct {
		Items []struct {
			JobID    string `json:"job_id"`
			CronExpr string `json:"cron_expr"`
			Active   bool   `json:"active"`
		} `json:"items"`
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&body))
	require.Equal(t, 1, body.Total)
	require.True(t, body.Items[0].Active)
	require.Equal(t, "*/5 * * * *", body.Items[0].CronExpr)
}

// --- DELETE /schedules/{id} ------------------------------------------

func TestCancelSchedule_Returns204AndDeactivates(t *testing.T) {
	f := newSchedulesFixture(t)
	createResp := f.post(t, "/schedules", validCreateScheduleBody())
	var created struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	createResp.Body.Close()

	delResp := f.del(t, "/schedules/"+created.JobID)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusNoContent, delResp.StatusCode)

	require.Equal(t, []application.JobID{application.JobID(created.JobID)}, f.dispatcher.cancelled)
	require.False(t, f.dispatcher.records[application.JobID(created.JobID)].Active)
}

// --- Basic sanity on Sampling mapping inherited from dto --------------

func TestCreateSchedule_UnknownMetric_Returns400(t *testing.T) {
	f := newSchedulesFixture(t)
	body := validCreateScheduleBody()
	body["metrics"] = []string{"totally.not.a.metric"}
	resp := f.post(t, "/schedules", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- No-op to ensure we can import chain without warning --------------
var _ = chain.OptimismMainnet
