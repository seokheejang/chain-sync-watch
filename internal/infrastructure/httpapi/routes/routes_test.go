package routes

import "testing"

// Smoke test so `go test -coverprofile` has a stub to run for this
// package. The health routes are exercised end-to-end by
// internal/infrastructure/httpapi server_test.go against the full
// chi + huma stack.
func TestRoutesPackageCompiles(t *testing.T) {
	t.Parallel()
}
