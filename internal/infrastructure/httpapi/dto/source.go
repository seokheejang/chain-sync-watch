package dto

import (
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// CapabilityView is one entry in a source's capability matrix.
// Name is the stable identifier (e.g. "block.hash"); Tier is the
// policy tier the core assigns to that capability.
type CapabilityView struct {
	Name string `json:"name"`
	Tier string `json:"tier" enum:"A,B,C,unknown"`
}

// SourceCapabilityMatrix is the read-only "what can this source
// answer" view. Used by GET /sources/{id}/capabilities when an
// operator wants to audit an adapter's coverage. CRUD pages don't
// need this — the core fields live on SourceConfigView.
type SourceCapabilityMatrix struct {
	ID           string           `json:"id"`
	ChainID      uint64           `json:"chain_id"`
	Capabilities []CapabilityView `json:"capabilities"`
}

// ToSourceCapabilityMatrix renders a live source.Source into the
// capability matrix wire shape. Iterates AllCapabilities() so the
// response order is stable across restarts / adapter versions.
func ToSourceCapabilityMatrix(s source.Source) SourceCapabilityMatrix {
	caps := make([]CapabilityView, 0)
	for _, c := range source.AllCapabilities() {
		if !s.Supports(c) {
			continue
		}
		caps = append(caps, CapabilityView{
			Name: string(c),
			Tier: c.Tier().String(),
		})
	}
	return SourceCapabilityMatrix{
		ID:           string(s.ID()),
		ChainID:      uint64(s.ChainID()),
		Capabilities: caps,
	}
}

// SourceConfigView is the admin-facing representation of a
// SourceConfig row. The Options bag is returned verbatim so the UI
// can display adapter-specific settings (archive, rate limits).
// HasSecret is a boolean flag rather than the ciphertext — the
// server never ships credential material over the wire, even
// encrypted, because ciphertext plus a compromised CSW_SECRET_KEY
// is game-over.
//
// Type is an open string rather than an OpenAPI enum because
// private-build forks add their own adapter types via
// gateway.Registry. The backend still validates — an unknown type
// yields ErrUnknownType at build time before any row is persisted.
type SourceConfigView struct {
	ID        string         `json:"id"`
	Type      string         `json:"type" doc:"Adapter type (rpc / blockscout / routescan, or a private-build custom type)"`
	ChainID   uint64         `json:"chain_id"`
	Endpoint  string         `json:"endpoint"`
	Options   map[string]any `json:"options"`
	HasSecret bool           `json:"has_secret"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ToSourceConfigView renders a SourceConfig into the admin view.
// Options defaults to an empty map when the row carries nil so the
// JSON consumer never has to branch on null.
func ToSourceConfigView(cfg application.SourceConfig) SourceConfigView {
	opts := cfg.Options
	if opts == nil {
		opts = map[string]any{}
	}
	return SourceConfigView{
		ID:        cfg.ID,
		Type:      cfg.Type,
		ChainID:   cfg.ChainID.Uint64(),
		Endpoint:  cfg.Endpoint,
		Options:   opts,
		HasSecret: cfg.HasSecret(),
		Enabled:   cfg.Enabled,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
	}
}

// ListSourcesResponse is the GET /sources body — the admin CRUD
// shape. Total mirrors len(Items) because /sources is chain-scoped
// and does not paginate today.
type ListSourcesResponse struct {
	Items []SourceConfigView `json:"items"`
	Total int                `json:"total"`
}

// SourceTypesResponse lists the adapter type strings the current
// binary's gateway Registry knows about. The admin UI feeds this
// to the type dropdown so private-build deployments see their
// custom types without a public-repo code change.
type SourceTypesResponse struct {
	Types []string `json:"types"`
}

// CreateSourceRequest is the POST /sources body. APIKey is plaintext
// on the wire (TLS protects transport); the server encrypts with
// CSW_SECRET_KEY before persisting. APIKey="" means "no credential".
// Type accepts any adapter type the backend's gateway.Registry
// knows about — an unknown value errors at handler time rather
// than being silently accepted.
type CreateSourceRequest struct {
	Type     string         `json:"type" minLength:"1" doc:"Adapter type registered in the gateway (rpc / blockscout / routescan by default; private builds may add more)"`
	ChainID  uint64         `json:"chain_id" minimum:"1" example:"10"`
	Endpoint string         `json:"endpoint" minLength:"1"`
	APIKey   string         `json:"api_key,omitempty" doc:"Plaintext secret; server encrypts with CSW_SECRET_KEY before persisting. Empty = no credential."`
	Options  map[string]any `json:"options,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty" doc:"Defaults to true when omitted"`
}

// UpdateSourceRequest mirrors the create body minus ChainID + Type
// (those are natural-key; changing them means deleting and
// recreating). APIKey semantics: empty string keeps the existing
// secret; to clear it the caller sets ClearSecret=true.
type UpdateSourceRequest struct {
	Endpoint    *string        `json:"endpoint,omitempty"`
	APIKey      *string        `json:"api_key,omitempty" doc:"Nil keeps existing secret; empty string still keeps; set ClearSecret=true to remove."`
	ClearSecret bool           `json:"clear_secret,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty"`
}
