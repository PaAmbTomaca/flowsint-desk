// Package snap implementa el contenido del snapshot y su verificación:
//   - postgres.sql: pg_dump en texto plano (ejecutado dentro del contenedor,
//     la contraseña sale del env del contenedor, nunca de argv)
//   - neo4j.cypher: export APOC online (formato plain) → restaurable con
//     cypher-shell -f en una BD vacía; validado en fase 0 (idéntico salvo elementId)
//   - secrets.env: secretos internos de Flowsint
//   - manifest.json: versión, digests de imágenes, host, fecha, sha256 de los
//     ficheros y recuentos de control (filas por tabla, nodos por etiqueta,
//     relaciones por tipo)
//
// Tras restaurar se comparan sha256 y recuentos con el manifest; si no
// coinciden, NO se arranca la interfaz.
package snap

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowsint-desk/assets"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/run"
)

// Version de la CLI. Las versiones publicadas la fijan al compilar
// (-ldflags "-X flowsint-desk/internal/snap.Version=…").
var Version = "0.1.0-dev"

type Manifest struct {
	Version      string            `json:"version"`
	Kind         string            `json:"kind"` // "init" | "data"
	Created      string            `json:"created"`
	Host         string            `json:"host"`
	BootID       string            `json:"boot_id"`
	ImageDigests map[string]string `json:"image_digests"`
	PGRows       map[string]int    `json:"pg_rows"`
	Neo4jNodes   map[string]int    `json:"neo4j_nodes_by_label"`
	Neo4jRels    map[string]int    `json:"neo4j_rels_by_type"`
	Neo4jTotalN  int               `json:"neo4j_total_nodes"`
	Neo4jTotalR  int               `json:"neo4j_total_rels"`
	SHA256       map[string]string `json:"sha256"`
}

var pgTables = []string{
	"profiles", "investigations", "sketches", "sketches_profiles", "keys",
	"scans", "logs", "flows", "chats", "messages", "analyses", "custom_types",
	"enricher_templates", "investigation_user_roles", "feedbacks",
}

// SHA256File calcula el sha256 hex de un fichero (expuesto para init).
func SHA256File(p string) (string, error) { return sha256File(p) }

func sha256File(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// pgDump vuelca Postgres a staging/postgres.sql (texto plano, sin cifrar:
// restic cifra después y la dedup lo agradece). Solo stdout va al fichero:
// un aviso de pg_dump por stderr dentro del .sql lo haría irrestaurable
// (psql con ON_ERROR_STOP) y no se descubriría hasta el día del restore.
func pgDump(dockerEnv []string, staging string) error {
	if err := dock.RunToFile(dockerEnv, filepath.Join(staging, "postgres.sql"),
		"docker", "exec", "flowsint-desk-postgres",
		"sh", "-c", `pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"`); err != nil {
		return fmt.Errorf("pg_dump: %w", err)
	}
	return nil
}

func psql(dockerEnv []string, query string) (string, error) {
	return dock.Output(dockerEnv, "docker", "exec", "flowsint-desk-postgres",
		"psql", "-U", "flowsint", "-d", "flowsint", "-t", "-A", "-F", "|", "-c", query)
}

// pgCounts devuelve filas por tabla principal. Solo cuenta las tablas de
// pgTables que EXISTEN: las imágenes de Flowsint van por tag `latest` y una
// migración upstream que renombre o quite una tabla no debe dejar al usuario
// sin poder hacer snapshots.
func pgCounts(dockerEnv []string) (map[string]int, error) {
	out, err := psql(dockerEnv, "SELECT tablename FROM pg_tables WHERE schemaname = 'public'")
	if err != nil {
		return nil, fmt.Errorf("pgCounts (tablas): %w", err)
	}
	existe := map[string]bool{}
	for _, t := range strings.Fields(out) {
		existe[t] = true
	}
	var q strings.Builder
	for _, t := range pgTables {
		if !existe[t] {
			continue
		}
		if q.Len() > 0 {
			q.WriteString(" UNION ALL ")
		}
		fmt.Fprintf(&q, "SELECT '%s', count(*) FROM %s", t, t)
	}
	if q.Len() == 0 {
		return map[string]int{}, nil
	}
	out, err = psql(dockerEnv, q.String())
	if err != nil {
		return nil, fmt.Errorf("pgCounts: %w", err)
	}
	return parsePGCounts(out)
}

// parsePGCounts interpreta líneas "tabla|n" (psql -t -A -F '|').
func parsePGCounts(out string) (map[string]int, error) {
	m := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, num, ok := strings.Cut(line, "|")
		if !ok {
			return nil, fmt.Errorf("pgCounts: línea inesperada %q", line)
		}
		n, err := strconv.Atoi(strings.TrimSpace(num))
		if err != nil {
			return nil, fmt.Errorf("pgCounts: recuento ilegible en %q", line)
		}
		m[name] = n
	}
	return m, nil
}

