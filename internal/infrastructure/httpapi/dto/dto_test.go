package dto

import "testing"

// Smoke test so `go test -coverprofile` has a stub to run for this
// package. The mappers are exercised via the routes package's
// httptest-backed handler tests.
func TestDTOPackageCompiles(t *testing.T) {
	t.Parallel()
}
