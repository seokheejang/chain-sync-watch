// Package httpapi wires the chi router to the huma v2 API framework and
// exposes a typed REST surface to the frontend. The package is split into:
//
//   - server.go — NewServer builds the chi.Mux, installs middleware,
//     registers the huma API, and returns a standard *http.Server.
//   - routes/ — one file per resource (runs, diffs, schedules, sources,
//     health). Each exposes a Register function that takes only the
//     use-case structs it needs, keeping dependency wiring explicit.
//   - dto/ — HTTP-shaped input/output types. Mappers between DTOs and
//     the domain live alongside each DTO; the domain never imports this
//     package.
//   - errors.go — application sentinel errors → huma HTTP errors.
//
// The package deliberately knows nothing about database drivers,
// queues, or Redis — every side effect goes through an application
// port. The cmd/csw-server binary is the only place that constructs
// the concrete adapters and hands them to NewServer.
package httpapi