func neo4jShell(dockerEnv []string, cypher string, failFast bool) (string, error) {
	// el user/pass salen del env del contenedor (NEO4J_AUTH), nunca de argv del host
	args := []string{"exec", "flowsint-desk-neo4j", "sh", "-c",
		`cypher-shell --format plain -u "${NEO4J_AUTH%%/*}" -p "${NEO4J_AUTH#*/}" ` +
			map[bool]string{true: "--fail-fast ", false: ""}[failFast] +
			fmt.Sprintf("%q", cypher)}
	return dock.Output(dockerEnv, "docker", args...)
}

// neo4jExport exporta el grafo completo vía APOC (online) y lo copia a
// staging/neo4j.cypher. Todo el intercambio con el import dir va por docker cp
// (el daemon escribe como root del user-ns; ficheros escritos desde el host
// quedan "nobody" dentro del contenedor rootless y neo4j no los lee).
func neo4jExport(dockerEnv []string, runDir, staging string) error {
	tmp := filepath.Join(runDir, "call-export.cypher")
	call := `CALL apoc.export.cypher.all("/var/lib/neo4j/import/neo4j-export.cypher", {format: "plain", stream: false});`
	if err := os.WriteFile(tmp, []byte(call), 0o600); err != nil {
		return err
	}
	if err := dock.CP(dockerEnv, tmp, "flowsint-desk-neo4j:/var/lib/neo4j/import/call-export.cypher"); err != nil {
		return err
	}
	out, err := dock.Run(dockerEnv, "docker", "exec", "flowsint-desk-neo4j", "sh", "-c",
		`cypher-shell --fail-fast -u "${NEO4J_AUTH%%/*}" -p "${NEO4J_AUTH#*/}" -f /var/lib/neo4j/import/call-export.cypher`)
	if err != nil {
		return fmt.Errorf("apoc export: %v: %.300s", err, out)
	}
	dst := filepath.Join(runDir, "neo4j-export.cypher")
	if err := dock.CP(dockerEnv, "flowsint-desk-neo4j:/var/lib/neo4j/import/neo4j-export.cypher", dst); err != nil {
		return fmt.Errorf("recogiendo export APOC: %w", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(staging, "neo4j.cypher"), b, 0o600)
}

// neo4jCounts devuelve nodos por etiqueta, rels por tipo y totales.
// cypher-shell ejecuta UN solo statement por llamada: 4 llamadas separadas.
func neo4jCounts(dockerEnv []string) (nodes, rels map[string]int, totalN, totalR int, err error) {
	nodes = map[string]int{}
	rels = map[string]int{}
	q := func(query string) ([]string, error) {
		out, err := neo4jShell(dockerEnv, query, true)
		if err != nil {
			return nil, fmt.Errorf("neo4jCounts %q: %w", query, err)
		}
		var lines []string
		for _, l := range strings.Split(out, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || l == "count(n)" || l == "count(r)" || strings.HasPrefix(l, "l,") || strings.HasPrefix(l, "type(r),") {
				continue
			}
			lines = append(lines, l)
		}
		return lines, nil
	}
	parseInt := func(s string) int {
		var v int
		fmt.Sscanf(strings.Trim(s, `"`), "%d", &v)
		return v
	}
	lines, err := q(`MATCH (n) RETURN count(n)`)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	for _, l := range lines {
		totalN = parseInt(l)
	}
	lines, err = q(`MATCH (n) UNWIND labels(n) AS l RETURN l, count(n) ORDER BY l`)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	for _, l := range lines {
		rec, err := csv.NewReader(strings.NewReader(l)).Read()
		if err != nil || len(rec) != 2 {
			continue
		}
		nodes[strings.Trim(rec[0], `"`)] = parseInt(rec[1])
	}
	lines, err = q(`MATCH ()-[r]->() RETURN type(r), count(r) ORDER BY type(r)`)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	for _, l := range lines {
		rec, err := csv.NewReader(strings.NewReader(l)).Read()
		if err != nil || len(rec) != 2 {
			continue
		}
		rels[strings.Trim(rec[0], `"`)] = parseInt(rec[1])
	}
	lines, err = q(`MATCH ()-[r]->() RETURN count(r)`)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	for _, l := range lines {
		totalR = parseInt(l)
	}
	return nodes, rels, totalN, totalR, nil
}

// Build prepara el directorio staging con los 4 ficheros + manifest.
func Build(dockerEnv []string, runDir, staging string, digests map[string]string, kind string) (*Manifest, error) {
	if err := os.RemoveAll(staging); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return nil, err
	}
	if err := pgDump(dockerEnv, staging); err != nil {
		return nil, err
	}
	if err := neo4jExport(dockerEnv, runDir, staging); err != nil {
		return nil, err
	}
	// secretos
	secB, err := os.ReadFile(filepath.Join(runDir, "secrets.env"))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(staging, "secrets.env"), secB, 0o600); err != nil {
		return nil, err
	}
	// recuentos
	pg, err := pgCounts(dockerEnv)
	if err != nil {
		return nil, err
	}
	nd, rl, tn, tr, err := neo4jCounts(dockerEnv)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	boot, _ := run.BootID()
	m := &Manifest{
		Version: Version, Kind: "data", Created: time.Now().UTC().Format(time.RFC3339),
		Host: host, BootID: boot, ImageDigests: digests,
		PGRows: pg, Neo4jNodes: nd, Neo4jRels: rl, Neo4jTotalN: tn, Neo4jTotalR: tr,
		SHA256: map[string]string{},
	}
	for _, f := range []string{"postgres.sql", "neo4j.cypher", "secrets.env"} {
		h, err := sha256File(filepath.Join(staging, f))
		if err != nil {
			return nil, err
		}
		m.SHA256[f] = h
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), mb, 0o600); err != nil {
		return nil, err
	}
	_ = kind
	return m, nil
}

