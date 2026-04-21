// Command csw is the project's operator CLI. Subcommands:
//
//   - migrate  — apply / roll back / inspect DB migrations
//   - (more subcommands land as later phases ship, e.g., openapi-dump
//     in Phase 8)
//
// The single-binary convention is intentional: Phase 0 picked
// directory names as binary names, so adding a subcommand here does
// not create a new binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/seokheejang/chain-sync-watch/cmd/csw/cmdmigrate"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "csw:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("missing command")
	}
	switch args[0] {
	case "migrate":
		return cmdmigrate.Run(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: csw <command> [args]

Commands:
  migrate up            Apply all pending DB migrations
  migrate down          Roll back every migration (use with care)
  migrate status        Report current migration version

Environment:
  DATABASE_URL          Postgres DSN, e.g. postgres://user:pass@host:5432/db?sslmode=disable`)
}
