// Package migrations embeds the canonical SQL schema (single source of truth).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
