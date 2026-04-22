// Package cmdopenapi implements the `csw openapi-dump` subcommand.
// Its only job is to serialise the HTTP server's OpenAPI 3.1
// document to stdout so the frontend codegen step (or a CI drift
// check) can consume a committed spec file.
package cmdopenapi

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
)

// Run dispatches the `csw openapi-dump` subcommand. Accepted flags:
//
//	--format=json|yaml  (default: json)
//	--output=path       (default: stdout)
//
// The command instantiates a server with a zero Deps struct — the
// OpenAPI document reflects every route a Deps=zero server would
// expose, which today is the full resource surface because each
// Register* function emits its routes unconditionally (routes that
// need optional deps are registered when those deps are nil-safe).
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

	deps := httpapi.Deps{}

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
