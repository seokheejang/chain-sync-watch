package source

import (
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// BlockTag anchors a Query to a specific view of chain state. It wraps
// the three named RPC tags ("latest", "safe", "finalized") and a
// numeric form so adapters can translate to whatever the upstream
// expects (JSON-RPC tag string, REST ?block= parameter, GraphQL int).
//
// The zero value is BlockTagLatest so existing Query types can gain an
// Anchor field without breaking callers who construct queries with
// struct literals — an unset Anchor still means "latest", preserving
// pre-2C behaviour.
type BlockTag struct {
	kind BlockTagKind
	num  chain.BlockNumber
}

// BlockTagKind enumerates the supported anchor forms.
type BlockTagKind uint8

const (
	// BlockTagLatest is the zero value. Any field typed BlockTag that
	// is left unset reads as "latest".
	BlockTagLatest BlockTagKind = iota
	BlockTagSafe
	BlockTagFinalized
	BlockTagNumeric
)

// NewBlockTagLatest returns the "latest" tag. Prefer the zero value of
// BlockTag in most places; use this constructor when intent matters at
// the call site (e.g., setting a struct field by name).
func NewBlockTagLatest() BlockTag { return BlockTag{kind: BlockTagLatest} }

// NewBlockTagSafe returns the "safe" tag. Semantics follow the JSON-
// RPC spec: the latest block considered safe by the execution client.
func NewBlockTagSafe() BlockTag { return BlockTag{kind: BlockTagSafe} }

// NewBlockTagFinalized returns the "finalized" tag. This is the
// preferred verification anchor — blocks at or below this height are
// guaranteed not to reorg.
func NewBlockTagFinalized() BlockTag { return BlockTag{kind: BlockTagFinalized} }

// BlockTagAt pins the tag to a specific historical height. Adapters
// without archive support must return ErrUnsupported for numeric
// anchors that fall before their earliest served height.
func BlockTagAt(n chain.BlockNumber) BlockTag {
	return BlockTag{kind: BlockTagNumeric, num: n}
}

// Kind returns which form the tag takes. Adapters branch on this
// before reading Number.
func (t BlockTag) Kind() BlockTagKind { return t.kind }

// Number returns the historical height for a BlockTagNumeric tag and
// the zero BlockNumber otherwise. Callers must always consult Kind
// first — relying on Number alone will silently yield zero for named
// tags.
func (t BlockTag) Number() chain.BlockNumber { return t.num }

// String returns the RPC-canonical form: "latest", "safe", "finalized",
// or the lowercase hex height ("0x2a"). The output is stable so
// adapters can forward it directly as a JSON-RPC tag parameter without
// an adapter-specific conversion.
func (t BlockTag) String() string {
	switch t.kind {
	case BlockTagLatest:
		return "latest"
	case BlockTagSafe:
		return "safe"
	case BlockTagFinalized:
		return "finalized"
	case BlockTagNumeric:
		return t.num.Hex()
	}
	return fmt.Sprintf("unknown(%d)", t.kind)
}

// IsZero reports whether the tag is the zero value (semantically
// identical to BlockTagLatest). Useful for adapters that want to log
// "caller left Anchor unset" vs "caller asked explicitly for latest".
func (t BlockTag) IsZero() bool {
	return t.kind == BlockTagLatest && t.num == 0
}
