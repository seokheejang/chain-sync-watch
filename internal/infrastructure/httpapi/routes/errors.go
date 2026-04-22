package routes

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/seokheejang/chain-sync-watch/internal/application"
)

// MapError lifts an application-layer error into the closest huma HTTP
// error. Route handlers return the result directly; huma converts it
// to an RFC 7807 problem response.
//
// The set is intentionally narrow:
//
//   - ErrRunNotFound / ErrDiffNotFound         → 404
//   - ErrDuplicateRun                          → 409
//   - ErrInvalidRun (validation failures)      → 400
//   - ErrBudgetExhausted                       → 429
//   - everything else                          → 500 (with original
//     error passed so huma includes the message in the body — we
//     trust the application layer not to leak sensitive detail).
//
// A nil error returns nil so callers can funnel all error paths through
// one helper without a nil-check on every branch.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, application.ErrRunNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, application.ErrDiffNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, application.ErrDuplicateRun):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, application.ErrInvalidRun):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, application.ErrBudgetExhausted):
		return huma.Error429TooManyRequests(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
