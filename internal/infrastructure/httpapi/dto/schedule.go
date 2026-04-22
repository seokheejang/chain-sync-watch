package dto

import (
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
)

// CreateScheduleRequest is the POST /schedules body. Schedule-centric
// (no trigger.kind discriminator — the route forces ScheduledTrigger
// behind the scenes). The fields mirror the subset of CreateRunRequest
// that makes sense for a recurring configuration: chain, metrics,
// sampling, schedule, address plans.
type CreateScheduleRequest struct {
	ChainID      uint64             `json:"chain_id" minimum:"1" example:"10"`
	Metrics      []string           `json:"metrics" minItems:"1"`
	Sampling     SamplingInput      `json:"sampling"`
	Schedule     ScheduleInput      `json:"schedule" doc:"Cron + timezone"`
	AddressPlans []AddressPlanInput `json:"address_plans,omitempty"`
}

// CreateScheduleResponse is the POST /schedules body.
type CreateScheduleResponse struct {
	JobID string `json:"job_id" doc:"Opaque identifier for DELETE /schedules/{id}"`
	RunID string `json:"run_id" doc:"First Run the schedule materialised; absent for deferred modes"`
}

// ScheduleView is the canonical GET representation of a
// ScheduleRecord. CronExpr + Timezone come straight from the domain
// Schedule; PlanKinds / StrategyKind / MetricKeys summarise the
// payload without dumping the raw serialised blobs.
type ScheduleView struct {
	JobID            string    `json:"job_id"`
	ChainID          uint64    `json:"chain_id"`
	CronExpr         string    `json:"cron_expr"`
	Timezone         string    `json:"timezone,omitempty"`
	StrategyKind     string    `json:"strategy_kind"`
	MetricKeys       []string  `json:"metric_keys"`
	AddressPlanKinds []string  `json:"address_plan_kinds,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Active           bool      `json:"active"`
}

// ListSchedulesResponse is the GET /schedules body. No pagination
// yet — operators rarely have hundreds of active schedules, and the
// underlying port only exposes ListActive. Paging lands if the list
// grows.
type ListSchedulesResponse struct {
	Items []ScheduleView `json:"items"`
	Total int            `json:"total"`
}

// ToScheduleView renders a ScheduleRecord into the wire shape.
func ToScheduleView(rec application.ScheduleRecord) ScheduleView {
	mkeys := make([]string, len(rec.Metrics))
	for i, m := range rec.Metrics {
		mkeys[i] = m.Key
	}
	var pkinds []string
	if len(rec.AddressPlans) > 0 {
		pkinds = make([]string, len(rec.AddressPlans))
		for i, p := range rec.AddressPlans {
			pkinds[i] = p.Kind()
		}
	}
	view := ScheduleView{
		JobID:            string(rec.JobID),
		ChainID:          uint64(rec.ChainID),
		CronExpr:         rec.Schedule.CronExpr(),
		StrategyKind:     rec.Strategy.Kind(),
		MetricKeys:       mkeys,
		AddressPlanKinds: pkinds,
		CreatedAt:        rec.CreatedAt,
		Active:           rec.Active,
	}
	if loc := rec.Schedule.Timezone(); loc != nil {
		view.Timezone = loc.String()
	}
	return view
}
