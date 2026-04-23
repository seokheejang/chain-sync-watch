package application

import "errors"

// Sentinel errors returned by the application layer. They wrap the
// repository / gateway errors into a stable set so callers (HTTP
// handlers in Phase 8, queue workers in Phase 7) can match with
// errors.Is and map to HTTP status codes or retry policies without
// importing the infrastructure packages.
var (
	// ErrRunNotFound — no Run with the given RunID exists in the
	// RunRepository.
	ErrRunNotFound = errors.New("application: run not found")

	// ErrDiffNotFound — no DiffRecord with the given DiffID exists in
	// the DiffRepository.
	ErrDiffNotFound = errors.New("application: diff not found")

	// ErrDuplicateRun — a Run with the given RunID already exists.
	// RunRepository.Save returns this when the caller supplies an id
	// that collides with an existing record.
	ErrDuplicateRun = errors.New("application: duplicate run id")

	// ErrBudgetExhausted — the RateLimitBudget denied a Reserve call.
	// ExecuteRun's budget-exhausted policy (skip / defer / fail)
	// branches on this sentinel.
	ErrBudgetExhausted = errors.New("application: rate limit budget exhausted")

	// ErrInvalidRun — Run construction rejected the inputs (empty
	// metrics list, nil strategy, etc.). The underlying verification-
	// layer error is wrapped and retrievable via errors.Unwrap.
	ErrInvalidRun = errors.New("application: invalid run")

	// ErrSourceNotFound — no SourceConfig with the given id exists.
	ErrSourceNotFound = errors.New("application: source not found")

	// ErrDuplicateSource — a SourceConfig with the same (Type,
	// ChainID) pair already exists. The DB UNIQUE constraint surfaces
	// as this sentinel so the HTTP layer can map to 409 Conflict.
	ErrDuplicateSource = errors.New("application: duplicate source for chain")
)
