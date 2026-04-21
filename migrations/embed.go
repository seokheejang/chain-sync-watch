// Package migrations exposes the SQL migration files as an embedded
// filesystem. csw-server and the csw migrate CLI both read from here,
// so the binary never needs to be shipped alongside a migrations
// directory on disk.
package migrations

import "embed"

// FS holds every migration SQL file. File names follow the
// golang-migrate convention: NNN_description.{up,down}.sql.
//
//go:embed *.sql
var FS embed.FS
