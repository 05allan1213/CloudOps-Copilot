// Package migrations embeds the immutable forward SQL history.
package migrations

import "embed"

// FS contains all versioned SQL migration files.
//
//go:embed *.sql
var FS embed.FS
