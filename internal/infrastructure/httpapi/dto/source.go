package dto

import (
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// CapabilityView is one entry in a source's capability matrix.
// Name is the stable identifier (e.g. "block.hash"); Tier is the
// policy tier the core assigns to that capability.
type CapabilityView struct {
	Name string `json:"name"`
	Tier string `json:"tier" enum:"A,B,C,unknown"`
}

// SourceView describes one registered adapter. Capabilities is the
// set it declares via Source.Supports — filtered down to the
// intersection of AllCapabilities() and the adapter's real answer so
// the list is stable across restarts.
type SourceView struct {
	ID           string           `json:"id"`
	ChainID      uint64           `json:"chain_id"`
	Capabilities []CapabilityView `json:"capabilities"`
}

// ListSourcesResponse is the GET /sources body.
type ListSourcesResponse struct {
	Items []SourceView `json:"items"`
	Total int          `json:"total"`
}

// ToSourceView renders a Source into the wire shape. Iterates the
// canonical AllCapabilities() list so every adapter's output is in
// the same order (handy for UI tables and snapshot tests).
func ToSourceView(s source.Source) SourceView {
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
	return SourceView{
		ID:           string(s.ID()),
		ChainID:      uint64(s.ChainID()),
		Capabilities: caps,
	}
}
