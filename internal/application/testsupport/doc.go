// Package testsupport provides in-memory fakes for every application
// port (RunRepository, DiffRepository, SourceGateway, JobDispatcher,
// ChainHead, Clock, RateLimitBudget). Application use-case tests wire
// these up in place of the real infrastructure so scenarios stay
// deterministic and execute in memory.
//
// Fakes live here (not in *_test.go files) so infrastructure-layer
// tests in Phase 6/7 can reuse them as contract fixtures — e.g.,
// running the same ExecuteRun scenarios once against FakeDiffRepo and
// once against the Postgres-backed repository to prove they behave
// identically.
package testsupport
