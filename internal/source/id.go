// Package source defines the abstract contract every data source must
// satisfy — the Source port. Concrete implementations live in the
// top-level adapters/ directory; this package intentionally imports no
// transport libraries (HTTP clients, RPC SDKs, ORMs) so the domain
// stays buildable without any of them.
//
// The contract is field-grained: a Source declares which Capability
// values it can serve, and callers check Supports(...) before trusting
// a Fetch* result to populate a given field.
package source

// SourceID identifies an adapter instance. The core deliberately does
// not enum the known values; each adapter declares its own constant
// (e.g., "rpc", "blockscout") so the core never imports a specific
// adapter package.
type SourceID string
