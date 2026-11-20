# Auditoría de seguridad severa — flowsint-desk (2026-11-20)

- **Alcance**: CLI `flowsint-desk`, compose embebido, ciclo de vida de sesión,
  snapshots/restic, exposición de red, secretos, logs, entorno de la máquina.
- **Método**: revisión de código + pruebas ejecutadas (fase 4) + verificación en
  la máquina (`/proc/swaps`, `assets/compose.yml`) + búsqueda de amenazas.
  Cada hallazgo trae evidencia (fichero:línea o comando → salida).
- **Severidad**: Crítica / Alta / Media / Baja / Info.

## Resumen ejecutivo

| # | Severidad | Hallazgo | Estado |
|---|-----------|----------|--------|
| A1 | **Alta** | Imágenes por tag mutable (`latest`/`15-alpine`) sin pin de digest → riesgo de supply chain | pendiente (propuesta) |
| A2 | **Alta** | `api`/`celery` montan el socket rootless → RCE en Flowsint = lectura de todo el home del usuario (montando `/`) | pendiente (propuesta: socket-proxy filtrado) |
| M1 | Media | Swap podría filtrar tmpfs a disco en claro — **verificado que hoy es zram (RAM)**, no disco | mitigado hoy; documentar "no swap en disco" |
| M2 | Media | `doctor --fix` descarga paquetes Arch sin verificar firma PGP (solo HTTPS); restic sí verifica sha256 | pendiente (propuesta) |
| M3 | Media | Frase de paso mínima de 8 caracteres (baja entropía posible) | pendiente (recomendar ≥ 16–20) |
| B1 | Baja | pCloud: el proveedor ve metadatos (tamaños, nº/frecuencia de snapshots, lock host/pid); el contenido va cifrado | aceptado (documentado) |
| B2 | Baja | Bloqueo entre equipos best-effort (pCloud no atómico) | aceptado (documentado, ADR-0002/lock) |
| V1–V8 | Info ✔ | Controles verificados en verde (ver abajo) | hechos |

## Hallazgos detallados

### A1 — Imágenes sin pin de digest (supply chain) — ALTA
**Evidencia**: `assets/compose.yml` usa tags mutables:
```
8:    image: postgres:15-alpine
27:    image: redis:7-alpine
42:    image: neo4j:5
68:    image: ghcr.io/reconurge/flowsint-api:latest
97:    image: ghcr.io/reconurge/flowsint-api:latest
127:   image: ghcr.io/reconurge/flowsint-app:latest
```
Si el registro upstream (ghcr.io/reconurge) se compromete o republica un tag,
`up` descarga y ejecuta la imagen maliciosa con acceso al socket rootless (ver A2).
El `manifest.json` guarda digests, pero solo para verificar *datos* tras el
restore, no la integridad de la imagen antes de ejecutar.
**Mitigación propuesta**: fijar `image: repo@sha256:<digest>` en el compose
embebido (con actualización controlada vía `doctor`/CI), y que `doctor` alerte
si el digest local difiere del esperado. Es un cambio de 6 líneas + proceso de
actualización.

### A2 — Socket Docker rootless montado en api/celery — ALTA
**Evidencia**: `assets/compose.yml:80,111`:
```
- ${DESK_RUN}/docker.sock:/var/run/docker.sock:ro
```
y `DOCKER_HOST=unix:///var/run/docker.sock` en api y celery (líneas 78, 109).
Los enrichers (subfinder/naabu/httpx) lanzan contenedores desde ahí.
**Impacto**: cualquier RCE en la API de Flowsint o en un enricher (o un prompt
injection que haga que la API lance un contenedor arbitrario) puede crear un
contenedor con `-v /home/pollo:/host` (o montar `/`) dentro del mismo user-ns
rootless y leer **todo lo que el usuario puede leer**: `~/.config`, `~/.ssh`,
el pCloud montado (repo cifrado, pero también sesión del navegador, etc.).
El socket rootless no da root del host, pero sí equivale a "todo el usuario".
**Mitigación propuesta**: interponer un **socket-proxy** (p. ej.
tecnativa/docker-socket-proxy) que solo permita los endpoints que usan los
enrichers (`/containers/create`, `/containers/*/start`, `/images/*`, …) y
deniegue `/containers/create` con mounts arbitrarios; o pasar a un socket por
sesión con política. Hasta entonces, documentado como riesgo aceptado de v1
(Flowsint necesita el socket para los enrichers; la exposición de red es solo
loopback, lo que reduce mucho la superficie remota).

