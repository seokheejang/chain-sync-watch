package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
)

// ChainsDeps carries the chain catalog the server is willing to
// surface. The slice is owned upstream (config.Load output, reshaped
// into wire DTOs) so this layer stays free of koanf / config imports.
// An empty slice still registers the route — callers hit a `/chains`
// that returns an empty list rather than 404, which is easier for
// the frontend to reason about.
type ChainsDeps struct {
	Catalog []dto.ChainView
}

// RegisterChains mounts GET /chains. The endpoint is intentionally
// read-only and unauthenticated — the catalog is static and public.
func RegisterChains(api huma.API, d ChainsDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-chains",
		Method:      http.MethodGet,
		Path:        "/chains",
		Summary:     "List supported chain catalog entries",
		Tags:        []string{"chains"},
	}, func(_ context.Context, _ *struct{}) (*listChainsOutput, error) {
		items := append([]dto.ChainView(nil), d.Catalog...)
		out := &listChainsOutput{}
		out.Body.Items = items
		out.Body.Total = len(items)
		return out, nil
	})
}

// --- Typed IO --------------------------------------------------------

type listChainsOutput struct {
	Body dto.ListChainsResponse
}
