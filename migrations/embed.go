// Package migrations embeds the immutable forward SQL history and exposes the
// single schema version expected by migration and runtime readiness gates.
package migrations

import "embed"

const LatestVersion int64 = 11

// FS contains all versioned SQL migration files.
//
//go:embed *.sql
var FS embed.FS