### M1 — Swap y tmpfs — MEDIA (mitigado hoy, documentar)
**Evidencia**: `cat /proc/swaps` →
```
/dev/zram0  partition  32534524  1277164  100
```
Hoy el swap es **zram (RAM comprimida)**: las páginas de tmpfs que swapee van a
RAM, no a disco → no hay fuga en claro. **Peligro latente**: si se configura
swap en un disco/partición real, las páginas de `/tmp` (tmpfs) podrían escribirse
en claro en el disco de swap. **Recomendación**: documentar "no usar swap en
disco" (o cifrar el swap) en el README de seguridad.

### M2 — `doctor --fix` descarga sin firma verificable — MEDIA
**Evidencia**: `internal/cli/doctor.go` descarga `*.pkg.tar.zst` de
`geo.mirror.pkgbuild.com` y `dockerd-rootless.sh` de GitHub raw con HTTPS pero
**sin verificar firma PGP** (solo restic comprueba su `.sha256` oficial).
Si el mirror se compromete, instala binarios en `~/.local/bin`.
**Mitigación propuesta**: descargar también el `.sig` y verificar con la clave
pública de Arch (o fijar hashes sha256 conocidos en el código).

### M3 — Política de frase de paso — MEDIA
**Evidencia**: `passphrase.Prompt(label, minLen)` se llama con `minLen = 8`
(`passphrase.go`). restic usa scrypt (bien), pero 8 caracteres pueden tener
baja entropía (p. ej. una palabra corta). **Recomendación**: subir el mínimo a
16–20 y mostrar un aviso de entropía estimada; no romper repos existentes.

### B1 — Metadatos visibles en pCloud — BAJA (aceptado)
El proveedor de pCloud ve tamaños de snapshots, frecuencia, nº de snapshots y
el `lock.json` (host, pid, boot_id, latidos). El contenido va cifrado con la
frase. Aceptado: es el modelo de confianza elegido (ADR-0002).

### B2 — Bloqueo best-effort — BAJA (aceptado)
pCloud no es atómico; el lock con latido de 15 min protege a una persona con
dos equipos, no concurrencia real (documentado en ADR-0002 y README).

## Controles verificados en verde (con evidencia de la fase 4)

- **V1** Exposición mínima: solo `127.0.0.1:5273` publicado; ni api/neo4j/pg/redis
  salen de loopback (`assets/compose.yml`); Redis `--save "" --appendonly no`.
- **V2** Datos en claro solo en tmpfs: `statfs`/`TMPFS_MAGIC` antes de escribir
  (`internal/run`); data-root, volúmenes pg/neo4j y logs de contenedores medidos
  en tmpfs durante una sesión viva; al cerrar, run dir borrado y 0 procesos.
- **V3** No secretos en argv ni logs: frase por `RESTIC_PASSWORD_FILE` 0600 en
  tmpfs; `Redact` enmascara secretos/frase en avisos y `docker logs` (test con
  clave de sonda + verificación en vivo: 0 secretos en la salida de `logs`).
- **V4** Anti-root: la CLI sale con error si `euid==0` (ADR-0007); sin sudo en
  toda la CLI (`setup` solo imprime los pasos sudo).
- **V5** Docker rootful del sistema intocado: todo por `DOCKER_HOST` al socket
  rootless; prueba 9 de la fase 4 (foto de control `docker ps` sin DOCKER_HOST).
- **V6** Snapshot cifrado: 0 fugas en claro en el vault (restic) ni en el disco
  local, con control positivo; verificación por `manifest.json` (sha256 +
  recuentos) tras cada restore; si no cuadra, no se arranca la UI.
- **V7** Recovery: tras `kill -9`, el siguiente `up` mata huérfanos, barre restos
  del mismo boot_id y restaura el último snapshot (probado).
- **V8** Bloqueo entre equipos: niega con latido fresco, avisa si caducado,
  `unlock --force` con confirmación `SI` (probado).

## Recomendaciones priorizadas

1. **A2**: socket-proxy filtrado para los enrichers (o al menos documentar el
   riesgo y desactivar enrichers que no se usen).
2. **A1**: pin de digest `sha256:` en las 6 imágenes del compose embebido.
3. **M2**: verificar firmas/hashes de lo que descarga `doctor --fix`.
4. **M3**: mínimo de frase a 16–20 caracteres + aviso de entropía.
5. **M1**: documentar "no swap en disco" (hoy zram, seguro).

## Referencias

- `assets/compose.yml` (imágenes, socket, puertos, redis).
- `internal/cli/root.go` (anti-root), `internal/run` (statfs), `internal/cli/logs.go`
  + `internal/secrets` (Redact), `internal/store` (RESTIC_PASSWORD_FILE),
  `internal/lock`, `internal/cli/doctor.go` (--fix).
- Pruebas de la fase 4 (informes de sesión): 9/9 + vault real + passwd/forget/logs.
- `docs/decisions/0001..0007`, `docs/proposals/P001`, `docs/research/2026-11-podman-deep-research.md`.
