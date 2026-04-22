package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// DiffsDeps wires the /diffs routes' use cases. Replay is optional —
// deployments without Sources wired yet (pre-Phase 10) can leave it
// nil and the route will simply not register.
type DiffsDeps struct {
	Query  application.QueryDiffs
	Replay *application.ReplayDiff
}

// RegisterDiffs mounts the /diffs resource.
func RegisterDiffs(api huma.API, d DiffsDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-diff",
		Method:      http.MethodGet,
		Path:        "/diffs/{id}",
		Summary:     "Fetch a discrepancy record by id",
		Tags:        []string{"diffs"},
	}, func(ctx context.Context, in *getDiffInput) (*diffDetailOutput, error) {
		rec, err := d.Query.Get(ctx, application.DiffID(in.ID))
		if err != nil {
			return nil, MapError(err)
		}
		out := &diffDetailOutput{}
		out.Body = dto.ToDiffView(rec.ID, *rec)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-diffs",
		Method:      http.MethodGet,
		Path:        "/diffs",
		Summary:     "List discrepancies with optional filters",
		Tags:        []string{"diffs"},
	}, func(ctx context.Context, in *listDiffsInput) (*listDiffsOutput, error) {
		filter := application.DiffFilter{
			Limit:  in.Limit,
			Offset: in.Offset,
		}
		if in.RunID != "" {
			rid := verification.RunID(in.RunID)
			filter.RunID = &rid
		}
		if in.MetricKey != "" {
			mk := in.MetricKey
			filter.MetricKey = &mk
		}
		if in.Severity != "" {
			sev, err := dto.ParseSeverity(in.Severity)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			filter.Severity = sev
		}
		if in.Resolved != "" {
			switch in.Resolved {
			case "true":
				v := true
				filter.Resolved = &v
			case "false":
				v := false
				filter.Resolved = &v
			default:
				return nil, huma.Error400BadRequest("resolved must be 'true' or 'false'")
			}
		}

		records, total, err := d.Query.List(ctx, filter)
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.DiffView, len(records))
		for i, r := range records {
			items[i] = dto.ToDiffView(r.ID, r)
		}
		out := &listDiffsOutput{}
		out.Body.Items = items
		out.Body.Total = total
		out.Body.Limit = filter.Limit
		out.Body.Offset = filter.Offset
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-run-diffs",
		Method:      http.MethodGet,
		Path:        "/runs/{id}/diffs",
		Summary:     "List discrepancies produced by a specific run",
		Tags:        []string{"diffs", "runs"},
	}, func(ctx context.Context, in *getRunDiffsInput) (*listDiffsOutput, error) {
		records, err := d.Query.ByRun(ctx, verification.RunID(in.ID))
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.DiffView, len(records))
		for i, r := range records {
			items[i] = dto.ToDiffView(r.ID, r)
		}
		out := &listDiffsOutput{}
		out.Body.Items = items
		out.Body.Total = len(items)
		return out, nil
	})

	if d.Replay != nil {
		huma.Register(api, huma.Operation{
			OperationID: "replay-diff",
			Method:      http.MethodPost,
			Path:        "/diffs/{id}/replay",
			Summary:     "Re-verify a discrepancy against the original sources",
			Tags:        []string{"diffs"},
		}, func(ctx context.Context, in *replayDiffInput) (*replayDiffOutput, error) {
			result, err := d.Replay.Execute(ctx, application.DiffID(in.ID))
			if err != nil {
				return nil, MapError(err)
			}
			out := &replayDiffOutput{}
			out.Body = dto.ToReplayDiffResponse(result)
			return out, nil
		})
	}
}

// --- Typed IO --------------------------------------------------------

type getDiffInput struct {
	ID string `path:"id"`
}

type diffDetailOutput struct {
	Body dto.DiffView
}

type listDiffsInput struct {
	RunID     string `query:"run_id" doc:"Filter by run id"`
	MetricKey string `query:"metric_key" doc:"Filter by metric key (e.g. block.hash)"`
	Severity  string `query:"severity" doc:"Filter by severity (info|warning|critical)"`
	Resolved  string `query:"resolved" doc:"Filter by resolution state ('true' or 'false')"`
	Limit     int    `query:"limit" minimum:"0" maximum:"500" default:"50"`
	Offset    int    `query:"offset" minimum:"0" default:"0"`
}

type listDiffsOutput struct {
	Body dto.ListDiffsResponse
}

type getRunDiffsInput struct {
	ID string `path:"id"`
}

type replayDiffInput struct {
	ID string `path:"id"`
}

type replayDiffOutput struct {
	Body dto.ReplayDiffResponse
}
