package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// SourcesDeps wires every /sources route. Each field is optional:
//
//   - Repo: required for config CRUD (list/get/create/update/delete).
//     Without it only capability inspection (via Gateway) is served.
//   - Gateway: required for the capability matrix endpoint. Without
//     it that one route skips registration.
//   - Cipher: required when the deployment lets operators submit
//     api_keys through the API. A nil Cipher rejects any create /
//     update with a non-empty APIKey at handler time.
//   - Clock: stamps CreatedAt / UpdatedAt. Injected so tests can
//     freeze time.
//
// All four are passed from cmd/csw-server/main.go's buildDeps. The
// openapi-dump binary wires stubs so the full route set emits in
// the generated spec.
type SourcesDeps struct {
	Repo    application.SourceConfigRepository
	Gateway application.SourceGateway
	Cipher  *secrets.Cipher
	Clock   application.Clock
	// Types is the list of adapter type strings the current
	// binary's gateway.Registry knows about. Surfaces through
	// GET /sources/types so the admin UI can populate its type
	// dropdown without a hard-coded enum (private builds add
	// entries here transparently).
	Types []string
}

// RegisterSources mounts the /sources resource. Handlers are
// carved per-route below so individual endpoints can opt out
// cleanly when their dep is nil (e.g. capability matrix without a
// Gateway).
//
// TODO(phase-10b): wrap the write paths (POST/PUT/DELETE) with a
// role-aware middleware. Phase 10a accepts unauthenticated writes
// under the "reverse proxy enforces auth" assumption documented in
// README — doing it at the app layer would duplicate the check.
func RegisterSources(api huma.API, d SourcesDeps) {
	if d.Repo != nil {
		registerSourceCRUD(api, d)
	}
	if d.Gateway != nil {
		registerSourceCapabilities(api, d.Gateway)
	}
	if len(d.Types) > 0 {
		registerSourceTypes(api, d.Types)
	}
}

func registerSourceTypes(api huma.API, types []string) {
	huma.Register(api, huma.Operation{
		OperationID: "list-source-types",
		Method:      http.MethodGet,
		Path:        "/sources/types",
		Summary:     "List adapter type strings registered in this binary",
		Tags:        []string{"sources"},
	}, func(_ context.Context, _ *struct{}) (*sourceTypesOutput, error) {
		out := &sourceTypesOutput{}
		out.Body.Types = append([]string(nil), types...)
		return out, nil
	})
}

func registerSourceCRUD(api huma.API, d SourcesDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-sources",
		Method:      http.MethodGet,
		Path:        "/sources",
		Summary:     "List configured source adapter rows for a chain",
		Tags:        []string{"sources"},
	}, func(ctx context.Context, in *listSourcesInput) (*listSourcesOutput, error) {
		rows, err := d.Repo.ListByChain(ctx, chain.ChainID(in.ChainID), false)
		if err != nil {
			return nil, MapError(err)
		}
		items := make([]dto.SourceConfigView, len(rows))
		for i, r := range rows {
			items[i] = dto.ToSourceConfigView(r)
		}
		out := &listSourcesOutput{}
		out.Body.Items = items
		out.Body.Total = len(items)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-source",
		Method:      http.MethodGet,
		Path:        "/sources/{id}",
		Summary:     "Fetch a source configuration by id",
		Tags:        []string{"sources"},
	}, func(ctx context.Context, in *sourceIDPath) (*sourceConfigOutput, error) {
		cfg, err := d.Repo.FindByID(ctx, in.ID)
		if err != nil {
			return nil, MapError(err)
		}
		out := &sourceConfigOutput{}
		out.Body = dto.ToSourceConfigView(*cfg)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-source",
		Method:        http.MethodPost,
		Path:          "/sources",
		Summary:       "Create a new source configuration",
		Tags:          []string{"sources"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSourceInput) (*sourceConfigOutput, error) {
		cfg, err := buildCreateConfig(in.Body, d.Cipher, clockOrNow(d.Clock))
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if err := d.Repo.Save(ctx, cfg); err != nil {
			return nil, MapError(err)
		}
		out := &sourceConfigOutput{}
		out.Body = dto.ToSourceConfigView(cfg)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-source",
		Method:      http.MethodPut,
		Path:        "/sources/{id}",
		Summary:     "Update a source configuration",
		Tags:        []string{"sources"},
	}, func(ctx context.Context, in *updateSourceInput) (*sourceConfigOutput, error) {
		existing, err := d.Repo.FindByID(ctx, in.ID)
		if err != nil {
			return nil, MapError(err)
		}
		updated, err := applyUpdate(*existing, in.Body, d.Cipher, clockOrNow(d.Clock))
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if err := d.Repo.Save(ctx, updated); err != nil {
			return nil, MapError(err)
		}
		out := &sourceConfigOutput{}
		out.Body = dto.ToSourceConfigView(updated)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-source",
		Method:        http.MethodDelete,
		Path:          "/sources/{id}",
		Summary:       "Delete a source configuration",
		Tags:          []string{"sources"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sourceIDPath) (*struct{}, error) {
		if err := d.Repo.Delete(ctx, in.ID); err != nil {
			return nil, MapError(err)
		}
		return nil, nil
	})
}

