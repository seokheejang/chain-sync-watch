package source

import "errors"

// Sentinel errors returned by Source implementations. Callers branch on
// these via errors.Is to decide retry policy (transient vs permanent),
// judgement severity, and whether to fall through to a different
// source. Each adapter MUST map its transport-layer errors onto one of
// these before returning — never leak an ethclient/HTTP error directly.
//
// Classification:
//
//	ErrUnsupported       — source does not implement this Capability.
//	                       Skip the combination; not a failure.
//	ErrRateLimited       — transient, retry with backoff; do NOT mark the
//	                       run as failed.
//	ErrSourceUnavailable — transient, network/service down; retry then
//	                       skip this source for the run.
//	ErrNotFound          — permanent for the given query (block/address
//	                       does not exist in this source's view).
//	ErrInvalidResponse   — source returned something we cannot parse;
//	                       almost always indicates a version mismatch
//	                       or corruption — escalate, do not silently
//	                       retry.
var (
	ErrUnsupported       = errors.New("source: unsupported capability")
	ErrRateLimited       = errors.New("source: rate limited")
	ErrSourceUnavailable = errors.New("source: unavailable")
	ErrNotFound          = errors.New("source: not found")
	ErrInvalidResponse   = errors.New("source: invalid response")
)
