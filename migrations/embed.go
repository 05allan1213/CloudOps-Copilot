// Package migrations embeds the reviewed V2 SQL migrations for the migrate command.
package migrations

import "embed"

// FS contains all versioned SQL migration files.
//
//go:embed *.sql
var FS embed.FS
