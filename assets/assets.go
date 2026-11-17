// Package assets contiene embebidos el compose y las migraciones Cypher de
// Neo4j (idempotentes, IF NOT EXISTS) que se re-aplican tras restaurar un
// snapshot, porque el export APOC no incluye índices range.
package assets

import "embed"

//go:embed compose.yml
var ComposeYML []byte

//go:embed migrations/*.cypher
var MigrationsFS embed.FS
