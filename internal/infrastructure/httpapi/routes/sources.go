package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
)

// SourcesDeps wires the /sources route. A nil Gateway causes the
// route not to register — pre-Phase 10 deployments that haven't
// assembled a gateway skip source discovery this way instead of
// crashing on nil.
type SourcesDeps struct {
	Gateway application.SourceGateway
}

// RegisterSources mounts GET /sources. The endpoint requires
// chain_id — the SourceGateway port is keyed by chain and has no
// enumeration method yet. When we need cross-chain listings we'll
// widen the port instead of faking it here.
func RegisterSources(api huma.API, d SourcesDeps) {
	if d.Gateway == nil {
		return
	}
	huma.Register(api, huma.Operation{
		OperationID: "list-sources",
		Method:      http.MethodGet,
		Path:        "/sources",
		Summary:     "List configured adapters for a chain with their capability + tier matrix",
		Tags:        []string{"sources"},
	}, func(ctx context.Context, in *listSourcesInput) (*listSourcesOutput, error) {
		sources, err := d.Gateway.ForChain(chain.ChainID(in.ChainID))
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.SourceView, len(sources))
		for i, s := range sources {
			items[i] = dto.ToSourceView(s)
		}
		out := &listSourcesOutput{}
		out.Body.Items = items
		out.Body.Total = len(items)
		return out, nil
	})
}

type listSourcesInput struct {
	ChainID uint64 `query:"chain_id" required:"true" minimum:"1" example:"10" doc:"Target chain id"`
}

type listSourcesOutput struct {
	Body dto.ListSourcesResponse
}
