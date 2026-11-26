# AGENTS.md — flowsint-desk

CLI en Go (`flowsint-desk`) que levanta Flowsint en Docker rootless, efímero en tmpfs,
con snapshots cifrados (restic) en pCloud. La especificación completa está en
`ENCARGO.md`; el manual de uso en `README.md`. `flowsint/` es la referencia upstream de
**solo lectura** (tiene `.git`: no lo toques con ninguna operación git).

## Reglas duras de operación (incidentes reales del encargo)

- ~~**Git prohibido enteramente**~~ **RETIRADO POR ORDEN DEL OPERADOR DIRECTA (2026-11-17)**: el agente
  puede publicar este árbol en el repo remoto `github.com/PaAmbTomaca/flowsint-desk`.
- **Prohibido sudo.** Prohibido tocar el Docker rootful del sistema: la única excepción es
  `docker ps` SIN `DOCKER_HOST` (foto de control). Todo lo demás usa
  `DOCKER_HOST=unix:///tmp/flowsint-desk-<boot_id>/docker.sock` (ver `internal/dock`).
- **Prohibido ejecutar flowsint-desk como root** (la CLI se niega con euid=0). Prohibido `sudo flowsint-desk ...`.
  [Añadido 2026-11-17, orden del operador]
- **HARDRULE — commits granulares**: un commit por cambio lógico, siempre con mensaje que diga
  QUÉ y POR QUÉ; si un cambio lo necesita, engrosar `docs/` (ADR en `docs/decisions/` o
  `docs/proposals/`) con el porqué. Prohibidos los commits masivos de "varios cambios".
  [Añadido 2026-11-17, orden del operador]
- **Nunca secretos en argv ni impresos** (frase de paso, contraseñas, claves). A restic la
  frase le llega por `RESTIC_PASSWORD_FILE` (fichero 0600 en tmpfs). En pruebas, pasar la
  frase por **stdin** (pipe), nunca por argumento.
- **No hacer `cat` de ficheros `.env`**: los valores quedan en claro en los logs del harness
  (`opencode.db`). Usar `grep -c`/`-rl` (solo conteos/nombres) o comparaciones IGUAL/DISTINTO.
- Antes de afirmar un "0 resultados", demostrar con control positivo (señuelo) que el mismo
  comando encuentra lo que sí existe.
- No usar `pkill -f`/`pgrep -f` (se matan a sí mismos). Matar por pid o escanear `/proc/*/cmdline`.
- Si algo falla, **no revertir**: describir y parar.
- Antes de escribir datos en claro, comprobar con `statfs` que el path es tmpfs (`internal/run`).
- El proceso fija `umask 077` y `RLIMIT_CORE=0` solo (root.go).

## Estructura

- `main.go` → `internal/cli` (cobra: doctor, setup, init, up, down, status, backup, snapshots, unlock, forget, passwd, logs)
- `internal/{config,passphrase,run,secrets,dock,session,store,snap,lock}`
- `assets/`: `compose.yml` embebido (se escribe en el run dir en cada `up`) y
  `assets/migrations/*.cypher` (migraciones Neo4j idempotentes, re-aplicadas tras restore)
- `dist/`: binario compilado y `dist/vault-test` (almacén de pruebas)
- Datos en claro solo en `/tmp/flowsint-desk-<boot_id>/` (tmpfs, se borra al cerrar).
  Persistente en disco: `~/.config/flowsint-desk/config.toml` (0600, SIN secretos),
  `~/.cache/flowsint-desk/images/*.tar` (imágenes públicas) y el repo restic cifrado en
  `~/pCloudDrive/flowsint-vault`. La caché local de restic va a tmpfs (`RESTIC_CACHE_DIR`).

## Comandos de desarrollo

```sh
go build -o dist/flowsint-desk .
go vet ./...
go test ./...   # sin Docker ni restic; el test del llavero usa una clave de SONDA y se salta si no hay Secret Service
```

CI (`.github/workflows/ci.yml`): build + vet + test en cada push. Los tests
unitarios NO sustituyen a la batería de aceptación de `ENCARGO.md` §7.

## Uso del producto

```sh
./dist/flowsint-desk doctor
./dist/flowsint-desk init --vault ~/pCloudDrive/flowsint-vault   # una vez; frase 2 veces, opcional llavero
./dist/flowsint-desk up        # restaura último snapshot, abre http://127.0.0.1:5273
./dist/flowsint-desk backup | snapshots | status
./dist/flowsint-desk down      # snapshot final + limpieza total
```

La frase está guardada en el llavero (Secret Service vía go-keyring) tras el último `init`;
si no, se pide por terminal. Puerto por defecto 5273 (configurable en config.toml).

## Trampas medidas (no las redescubras)

- `rm -rf` desde el host **no puede borrar** el data-root rootless (ficheros de uids mapeados
  por subuid → "Permiso denegado"). La limpieza se hace con UN contenedor alpine dentro del
  propio user-ns: `dock.WipeSubdirs` (en una sola ejecución; borrar el data-root por partes
  corrompe el daemon vivo).
