// Package cmdopenapi implements the `csw openapi-dump` subcommand.
// Its only job is to serialise the HTTP server's OpenAPI 3.1
// document to stdout so the frontend codegen step (or a CI drift
// check) can consume a committed spec file.
package cmdopenapi

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/stubs"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// Run dispatches the `csw openapi-dump` subcommand. Accepted flags:
//
//	--format=json|yaml  (default: json)
//	--output=path       (default: stdout)
//
// The command instantiates a server with a Deps struct populated by
// in-memory stubs — no DB or Redis required. The use cases are
// real struct literals binding no-op repositories, so every
// Register* function takes its "wired" branch and emits the full
// route set. Handlers would error at runtime if invoked, but the
// spec generator only traverses registrations, never dispatches.
//
// Writes are atomic at the process boundary: we buffer the full
// payload, then io.Copy in one shot, so a truncated file is never
// written under normal conditions.
func Run(args []string) error {
	fs := flag.NewFlagSet("openapi-dump", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "json", "output format: json or yaml")
	output := fs.String("output", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("openapi-dump: parse flags: %w", err)
	}

	deps := specDeps()

	var (
		data []byte
		err  error
	)
	switch *format {
	case "json":
		data, err = httpapi.OpenAPIJSON(deps)
	case "yaml":
		data, err = httpapi.OpenAPIYAML(deps)
	default:
		return fmt.Errorf("openapi-dump: unknown format %q", *format)
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("openapi-dump: empty spec")
	}

	if *output == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("openapi-dump: write stdout: %w", err)
		}
		// Pretty-friendly newline on stdout so shell redirects produce
		// a trailing LF like every other unix tool.
		_, _ = os.Stdout.Write([]byte("\n"))
		return nil
	}

	// #nosec G306 — dumped spec is public; 0o644 is fine.
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return fmt.Errorf("openapi-dump: write file: %w", err)
	}
	return nil
}

// specDeps returns a fully-populated Deps tree backed by in-memory
// stubs. Every conditional route registers because its dep pointer /
// interface is non-nil. Invoking any of these handlers at runtime
// would hit the stub's error path — which is fine for spec emission.
func specDeps() httpapi.Deps {
	clock := stubs.SystemClock{}
	gateway := stubs.NullGateway{}
	runs := noopRunRepo{}
	diffs := noopDiffRepo{}
	schedules := noopScheduleRepo{}
	dispatcher := noopDispatcher{}

	schedule := &application.ScheduleRun{Runs: runs, Dispatcher: dispatcher, Clock: clock}
	cancelRun := &application.CancelRun{Runs: runs, Clock: clock}
	replay := &application.ReplayDiff{
		Diffs:     diffs,
		Sources:   gateway,
		Clock:     clock,
		Policy:    diff.DefaultPolicy{},
		Tolerance: application.DefaultToleranceResolver{},
	}

	return httpapi.Deps{
		Runs: routes.RunsDeps{
			Schedule: schedule,
			Query:    application.QueryRuns{Runs: runs},
			Cancel:   cancelRun,
		},
		Diffs: routes.DiffsDeps{
			Query:  application.QueryDiffs{Diffs: diffs},
			Replay: replay,
		},
		Schedules: routes.SchedulesDeps{
			Schedule:   schedule,
			Query:      application.QuerySchedules{Schedules: schedules},
			Dispatcher: dispatcher,
		},
		Sources: routes.SourcesDeps{Gateway: gateway},
	}
}

// errNoop is the common return for every no-op repo / dispatcher
// method below. Spec generation never triggers these paths, but if
// somebody wires the dump command into a live request flow by
// mistake the error surfaces loudly.
var errNoop = errors.New("openapi-dump: stub dep invoked")

// --- Repositories ---------------------------------------------------

type noopRunRepo struct{}

func (noopRunRepo) Save(context.Context, *verification.Run) error { return errNoop }
func (noopRunRepo) FindByID(context.Context, verification.RunID) (*verification.Run, error) {
	return nil, errNoop
}

func (noopRunRepo) List(context.Context, application.RunFilter) ([]*verification.Run, int, error) {
	return nil, 0, errNoop
}

type noopDiffRepo struct{}

func (noopDiffRepo) Save(context.Context, *diff.Discrepancy, diff.Judgement, application.SaveDiffMeta) (application.DiffID, error) {
	return "", errNoop
}

func (noopDiffRepo) FindByRun(context.Context, verification.RunID) ([]application.DiffRecord, error) {
	return nil, errNoop
}

func (noopDiffRepo) FindByID(context.Context, application.DiffID) (*application.DiffRecord, error) {
	return nil, errNoop
}

func (noopDiffRepo) List(context.Context, application.DiffFilter) ([]application.DiffRecord, int, error) {
	return nil, 0, errNoop
}

func (noopDiffRepo) MarkResolved(context.Context, application.DiffID, time.Time) error {
	return errNoop
}

type noopScheduleRepo struct{}

func (noopScheduleRepo) Save(context.Context, application.ScheduleRecord) error { return errNoop }
func (noopScheduleRepo) Deactivate(context.Context, application.JobID) error    { return errNoop }
func (noopScheduleRepo) ListActive(context.Context) ([]application.ScheduleRecord, error) {
	return nil, errNoop
}

// --- Dispatcher -----------------------------------------------------

type noopDispatcher struct{}

func (noopDispatcher) EnqueueRunExecution(context.Context, verification.RunID) error {
	return errNoop
}

func (noopDispatcher) ScheduleRecurring(context.Context, verification.Schedule, application.SchedulePayload) (application.JobID, error) {
	return "", errNoop
}

func (noopDispatcher) CancelScheduled(context.Context, application.JobID) error { return errNoop }