func registerSourceCapabilities(api huma.API, gw application.SourceGateway) {
	huma.Register(api, huma.Operation{
		OperationID: "get-source-capabilities",
		Method:      http.MethodGet,
		Path:        "/sources/{id}/capabilities",
		Summary:     "Inspect a source's capability matrix at runtime",
		Tags:        []string{"sources"},
	}, func(_ context.Context, in *sourceIDPath) (*sourceCapabilityOutput, error) {
		s, err := gw.Get(source.SourceID(in.ID))
		if err != nil {
			return nil, MapError(err)
		}
		out := &sourceCapabilityOutput{}
		out.Body = dto.ToSourceCapabilityMatrix(s)
		return out, nil
	})
}

// buildCreateConfig turns a CreateSourceRequest into a SourceConfig,
// including the encryption of any supplied APIKey. The config's ID
// is derived as `{type}-{chain_id}` — MVP scope caps one adapter
// instance per (type, chain), so the natural key doubles as the
// primary key.
func buildCreateConfig(body dto.CreateSourceRequest, cipher *secrets.Cipher, now time.Time) (application.SourceConfig, error) {
	if body.Type == "" {
		return application.SourceConfig{}, errors.New("type is required")
	}
	if body.ChainID == 0 {
		return application.SourceConfig{}, errors.New("chain_id is required")
	}
	cfg := application.SourceConfig{
		ID:        fmt.Sprintf("%s-%d", body.Type, body.ChainID),
		Type:      body.Type,
		ChainID:   chain.ChainID(body.ChainID),
		Endpoint:  body.Endpoint,
		Options:   body.Options,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}
	if body.APIKey != "" {
		if cipher == nil {
			return application.SourceConfig{}, errors.New("api_key supplied but server has no CSW_SECRET_KEY configured")
		}
		ct, nonce, err := cipher.Encrypt([]byte(body.APIKey))
		if err != nil {
			return application.SourceConfig{}, fmt.Errorf("encrypt api_key: %w", err)
		}
		cfg.SecretCiphertext = ct
		cfg.SecretNonce = nonce
	}
	return cfg, nil
}

// applyUpdate merges an UpdateSourceRequest into the existing
// config. Nil optional fields keep the previous value; ClearSecret
// wipes the secret pair even when APIKey is also set (clear wins).
func applyUpdate(current application.SourceConfig, body dto.UpdateSourceRequest, cipher *secrets.Cipher, now time.Time) (application.SourceConfig, error) {
	if body.Endpoint != nil {
		current.Endpoint = *body.Endpoint
	}
	if body.Options != nil {
		current.Options = body.Options
	}
	if body.Enabled != nil {
		current.Enabled = *body.Enabled
	}
	switch {
	case body.ClearSecret:
		current.SecretCiphertext = nil
		current.SecretNonce = nil
	case body.APIKey != nil && *body.APIKey != "":
		if cipher == nil {
			return application.SourceConfig{}, errors.New("api_key supplied but server has no CSW_SECRET_KEY configured")
		}
		ct, nonce, err := cipher.Encrypt([]byte(*body.APIKey))
		if err != nil {
			return application.SourceConfig{}, fmt.Errorf("encrypt api_key: %w", err)
		}
		current.SecretCiphertext = ct
		current.SecretNonce = nonce
	}
	current.UpdatedAt = now
	return current, nil
}

func clockOrNow(c application.Clock) time.Time {
	if c == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

// --- Typed IO ---------------------------------------------------------

type listSourcesInput struct {
	ChainID uint64 `query:"chain_id" required:"true" minimum:"1" example:"10" doc:"Target chain id"`
}

type listSourcesOutput struct {
	Body dto.ListSourcesResponse
}

type sourceIDPath struct {
	ID string `path:"id"`
}

type sourceConfigOutput struct {
	Body dto.SourceConfigView
}

type createSourceInput struct {
	Body dto.CreateSourceRequest
}

type updateSourceInput struct {
	ID   string `path:"id"`
	Body dto.UpdateSourceRequest
}

type sourceCapabilityOutput struct {
	Body dto.SourceCapabilityMatrix
}

type sourceTypesOutput struct {
	Body dto.SourceTypesResponse
}