- Ficheros escritos por el host en un bind-mount aparecen como `nobody` dentro del contenedor
  rootless y el proceso del contenedor no los lee. Para el import dir de Neo4j usar SIEMPRE
  `docker cp` (`dock.CP`).
- `cypher-shell` ejecuta UN solo statement por llamada: nada de multi-statement como argumento
  (`internal/snap.neo4jCounts` hace 4 llamadas).
- Los secretos del snapshot hay que restaurarlos **antes** de crear los contenedores de BD:
  si no, Neo4j crea `auth.ini` con el pass provisional y el healthcheck falla al cambiar el env.
- `restic restore` conserva los paths absolutos del staging (incluyen el boot_id): localizar
  ficheros por nombre base con `snap.FindFile`.
- go-keyring usa la colección **login** y atributo `username`; `secret-tool store` usa
  "default"/`account`: no interoperan. Guardar siempre con `passphrase.SaveKeyring`.
- `restic key add` en 0.19 imprime `saved new key with ID …` (no JSON).
- `kill -9` a `up` deja el daemon huérfano ocupando el puerto: el siguiente `up` mata los
  huérfanos escaneando `/proc/*/cmdline` por `/tmp/flowsint-desk-` ANTES de comprobar el puerto.
- El compose prod de Flowsint NO vale tal cual: publica 5173 en 0.0.0.0, monta el socket
  rootful y deja Redis con RDB. El embebido ya lo corrige (solo `127.0.0.1:${DESK_PORT}:8080`,
  redis `--save "" --appendonly no`, socket rootless + `DOCKER_HOST` en api/celery).
- Healthcheck de Neo4j: `NEO4J_USERNAME` queda vacío dentro del contenedor; derivar user/pass
  de `$NEO4J_AUTH` (`${NEO4J_AUTH%%/*}` / `${NEO4J_AUTH#*/}`). Celery: el healthcheck del
  Dockerfile (curl) falla siempre → `healthcheck: disable: true` en el compose embebido.
- `dockerd-rootless.sh` exige `HOME` en el entorno (ver `dock.Env`).
- Export APOC de Neo4j no trae índices range (solo constraints) → se re-aplican
  `assets/migrations/*.cypher` tras restaurar. Los `elementId` no sobreviven al export
  (UNIQUE IMPORT LABEL): vale porque nada en Postgres los persiste (verificado).
- Snapshots: `pg_dump` plano + APOC cypher plain + `secrets.env` + `manifest.json`
  (sha256 + recuentos pg/neo4j). Tras restaurar se verifican; si no cuadran, NO se arranca la UI.

- **Cierre**: si el snapshot final falla, `up` NO desmonta (se perdería el trabajo sin
  guardar); solo cierra sin guardar si la orden se repite antes de `closeGrace` (2 min).
  No "simplifiques" eso a un aviso + teardown.
- **Arranque**: las señales se registran al entrar en `up` y `session.json` se escribe
  desde el principio con `fase: arrancando`. Cualquier error tras tomar el bloqueo pasa
  por `teardown()` (defer). Todo desmontaje va por `teardown()`: no hay otro camino.
- **restic-pass**: `<runDir>/restic-pass` es de la sesión `up`. Los demás comandos usan
  `openStore()` (reutiliza el de la sesión viva sin reescribirlo, o `restic-pass.<pid>`
  que se borra al salir). No escribir ahí desde otro comando.
- `restic backup --json` y `restic ls --json` son **JSON Lines**; restic avisa por
  stderr. Parsear solo stdout, línea a línea (`internal/store`). Códigos de salida:
  10 = no hay repo, 12 = frase incorrecta.
- `restic forget` agrupa por host+paths y el path del staging lleva el `boot_id`:
  siempre `--group-by ""` o la retención no borra nada.
- `init` sobre un almacén con snapshots NO crea snapshot (taparía los datos con uno
  vacío: `up` restaura el último).
- El bloqueo distingue equipos por `equipo` (hash de `/etc/machine-id`), no solo por
  hostname. Para simular otro equipo en pruebas basta escribir un `lock.json` con otro
  `host` u otro `equipo`.
- `/tmp` es de todos: antes de usar o limpiar un directorio `flowsint-desk-*` comprobar
  `run.Private()` (directorio real, nuestro, 0700).
- Capturar con stderr separado todo lo que se guarde o se parsee (`dock.RunToFile`,
  `dock.Output`): un aviso de `pg_dump` dentro del `.sql` lo hace irrestaurable.

## Entorno ya preparado en esta máquina (sin sudo)

`~/.local/bin/`: restic 0.19.1, rootlesskit 3.3.0, slirp4netns 1.3.7, fuse-overlayfs 1.18,
`dockerd-rootless.sh`. `/etc/subuid` ya tiene `pollo:100000:65536`. pCloud montado en
`~/pCloudDrive` (FUSE). Go 1.27.3, docker compose 5.7.1. Llavero Secret Service activo.
