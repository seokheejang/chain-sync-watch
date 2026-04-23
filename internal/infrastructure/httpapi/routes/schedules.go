package routes

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// SchedulesDeps wires /schedules routes. Schedule is the write path
// (ScheduleRun with ScheduledTrigger), Query lists active records,
// Cancel deactivates by JobID through the dispatcher.
type SchedulesDeps struct {
	Schedule *application.ScheduleRun
	Query    application.QuerySchedules
	// Dispatcher is the one-method slice used for DELETE — we narrow
	// to the method we need so tests don't have to satisfy the full
	// JobDispatcher interface.
	Dispatcher ScheduleCanceller
}

// ScheduleCanceller is the minimal dispatcher surface the DELETE
// handler consumes. application.JobDispatcher satisfies it.
type ScheduleCanceller interface {
	CancelScheduled(ctx context.Context, id application.JobID) error
}

// RegisterSchedules mounts the /schedules resource.
func RegisterSchedules(api huma.API, d SchedulesDeps) {
	if d.Schedule != nil {
		huma.Register(api, huma.Operation{
			OperationID:   "create-schedule",
			Method:        http.MethodPost,
			Path:          "/schedules",
			Summary:       "Register a recurring verification schedule",
			Tags:          []string{"schedules"},
			DefaultStatus: http.StatusCreated,
		}, func(ctx context.Context, in *createScheduleInput) (*createScheduleOutput, error) {
			ucInput, err := scheduleRequestToUseCase(in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			result, err := d.Schedule.Execute(ctx, ucInput)
			if err != nil {
				return nil, MapError(err)
			}
			if result.JobID == nil {
				return nil, huma.Error500InternalServerError("schedule create: job id not returned")
			}
			out := &createScheduleOutput{}
			out.Body.JobID = string(*result.JobID)
			out.Body.RunID = string(result.RunID)
			return out, nil
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-schedules",
		Method:      http.MethodGet,
		Path:        "/schedules",
		Summary:     "List active schedules",
		Tags:        []string{"schedules"},
	}, func(ctx context.Context, _ *struct{}) (*listSchedulesOutput, error) {
		records, err := d.Query.ListActive(ctx)
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.ScheduleView, len(records))
		for i, r := range records {
			items[i] = dto.ToScheduleView(r)
		}
		out := &listSchedulesOutput{}
		out.Body.Items = items
		out.Body.Total = len(items)
		return out, nil
	})

	if d.Dispatcher != nil {
		huma.Register(api, huma.Operation{
			OperationID:   "cancel-schedule",
			Method:        http.MethodDelete,
			Path:          "/schedules/{id}",
			Summary:       "Deactivate a recurring schedule",
			Tags:          []string{"schedules"},
			DefaultStatus: http.StatusNoContent,
		}, func(ctx context.Context, in *cancelScheduleInput) (*struct{}, error) {
			if err := d.Dispatcher.CancelScheduled(ctx, application.JobID(in.ID)); err != nil {
				return nil, MapError(err)
			}
			return nil, nil
		})
	}
}

// scheduleRequestToUseCase reuses the Sampling / Schedule /
// AddressPlan mappers from /runs, forcing the trigger to
// ScheduledTrigger(CronExpr) so callers cannot accidentally create a
// one-off run via the schedules endpoint.
func scheduleRequestToUseCase(r dto.CreateScheduleRequest) (application.ScheduleRunInput, error) {
	strategy, err := r.Sampling.ToDomain()
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	sched, err := r.Schedule.ToDomain()
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	if sched.IsZero() {
		return application.ScheduleRunInput{}, fmt.Errorf("schedule.cron_expr is required")
	}
	metrics, err := dto.ResolveMetrics(r.Metrics)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	plans, err := dto.ResolveAddressPlans(r.AddressPlans)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	tokens, err := dto.ResolveTokenPlans(r.TokenPlans)
	if err != nil {
		return application.ScheduleRunInput{}, err
	}
	return application.ScheduleRunInput{
		ChainID:      chain.ChainID(r.ChainID),
		Strategy:     strategy,
		Metrics:      metrics,
		Trigger:      verification.ScheduledTrigger{CronExpr: sched.CronExpr()},
		Schedule:     sched,
		AddressPlans: plans,
		TokenPlans:   tokens,
	}, nil
}

// --- Typed IO --------------------------------------------------------

type createScheduleInput struct {
	Body dto.CreateScheduleRequest
}

type createScheduleOutput struct {
	Body dto.CreateScheduleResponse
}

type listSchedulesOutput struct {
	Body dto.ListSchedulesResponse
}

type cancelScheduleInput struct {
	ID string `path:"id"`
}
