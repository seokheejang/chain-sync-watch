package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// RunsDeps bundles the use-case structs the /runs routes need. The
// route registration takes only what it uses so that swapping or
// omitting a use case does not cascade through helper types.
type RunsDeps struct {
	Schedule *application.ScheduleRun
	Query    application.QueryRuns
	Cancel   *application.CancelRun
}

// RegisterRuns mounts the /runs resource onto the huma API when the
// three use cases are wired. A nil Schedule or Cancel pointer is
// tolerated — those endpoints simply don't register, keeping the
// dev/test story flexible.
func RegisterRuns(api huma.API, d RunsDeps) {
	if d.Schedule != nil {
		huma.Register(api, huma.Operation{
			OperationID:   "create-run",
			Method:        http.MethodPost,
			Path:          "/runs",
			Summary:       "Create and dispatch a verification run",
			Tags:          []string{"runs"},
			DefaultStatus: http.StatusCreated,
		}, func(ctx context.Context, in *createRunInput) (*createRunOutput, error) {
			ucInput, err := in.Body.ToUseCase()
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			result, err := d.Schedule.Execute(ctx, ucInput)
			if err != nil {
				return nil, MapError(err)
			}
			out := &createRunOutput{}
			out.Body.RunID = string(result.RunID)
			if result.JobID != nil {
				s := string(*result.JobID)
				out.Body.JobID = &s
			}
			return out, nil
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-run",
		Method:      http.MethodGet,
		Path:        "/runs/{id}",
		Summary:     "Fetch a run by id",
		Tags:        []string{"runs"},
	}, func(ctx context.Context, in *getRunInput) (*runDetailOutput, error) {
		r, err := d.Query.Get(ctx, verification.RunID(in.ID))
		if err != nil {
			return nil, MapError(err)
		}
		out := &runDetailOutput{}
		out.Body = dto.ToRunView(r)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-runs",
		Method:      http.MethodGet,
		Path:        "/runs",
		Summary:     "List runs with optional filters",
		Tags:        []string{"runs"},
	}, func(ctx context.Context, in *listRunsInput) (*listRunsOutput, error) {
		filter := application.RunFilter{
			Limit:  in.Limit,
			Offset: in.Offset,
		}
		if in.ChainID != 0 {
			cid := chain.ChainID(in.ChainID)
			filter.ChainID = &cid
		}
		if in.Status != "" {
			st := verification.Status(in.Status)
			filter.Status = &st
		}
		runs, total, err := d.Query.List(ctx, filter)
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.RunView, len(runs))
		for i, r := range runs {
			items[i] = dto.ToRunView(r)
		}
		out := &listRunsOutput{}
		out.Body.Items = items
		out.Body.Total = total
		out.Body.Limit = filter.Limit
		out.Body.Offset = filter.Offset
		return out, nil
	})

	if d.Cancel != nil {
		huma.Register(api, huma.Operation{
			OperationID:   "cancel-run",
			Method:        http.MethodPost,
			Path:          "/runs/{id}/cancel",
			Summary:       "Cancel a pending or running run",
			Tags:          []string{"runs"},
			DefaultStatus: http.StatusNoContent,
		}, func(ctx context.Context, in *cancelRunInput) (*struct{}, error) {
			if err := d.Cancel.Execute(ctx, verification.RunID(in.ID)); err != nil {
				return nil, MapError(err)
			}
			return nil, nil
		})
	}
}

// --- Typed IO --------------------------------------------------------

type createRunInput struct {
	Body dto.CreateRunRequest
}

type createRunOutput struct {
	Body dto.CreateRunResponse
}

type getRunInput struct {
	ID string `path:"id" example:"abc123" doc:"Run identifier"`
}

type runDetailOutput struct {
	Body dto.RunView
}

type listRunsInput struct {
	ChainID uint64 `query:"chain_id" doc:"Filter by chain id; 0 means no filter"`
	Status  string `query:"status" doc:"Filter by status (pending/running/completed/failed/cancelled)"`
	Limit   int    `query:"limit" minimum:"0" maximum:"500" default:"50"`
	Offset  int    `query:"offset" minimum:"0" default:"0"`
}

type listRunsOutput struct {
	Body dto.ListRunsResponse
}

type cancelRunInput struct {
	ID string `path:"id"`
}