// FindFile localiza un fichero por su nombre base bajo root (el restore de
// restic conserva los paths absolutos del staging, que incluyen el boot_id).
func FindFile(root, base string) (string, error) { return findFile(root, base) }

// findFile localiza un fichero por su nombre base bajo root (el restore de
// restic conserva los paths absolutos del staging, que incluyen el boot_id).
func findFile(root, base string) (string, error) {
	found := ""
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == base {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}

// LoadAndVerify lee el manifest de un snapshot ya restaurado en restoreDir y
// comprueba que los ficheros están completos e íntegros (sha256). No necesita
// Docker: lo usan tanto el restore de `up` como `verify`.
func LoadAndVerify(restoreDir string) (*Manifest, error) {
	manifestPath, err := findFile(restoreDir, "manifest.json")
	if err != nil {
		return nil, fmt.Errorf("sin manifest.json en el snapshot: %w", err)
	}
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("sin manifest.json en el snapshot: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("manifest corrupto: %w", err)
	}
	// 1. verificar sha256 de los ficheros restaurados
	for f, want := range m.SHA256 {
		p, err := findFile(restoreDir, f)
		if err != nil {
			return nil, fmt.Errorf("falta %s en el snapshot restaurado", f)
		}
		got, err := sha256File(p)
		if err != nil {
			return nil, err
		}
		if got != want {
			return nil, fmt.Errorf("VERIFICACIÓN FALLIDA: sha256 de %s no coincide con el manifest (snapshot corrupto)", f)
		}
	}
	// un snapshot con datos DEBE traer los dos volcados, estén o no listados
	// en el manifest: si faltan, no se arranca una BD vacía haciéndola pasar
	// por restaurada.
	if m.Kind != "init" {
		for _, f := range []string{"postgres.sql", "neo4j.cypher", "secrets.env"} {
			if _, ok := m.SHA256[f]; !ok {
				return nil, fmt.Errorf("VERIFICACIÓN FALLIDA: el manifest no trae sha256 de %s (snapshot incompleto)", f)
			}
		}
	}
	return &m, nil
}

// RestoreInto restaura el snapshot `id` (ya descargado en restoreDir) dentro
// de los contenedores db vivos, aplica las migraciones Neo4j y verifica el
// manifest. Devuelve error si algo no cuadra (entonces NO se arranca la UI).
func RestoreInto(dockerEnv []string, runDir, restoreDir string) error {
	mp, err := LoadAndVerify(restoreDir)
	if err != nil {
		return err
	}
	m := *mp
	// 2. restaurar Postgres (BD vacía recién creada) vía stdin, sin argv secreto
	//    (un snapshot "init" no trae postgres.sql: se salta)
	if pgPath, err := findFile(restoreDir, "postgres.sql"); err == nil {
		pgFile, err := os.Open(pgPath)
		if err != nil {
			return err
		}
		defer pgFile.Close()
		if err := dock.ExecStdin(dockerEnv, pgFile, "docker", "exec", "-i", "flowsint-desk-postgres",
			"psql", "-U", "flowsint", "-d", "flowsint", "-v", "ON_ERROR_STOP=1"); err != nil {
			return fmt.Errorf("restaurando postgres: %w", err)
		}
	}
	// 3. restaurar Neo4j (BD vacía) + migraciones idempotentes
	if cyPath, err := findFile(restoreDir, "neo4j.cypher"); err == nil {
		if err := dock.CP(dockerEnv, cyPath, "flowsint-desk-neo4j:/var/lib/neo4j/import/neo4j-restore.cypher"); err != nil {
			return err
		}
		if out, err := dock.Run(dockerEnv, "docker", "exec", "flowsint-desk-neo4j", "sh", "-c",
			`cypher-shell --fail-fast -u "${NEO4J_AUTH%%/*}" -p "${NEO4J_AUTH#*/}" -f /var/lib/neo4j/import/neo4j-restore.cypher`); err != nil {
			return fmt.Errorf("restaurando neo4j: %v: %.300s", err, out)
		}
	}
	// migraciones (idempotentes, IF NOT EXISTS): el export APOC no trae índices range
	entries, err := assets.MigrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".cypher") {
			continue
		}
		b, err := assets.MigrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tmp := filepath.Join(runDir, "mig-"+e.Name())
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		if err := dock.CP(dockerEnv, tmp, "flowsint-desk-neo4j:/var/lib/neo4j/import/"+e.Name()); err != nil {
			return err
		}
		if out, err := dock.Run(dockerEnv, "docker", "exec", "flowsint-desk-neo4j", "sh", "-c",
			`cypher-shell --fail-fast -u "${NEO4J_AUTH%%/*}" -p "${NEO4J_AUTH#*/}" -f /var/lib/neo4j/import/`+e.Name()); err != nil {
			return fmt.Errorf("migración neo4j %s: %v: %.300s", e.Name(), err, out)
		}
		_ = os.Remove(tmp)
	}
	// 4. verificar recuentos contra el manifest (un snapshot "init" no lleva datos)
	if m.Kind != "init" {
		pg, err := pgCounts(dockerEnv)
		if err != nil {
			return err
		}
		nd, rl, tn, tr, err := neo4jCounts(dockerEnv)
		if err != nil {
			return err
		}
		if dif := pgMismatch(m.PGRows, pg); dif != "" {
			return fmt.Errorf("VERIFICACIÓN FALLIDA: filas Postgres no cuadran con el manifest (%s) — NO se arranca la interfaz", dif)
		}
		if !mapsEqual(nd, m.Neo4jNodes) || !mapsEqual(rl, m.Neo4jRels) || tn != m.Neo4jTotalN || tr != m.Neo4jTotalR {
			return fmt.Errorf("VERIFICACIÓN FALLIDA: neo4j nodos=%v total=%d rels=%v total=%d != manifest %v/%d %v/%d — NO se arranca la interfaz",
				nd, tn, rl, tr, m.Neo4jNodes, m.Neo4jTotalN, m.Neo4jRels, m.Neo4jTotalR)
		}
	}
	// 5. los secretos vuelven del snapshot
	secPath, err := findFile(restoreDir, "secrets.env")
	if err != nil {
		return fmt.Errorf("sin secrets.env en el snapshot: %w", err)
	}
	secB, err := os.ReadFile(secPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "secrets.env"), secB, 0o600); err != nil {
		return err
	}
	return nil
}

// pgMismatch compara los recuentos del manifest con los restaurados: cada
// tabla del manifest debe existir tras restaurar y tener las mismas filas.
// Devuelve "" si cuadra, o la lista de diferencias.
func pgMismatch(want, got map[string]int) string {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var difs []string
	for _, k := range keys {
		g, ok := got[k]
		switch {
		case !ok:
			difs = append(difs, fmt.Sprintf("%s: falta (manifest %d)", k, want[k]))
		case g != want[k]:
			difs = append(difs, fmt.Sprintf("%s: %d (manifest %d)", k, g, want[k]))
		}
	}
	return strings.Join(difs, ", ")
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
