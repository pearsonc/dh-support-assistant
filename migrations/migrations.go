// Package migrations embeds the SQL migration files so they ship inside
// the compiled server binary. [Domain: goose migrations] pre-flight: the
// goose runner is invoked inside the app container at startup against the
// postgres service on the internal support-net; embedding keeps the
// migrations in the same artefact as the binary that applies them, so no
// volume mount or file-copy step is needed in the Docker build.
//
// The Go package lives alongside the .sql files because //go:embed can
// only reach files at or below its own source directory. internal/db
// imports this package as the FS source for the goose runner.
package migrations

import "embed"

// FS exposes every *.sql file in this directory at the root of the embed
// filesystem. Consumers pass "." as the goose directory argument.
//
//go:embed *.sql
var FS embed.FS
