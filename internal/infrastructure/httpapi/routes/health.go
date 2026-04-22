// Package routes hosts one file per HTTP resource. Each Register
// function takes only the ports/use-cases it needs — no package-wide
// facade — so dependency wiring is visible at the call site.
package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// HealthChecker is the readiness check port. Implementations probe
// downstream dependencies (Postgres ping, Redis ping) and return an
// error when any of them is unreachable. A nil implementation makes
// /readyz always report ok — useful in unit tests.
type HealthChecker interface {
	Ready(ctx context.Context) error
}

// HealthDeps bundles the optional readiness checker. Zero value is
// legal: /healthz always returns 200, /readyz returns 200 when
// Readiness is nil.
type HealthDeps struct {
	Readiness HealthChecker
}

// HealthResponse is the envelope for both endpoints. A stable shape
// (status + optional detail) keeps the Kubernetes probe contract
// simple: any 2xx is healthy, any 5xx is not.
type HealthResponse struct {
	Body struct {
		Status string `json:"status" example:"ok"`
		Detail string `json:"detail,omitempty"`
	}
}

// RegisterHealth wires /healthz (liveness) and /readyz (readiness)
// onto the huma API. Both live at the root of the API path so
// Kubernetes probes don't have to know about versioning.
func RegisterHealth(api huma.API, d HealthDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "health-live",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness probe",
		Description: "Returns 200 whenever the server process is accepting HTTP traffic. Does not probe downstream dependencies.",
		Tags:        []string{"health"},
	}, func(_ context.Context, _ *struct{}) (*HealthResponse, error) {
		out := &HealthResponse{}
		out.Body.Status = "ok"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Readiness probe",
		Description: "Returns 200 when Postgres and Redis are reachable; 503 otherwise.",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*HealthResponse, error) {
		out := &HealthResponse{}
		if d.Readiness == nil {
			out.Body.Status = "ok"
			return out, nil
		}
		if err := d.Readiness.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable(err.Error())
		}
		out.Body.Status = "ok"
		return out, nil
	})
}
