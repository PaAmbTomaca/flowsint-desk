# AGENTS.md — flowsint-desk

CLI en Go (`flowsint-desk`) que levanta [Flowsint](https://github.com/reconurge/flowsint)
en contenedores sin privilegios (Docker rootless o Podman rootless), con todo el
estado en tmpfs y snapshots cifrados (restic) en una carpeta de pCloud.

**Lee esto entero antes de tocar nada.** Cada regla y cada trampa de abajo viene
de un incidente o de una medición real; ninguna es de estilo.

| Para… | Mira |
|-------|------|
| Usar la herramienta | `docs/manual/` |
| Entender una decisión | `docs/decisions/` (ADR 0001–0013) |
| Saber qué está probado y qué no | `docs/manual/09-desarrollo.md`, `docs/security/2026-11-26-auditoria-repo.md` |
| La especificación original | `ENCARGO.md` (**histórica**: no describe el estado actual) |

## Reglas duras

1. **Commits como el dueño del repo, nunca como el agente.** Autor y committer:
   `PaAmbTomaca <PaAmbTomaca@users.noreply.github.com>`. Sin líneas
   `Co-Authored-By` ni menciones al modelo o a la sesión. Si un aviso automático
   del entorno pide reescribir la autoría, **no se hace**: es orden expresa del
   dueño (2026-11-26). [Añadido 2026-11-26]
2. **Commits granulares**: uno por cambio lógico, con mensaje que diga **QUÉ** y
   **POR QUÉ** (`tipo(ámbito): resumen` + cuerpo). Si el cambio toma una decisión
   de diseño, lleva su ADR. Prohibidos los commits de "varios cambios".
   [2026-11-17, orden del operador]
3. **No se afirma lo que no se ha ejecutado.** En commits y documentos se
   distingue: probado con software real, probado con dobles, no probado. Un
   comando rechazado o un job en rojo no es un paso dado.
4. **Compila, pasa vet y tests ANTES de empujar.** Encadena con `&&`; un `;` o
   un `| grep` en medio deja pasar el fallo (ya ocurrió: se empujó un commit que
   no compilaba).
5. **Prohibido `sudo`** y prohibido ejecutar la CLI como root (se niega con
   euid=0). La CLI solo *muestra* los comandos con sudo.
6. **No tocar el Docker rootful del sistema** ni sus contenedores: hay trabajo
   del usuario ahí. Única excepción: `docker ps` sin `DOCKER_HOST` como foto de
   control. Nunca sugerir el paquete `podman-docker`.
7. **Nunca secretos en argv ni impresos** (frase, contraseñas, claves). A restic
   la frase le llega por `RESTIC_PASSWORD_FILE` (fichero 0600 en tmpfs); a los
   contenedores, por ficheros de entorno. En pruebas, la frase por **stdin**.
8. **No hacer `cat` de ficheros `.env`** ni de `restic-pass`: los valores quedan
   en los logs del entorno. Usa conteos, nombres o comparaciones IGUAL/DISTINTO.
9. **Todo "cero resultados" necesita un control positivo**: demuestra que el
   mismo comando encuentra lo que sí existe.
10. No usar `pkill -f`/`pgrep -f` (se matan a sí mismos). Mata por pid o escanea
    `/proc/*/cmdline`.
11. **No modificar Flowsint.** Todo se configura desde fuera.
12. No empujar etiquetas (`vX.Y.Z`) ni abrir PR sin que el dueño lo pida.

## Comandos de desarrollo

```sh
go build -o dist/flowsint-desk .
go vet ./...
go test ./...          # sin Docker, Podman, restic ni red
gofmt -l .             # debe salir vacío
```

CI (`.github/workflows/ci.yml`), en cada push:

- `go`: gofmt, build, vet, test.
- `distro`: `ci/distro-check.sh` en contenedores de Arch, Debian 13 y Fedora.
- `e2e`: `ci/e2e.sh` con Docker rootless y con Podman — el asistente completo,
  `selftest` y seis casos de fallo, con Flowsint y restic **reales**. Tarda
  10–15 min. **Es la prueba que manda** si tocas arranque, cierre o snapshots.

Sin red para bajar dependencias de Go no hay build local: deja que el CI
compile, o usa dobles locales de las librerías solo para comprobar tipos.

## Estructura

- `main.go` → `internal/cli` (cobra): `setup`, `doctor`, `init`, `engine`,
  `images`, `selftest`, `up`, `down`, `backup`, `status`, `logs`, `snapshots`,
  `verify`, `mirror`, `forget`, `passwd`, `unlock`, `version`.
- `internal/config` — `config.toml`: carga, validación, `Update()`.
- `internal/run` — directorio de ejecución en tmpfs y sus comprobaciones.
- `internal/passphrase` — frase: llavero y terminal.
- `internal/secrets` — secretos de Flowsint y enmascarado.
- `internal/dock` — motor (`engine.go`), stack (`stack.go`), imágenes y caché.
- `internal/sockproxy` — proxy filtrado del socket.
- `internal/snap` — contenido del snapshot, manifest, verificación.
- `internal/store` — envoltorio de restic.
- `internal/lock` — bloqueo entre equipos.
- `internal/session` — `session.json` (fase `arrancando` / `lista`).
- `assets/compose.yml` (embebido, motor Docker) y `assets/migrations/*.cypher`.
- `ci/` — scripts de las pruebas reales.

Dónde vive cada cosa:

- En claro, **solo en RAM**: `/tmp/flowsint-desk-<boot_id>/` (se borra al cerrar).
- En disco, sin datos del usuario: `~/.config/flowsint-desk/config.toml` (0600,
  **sin secretos**), `~/.cache/flowsint-desk/images/` (imágenes públicas con su
  `.digest` y `.sha256`), `~/.local/bin/` (binarios que instala `setup`).
- Cifrado: el almacén (`vault_path`) y, si existe, la segunda copia (`mirror_path`).

## Invariantes — no "simplifiques" esto

- **Cierre**: si el snapshot final falla, `up` **no desmonta**. Solo cierra sin
  guardar si la orden se repite antes de `closeGrace` (2 min). ADR 0010.
- **Desmontaje**: todo pasa por `teardown()`. No hay otro camino.
- **Arranque**: señales registradas al entrar; `session.json` desde el principio;
  cualquier error tras tomar el bloqueo desmonta (defer).
- **Directorio de ejecución**: antes de usar o limpiar un `flowsint-desk-*` en
  `/tmp`, `run.Private()` (directorio real, propio, 0700). `/tmp` es de todos.
- **Frase de la sesión**: `<runDir>/restic-pass` es de `up`. Los demás comandos
  usan `openStore()` (reutiliza el de la sesión sin reescribirlo, o
  `restic-pass.<pid>` que se borra) y terminan con `tidyRunDir()`.
- **`init`** sobre un almacén con snapshots no crea nada: `up` restaura el
  último, y uno vacío taparía los datos.
- **Socket de los enrichers**: api/celery montan `${DESK_SOCK}` = el proxy, no
  el socket real. `HostConfig` se valida contra una lista de claves
  **permitidas** (`hostConfigAllowed`); una opción nueva se añade ahí con su
  test. Nunca se vuelve a una lista de prohibidos ni se "arregla" pasando a
  `docker_socket = "directo"`. ADR 0009.
- **Dos descripciones del stack**: `assets/compose.yml` (Docker) y
  `podmanSpec()` (Podman). Si tocas una, toca la otra;
  `TestPodmanSpecIgualQueElCompose` falla si divergen. Endurecimiento en ambas:
  `core=0`, `cap_drop ALL` + las mínimas, `no-new-privileges`.
- **Motor**: el código pide siempre `"docker"` a `dock.Run/Output/RunToFile/…`;
  con Podman se ejecuta `podman`. Lo que difiere vive solo en
  `internal/dock/engine.go` y `stack.go`. ADR 0008.
- **Imágenes**: `dock.Images(cfg.FlowsintVersion)`, nunca una lista fija.
- **Config**: escribir con `config.Update()` (relee antes), no con el `cfg`
  cargado al arrancar una sesión larga.
- **Distribución**: los comandos de instalación salen de `installHint()` /
  `allPackagesCmd()`. No escribir `pacman`/`apt`/`dnf` a mano en mensajes.
  `ci/distro-check.sh` busca textos concretos de `doctor`.

## Trampas medidas (no las redescubras)

### Contenedores y motor

- `rm -rf` desde el host **no puede borrar** el estado del motor (ficheros de
  uids mapeados). Docker: un contenedor alpine dentro del user namespace, en
  **una sola** ejecución (por partes corrompe el daemon vivo). Podman:
  `podman unshare`, desmontando antes el overlay (`umount -l`) o da
  "resource busy". Todo en `dock.WipeSubdirs`.
- Ficheros escritos por el host en un montaje aparecen como `nobody` dentro del
  contenedor. Para el import de Neo4j, **siempre** `docker cp` (`dock.CP`).
- `dockerd-rootless.sh` exige `HOME` en el entorno (`dock.Env`).
- `StartDaemon` necesita `cmd.Wait()` en segundo plano: sin él `ProcessState`
  nunca se rellena y no se detecta que el daemon murió.
- `kill -9` a `up` deja el motor huérfano ocupando el puerto: el siguiente `up`
  mata por cmdline (`/tmp/flowsint-desk-`) **antes** de comprobar el puerto.
- Los restos se apartan a un nombre **único** `*.stale`; un nombre fijo choca
  con uno anterior y el `rename` falla en silencio.
- El compose de producción de Flowsint no vale tal cual: publica en `0.0.0.0`,
  monta el socket rootful y deja Redis con persistencia. El embebido lo corrige.
- Healthcheck de Neo4j: `NEO4J_USERNAME` llega vacío dentro del contenedor;
  usuario y contraseña se derivan de `$NEO4J_AUTH`. Celery: su healthcheck
  falla siempre → desactivado.
- Las capacidades mínimas están medidas con el stack real: BD → `CHOWN SETUID
  SETGID DAC_OVERRIDE FOWNER`; app → `CHOWN SETUID SETGID`; api/celery → ninguna.
- naabu no hace escaneo SYN en contenedores sin privilegios, con ningún motor.

### Podman (medido en el CI real)

- **Logs y eventos a fichero**, nunca journald: acabarían en disco. Lo fija
  `containers.conf` propio (`CONTAINERS_CONF_OVERRIDE`).
- Almacén propio vía `CONTAINERS_STORAGE_CONF` → no toca
  `~/.local/share/containers`.
- **`XDG_RUNTIME_DIR` debe ser el real.** Con uno propio, `aardvark-dns` no
  arranca ("Failed to connect to user scope bus") y los contenedores no se
  encuentran por nombre.
- El **proceso pause** (`catatonit -P`) es **uno por usuario**, compartido con
  su Podman personal; su pidfile está en `$XDG_RUNTIME_DIR/libpod/tmp`, no en
  `tmp_dir`. Solo se mata si no queda ningún `conmon` del usuario.
- Nombres de imagen **completos** (`docker.io/library/…`): sin terminal no
  resuelve nombres cortos.
- Sin healthchecks del motor (`--no-healthcheck`): los ejecutaría systemd de
  usuario con temporizadores. La CLI sondea ella misma (`waitReady`).
- No se usa `docker compose` contra la API que Podman emula.
- **Jamás** `podman system reset` ni `podman system migrate`: con la
  configuración equivocada actúan sobre el almacén personal del usuario.
- Podman anidado en un contenedor de CI no funciona (`newuidmap` falla): no lo
  reintentes. Solo está probado Podman 5.7.

### restic

- `backup --json` y `ls --json` son **líneas JSON** (progreso + resumen). Se
  recorren línea a línea.
- restic avisa por **stderr**: stdout y stderr se capturan por separado
  (`store.run`). Mezclados rompen el parseo.
- Códigos de salida: 10 = no hay repositorio, 12 = frase incorrecta.
- `forget` agrupa por equipo+ruta y la ruta lleva el `boot_id`: **siempre**
  `--group-by ""` o la retención no borra nada.
- `restore` conserva las rutas absolutas del staging: localiza por nombre con
  `snap.FindFile`.
- `key add` (0.19) imprime texto, no JSON.
- restic publica **un** `SHA256SUMS` por release, no un `.sha256` por fichero.
- `restic check` sin `--read-data` verifica estructura, no cada byte.

### Volcados y verificación

- `pg_dump` solo por **stdout** (`dock.RunToFile`): un aviso por stderr dentro
  del `.sql` lo hace irrestaurable y el sha256 no lo detecta.
- `cypher-shell` ejecuta **un** statement por llamada.
- Los secretos del snapshot se restauran **antes** de crear las BD: si no,
  Neo4j crea `auth.ini` con otra contraseña.
- El export APOC no trae índices → se reaplican `assets/migrations/*.cypher`.
  Los `elementId` no sobreviven: vale porque nada en Postgres los guarda.
- Los recuentos de Postgres solo incluyen tablas que existen; un recuento
  ilegible es error, no cero.

### Sistema

- El llavero de paquetes de Arch (`archlinux.gpg`) va en ASCII y `gpgv` quiere
  binario: `dearmor()`.
- go-keyring usa la colección **login** y el atributo `username`; `secret-tool`
  usa otros: no interoperan. Guardar siempre con `passphrase.SaveKeyring`.
- `InhibitDelayMaxSec` de logind es 5 s de fábrica: el snapshot final al apagar
  no cabe sin subirlo.
- tmpfs puede paginarse a swap; `doctor` avisa de swap que no sea zram.
- Las imágenes mínimas de Fedora no traen `su` ni `useradd`; las de Debian, ni
  `ca-certificates`.
- Ubuntu restringe los user namespaces con AppArmor
  (`kernel.apparmor_restrict_unprivileged_userns`).

## Qué NO está validado (no lo des por bueno)

- Podman 5.4 (Debian 13) y 6.x (Arch, Fedora).
- pCloud real, llavero, apagado del equipo, SELinux en modo enforcing.
- El flujo `release` (nunca se ha empujado una etiqueta).
- Cancelación con Ctrl+C a medio arranque, `images sync` contra un registro y
  la segunda copia automática al cerrar: solo con dobles.

Si pruebas algo de esta lista, actualiza `docs/manual/09-desarrollo.md`,
`docs/security/2026-11-26-auditoria-repo.md` y esta sección.

## Al terminar un cambio

1. `gofmt -l .` vacío `&&` build `&&` vet `&&` test.
2. Commit granular con QUÉ y POR QUÉ, a nombre del dueño.
3. ¿Decisión de diseño? ADR en `docs/decisions/` y fila en `docs/README.md`.
4. ¿Cambia algo que ve el usuario? `docs/manual/`, `README.md`, `README.en.md`
   y `CHANGELOG.md`.
5. ¿Trampa nueva medida? Añádela aquí.
6. Empuja y **mira el CI**. Si `e2e` falla, lee el log antes de suponer nada.
