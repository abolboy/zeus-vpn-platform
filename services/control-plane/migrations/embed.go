package migrations

import "embed"

// Files contains the ordered SQL migration files. Migration application is handled
// by the explicit zeus-migrate command, never implicitly during API startup.
//
//go:embed *.sql
var Files embed.FS
