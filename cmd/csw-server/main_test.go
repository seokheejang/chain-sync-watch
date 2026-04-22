package main

import "testing"

// Present so `go test -coverprofile` has a stub binary to run for
// this package. Real coverage of main() lives in integration tests
// that exercise the server via httptest / docker-compose.
func TestMainPackageCompiles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("nothing to run in short mode")
	}
}
