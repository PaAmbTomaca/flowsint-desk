# ADR 0002 — Persistencia: restic cifrado en pCloud; prohibido git en el árbol

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11 (fase 2 del encargo)
- **Contexto**: Hay que persistir los datos entre sesiones y entre dos equipos
  (torre y portátil) con una sola llave, sin que queden datos en claro en el
  disco local.

## Decisión

- La persistencia es un **repositorio restic cifrado** en una carpeta de pCloud
  (`~/pCloudDrive/flowsint-vault`). restic cifra, deduplica y versiona.
- Los volcados van **en claro (texto) a un staging en tmpfs** y se pasan a
  restic como directorio; restic cifra después. No cifrar ni comprimir antes:
  rompería la deduplicación.
- **Git está prohibido enteramente en el árbol del proyecto** (lectura y
  escritura): el control de versiones lo lleva quien coordina. Tampoco se usa
  git para la persistencia de datos (no deduplica ficheros cifrados y su
  `.git` se rompe sobre una unidad sincronizada).
- **La BD no vive en pCloud**: Postgres/Neo4j sobre una unidad sincronizada se
  corrompe. En pCloud solo va el repo restic.

## Consecuencias

- **+** Una frase de paso abre todo desde cualquier equipo; deduplicación real
  entre snapshots; nada en claro en el disco local ni en pCloud.
- **−** La frase de paso viaja a restic solo por `RESTIC_PASSWORD_FILE`
  (fichero 0600 en tmpfs, borrado al terminar) o por stdin en pruebas: nunca
  en argv ni en logs.

## Alternativas consideradas

- Git para datos: rechazado (ver arriba).
- BD directamente en pCloud: rechazado (corrupción por sincronización).
- Cifrado previo del volcado: rechazado (rompe deduplicación).

## Referencias

- `ENCARGO.md` §2; `internal/store/`, `internal/snap/`.

## Actualización 2026-11-26

- La prohibición de git en el árbol se retiró el 2026-11-17 por orden del
  operador: el repositorio se publica en GitHub. El resto de la decisión
  (restic en pCloud; la base de datos nunca sobre una unidad sincronizada)
  sigue vigente.
- **Retención**: `forget` se ejecuta con `--group-by ""`. restic agrupa por
  equipo y ruta, y la ruta respaldada lleva el `boot_id`: sin eso, cada arranque
  del PC era un grupo con su propia cuota y casi no se borraba nada.
- La salida `--json` de restic son líneas JSON y sus avisos van por stderr: se
  leen por separado. Antes, un backup lento se daba por fallido.
- El bloqueo entre equipos distingue por identificador de máquina, no solo por
  nombre, y se escribe de forma atómica.
- Segunda copia y comprobación periódica: [ADR 0012](0012-dos-copias-y-verificacion.md).
- `init` sobre un almacén con snapshots conecta el equipo y no crea nada.
