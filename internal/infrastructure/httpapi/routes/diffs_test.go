package routes_test

import (
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
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

type diffsFixture struct {
	ts    *httptest.Server
	diffs *testsupport.FakeDiffRepo
}

func newDiffsFixture(t *testing.T) *diffsFixture {
	t.Helper()
	diffs := testsupport.NewFakeDiffRepo()
	query := application.QueryDiffs{Diffs: diffs}

	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Diffs: routes.DiffsDeps{Query: query},
	})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	return &diffsFixture{ts: ts, diffs: diffs}
}

func (f *diffsFixture) seed(t *testing.T, runID verification.RunID, metric verification.Metric) application.DiffID {
	t.Helper()
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	values := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc", FetchedAt: now},
		"blockscout": {Raw: "0xdef", FetchedAt: now},
	}
	d, err := diff.NewDiscrepancy(
		runID,
		metric,
		chain.BlockNumber(100),
		diff.Subject{Type: diff.SubjectBlock},
		values,
		now,
	)
	require.NoError(t, err)
	j := diff.Judgement{Severity: diff.SevCritical, TrustedSources: []source.SourceID{"rpc"}}
	id, err := f.diffs.Save(context.Background(), &d, j, application.SaveDiffMeta{
		Tier:        source.TierA,
		AnchorBlock: chain.BlockNumber(990),
	})
	require.NoError(t, err)
	return id
}

func (f *diffsFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(f.ts.URL + path)
	require.NoError(t, err)
	return resp
}

// --- GET /diffs/{id} --------------------------------------------------

func TestGetDiff_Returns200WithView(t *testing.T) {
	f := newDiffsFixture(t)
	id := f.seed(t, "run-1", verification.MetricBlockHash)

	resp := f.get(t, "/diffs/"+string(id))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		ID             string `json:"id"`
		RunID          string `json:"run_id"`
		MetricKey      string `json:"metric_key"`
		MetricCategory string `json:"metric_category"`
		Block          uint64 `json:"block"`
		Severity       string `json:"severity"`
		Tier           string `json:"tier"`
		AnchorBlock    uint64 `json:"anchor_block"`
		Values         []struct {
			SourceID string `json:"source_id"`
			Raw      string `json:"raw"`
		} `json:"values"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, string(id), body.ID)
	require.Equal(t, "run-1", body.RunID)
	require.Equal(t, "block.hash", body.MetricKey)
	require.Equal(t, "block_immutable", body.MetricCategory)
	require.Equal(t, uint64(100), body.Block)
	require.Equal(t, "critical", body.Severity)
	require.Equal(t, "A", body.Tier)
	require.Equal(t, uint64(990), body.AnchorBlock)
	require.Len(t, body.Values, 2)
	// Sources are sorted alphabetically by source id for determinism.
	require.Equal(t, "blockscout", body.Values[0].SourceID)
	require.Equal(t, "rpc", body.Values[1].SourceID)
}

func TestGetDiff_Missing_Returns404(t *testing.T) {
	f := newDiffsFixture(t)
	resp := f.get(t, "/diffs/no-such-id")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- GET /diffs --------------------------------------------------------

func TestListDiffs_Empty(t *testing.T) {
	f := newDiffsFixture(t)
	resp := f.get(t, "/diffs")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 0, body.Total)
}

func TestListDiffs_SeeveralSeeded_ReturnsAll(t *testing.T) {
	f := newDiffsFixture(t)
	f.seed(t, "run-1", verification.MetricBlockHash)
	f.seed(t, "run-1", verification.MetricBlockTimestamp)
	f.seed(t, "run-2", verification.MetricBlockHash)

	resp := f.get(t, "/diffs?limit=10")
	defer resp.Body.Close()
	var body struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 3, body.Total)
}

func TestListDiffs_FiltersByRunID(t *testing.T) {
	f := newDiffsFixture(t)
	f.seed(t, "run-1", verification.MetricBlockHash)
	f.seed(t, "run-2", verification.MetricBlockHash)

	resp := f.get(t, "/diffs?run_id=run-1")
	defer resp.Body.Close()
	var body struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 1, body.Total)
}

func TestListDiffs_UnknownSeverity_Returns400(t *testing.T) {
	f := newDiffsFixture(t)
	resp := f.get(t, "/diffs?severity=bogus")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- GET /runs/{id}/diffs --------------------------------------------

func TestGetRunDiffs_ReturnsOnlyThatRun(t *testing.T) {
	f := newDiffsFixture(t)
	f.seed(t, "run-1", verification.MetricBlockHash)
	f.seed(t, "run-1", verification.MetricBlockTimestamp)
	f.seed(t, "run-2", verification.MetricBlockHash)

	resp := f.get(t, "/runs/run-1/diffs")
	defer resp.Body.Close()
	var body struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 2, body.Total)
}
