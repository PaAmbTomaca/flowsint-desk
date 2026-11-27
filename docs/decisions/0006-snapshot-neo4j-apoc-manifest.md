# ADR 0006 — Método de snapshot de Neo4j (APOC cypher) y verificación por manifest

- **Estado**: aceptada (en vigor, verificado en fase 0)
- **Fecha**: 2026-11
- **Contexto**: El grafo completo vive en Neo4j y hay que volcarlo y restaurarlo
  de forma que Flowsint lo lea **idéntico**. Criterio del encargo: si el grafo
  restaurado no es idéntico, el método no es válido.

## Decisión

- Export con **APOC en línea** (`apoc.export.cypher.all`, formato plain) a
  `neo4j.cypher` en el staging tmpfs; restauración con `cypher-shell -f` en una
  BD vacía (ojo: `cypher-shell` ejecuta UN solo statement por llamada).
- Tras restaurar se **re-aplican las migraciones idempotentes**
  (`assets/migrations/*.cypher`): el export APOC no trae índices range (solo
  constraints).
- Los `elementId` de Neo4j **no sobreviven** al export (UNIQUE IMPORT LABEL);
  es válido porque nada fuera de Neo4j los persiste (verificado en Postgres).
- Cada snapshot lleva `manifest.json` con sha256 de cada fichero y recuentos
  de control (filas por tabla de Postgres, nodos por etiqueta y relaciones por
  tipo en Neo4j). Tras restaurar se comparan: **si no coinciden, no se arranca
  la interfaz**.
- `postgres.sql` = `pg_dump` plano ejecutado dentro del contenedor.
- `neo4j-admin database dump` (offline) se evaluó y descartó para este flujo:
  en Community exige la BD parada y no encaja con la restauración en caliente
  vía cypher-shell; el export APOC quedó verificado como idéntico salvo
  `elementId`.

## Consecuencias

- **+** Restauración verificable y reproducible; deduplicación buena en restic
  (texto plano).
- **−** Hay que re-aplicar migraciones tras cada restore; `cypher-shell` exige
  llamadas sueltas por statement.

## Alternativas consideradas

- `neo4j-admin database dump/load`: descartado (BD parada en Community, no
  encaja con el arranque del compose).
- Volcado binario del data-dir de Neo4j: descartado (no portable entre
  versiones/hosts y no verificable por recuentos).

## Referencias

- `ENCARGO.md` §3 (hechos verificados) y §5; `internal/snap/`; `assets/migrations/`.

## Actualización 2026-11-26

El método no cambia. Se ha endurecido lo que lo rodea:

- `pg_dump` se captura **solo por stdout**, en streaming. Antes, un aviso por
  stderr acababa dentro de `postgres.sql` y el snapshot no restauraba (el sha256
  cuadraba igual, porque se calculaba sobre el fichero ya contaminado).
- Los recuentos de Postgres solo incluyen las tablas que existen: una migración
  de Flowsint que quite una tabla ya no impide hacer snapshots.
- Un recuento ilegible es un error, no un cero.
- Al verificar, cada tabla del manifest debe existir con las mismas filas
  (compatible con los manifests anteriores).
- Un snapshot de datos debe traer sha256 de sus tres ficheros; si falta un
  volcado no se "restaura" una base de datos vacía.
- La verificación de ficheros (`LoadAndVerify`) se comparte con `verify`.
- El manifest anota el digest de las imágenes: [ADR 0011](0011-imagenes-version-digest-cache.md).
- Si la verificación falla, se desmonta todo; no solo se paran los servicios.
