package dto

// ChainView is the wire shape of a single chain catalog entry. Mirrors
// config.ChainConfig but with JSON tags and no koanf coupling — the
// HTTP surface stays stable even if the config struct evolves.
type ChainView struct {
	ID          uint64 `json:"id" example:"10"`
	Slug        string `json:"slug" example:"optimism"`
	DisplayName string `json:"display_name" example:"Optimism"`
}

// ListChainsResponse is the GET /chains body. No pagination — the
// catalog is small (a handful of chains) and served from embedded
// defaults.yaml, so a single payload is fine.
type ListChainsResponse struct {
	Items []ChainView `json:"items"`
	Total int         `json:"total"`
}
