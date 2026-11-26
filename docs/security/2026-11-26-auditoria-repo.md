# Auditoría completa del repo — flowsint-desk (2026-11-26)

- **Alcance**: todo el árbol (CLI Go, compose embebido, migraciones, docs).
  Vara de medir: las cinco garantías de `ENCARGO.md` §1 — datos en claro solo
  en RAM, persistencia = snapshots cifrados, una sola llave, Flowsint sin
  tocar, sin sudo.
- **Método**: lectura completa del código + tests unitarios nuevos + un
  simulacro del flujo (`init/up/backup/down/kill -9/señales`) con dobles de
  `docker`, `restic` y `dockerd-rootless.sh`, como usuario sin privilegios y
  con `/tmp` en tmpfs.
- **Límite, sin maquillar**: en el entorno de la auditoría **no había Docker
  rootless ni restic reales, ni red para bajar las dependencias de Go**. El
  código se ha compilado y probado contra dobles locales de las librerías
  (cobra, toml, keyring, x/sys, x/term). Nada de esto sustituye a la batería
  de aceptación de `ENCARGO.md` §7: **hay que repetirla en la máquina real
  antes de fusionar**. Lo marcado ⚠ abajo es lo que más depende de ello.

Complementa (no sustituye) a `2026-11-auditoria-seguridad.md` del 20/11: aquella
miró la superficie de ataque; esta, sobre todo, si el producto cumple lo que
promete cuando algo sale mal.

## Corregido

| # | Sev. | Qué pasaba | Dónde |
|---|------|-----------|-------|
| 1 | **Crítica** | Si el snapshot final fallaba al cerrar (pCloud desmontado, bloqueo perdido, error de restic), `up` avisaba y **borraba igualmente toda la sesión**: se perdía lo trabajado desde el último snapshot bueno. Ahora la sesión sigue abierta y solo se cierra sin guardar si se repite la orden en 2 min. | `cli/up.go`, `cli/down.go` |
| 2 | **Alta** | `init` sobre un almacén que ya tenía datos (segundo equipo, o init repetido) creaba secretos nuevos y un snapshot vacío que pasaba a ser "el último": el siguiente `up` abría un Flowsint vacío. Ahora solo conecta el equipo. | `cli/init.go` |
| 3 | **Alta** | `forget` no borraba casi nada: restic agrupa por host+paths y la ruta lleva el `boot_id`, así que cada arranque del PC era un grupo con su propia cuota. El repo crecía sin límite. ⚠ `--group-by ""` sin probar contra restic real. | `store/store.go` |
| 4 | **Alta** | `pg_dump` se capturaba con stderr mezclado: un aviso de pg_dump acababa dentro de `postgres.sql` y el snapshot quedaba irrestaurable (el sha256 cuadraba, porque se calculaba sobre el fichero ya contaminado). | `snap/snap.go`, `dock/dock.go` |
| 5 | **Alta** | `restic backup --json` emite JSON Lines y se parseaba como un único JSON: en cuanto el backup tardaba lo bastante para emitir progreso se daba por fallido un snapshot que sí existía. Igual con avisos de restic por stderr. | `store/store.go` |
| 6 | **Alta** | `status`/`backup`/`snapshots`/`forget`/`passwd` reescribían `restic-pass`, el fichero que usa la sesión viva: una frase mal tecleada en un `status` rompía todos los snapshots siguientes (y con el #1, perdía el trabajo al cerrar). Sin sesión, dejaban la frase en `/tmp` hasta reiniciar. | `cli/common.go` |
| 7 | **Alta** | El bloqueo entre equipos identificaba al dueño solo por hostname: torre y portátil con el mismo nombre (el de fábrica de la distro) se pisaban el bloqueo en silencio. El latido se escribía sin atomicidad. | `lock/lock.go` |
| 8 | **Alta** | Arranque de `up`: Ctrl+C a medias, o cualquier error tras lanzar el daemon, dejaba daemon, contenedores, secretos y volcados restaurados vivos en RAM y el bloqueo tomado. `backup` durante el arranque mataba `up` (SIGUSR1 sin manejador). Un segundo `up` mataba al primero creyéndolo un resto. ⚠ | `cli/up.go` |
| 9 | Media | `/tmp/flowsint-desk-<boot_id>` es un nombre predecible en un directorio de todos y no se comprobaba dueño ni si era un enlace: otro usuario local podía pre-crearlo y recibir frase, secretos y volcados. Los `*.stale` ajenos también se procesaban. | `run/run.go`, `cli/up.go` |
| 10 | Media | Saneado de logs: la frase **no** se enmascaraba cuando venía del llavero o del prompt (el caso normal); y al revés, se tapaba la palabra `flowsint` (valor de `POSTGRES_USER`) en todos los logs. | `cli/logs.go` |
| 11 | Media | Nada avisaba de swap en disco: tmpfs puede paginarse y dejar datos en claro en el disco (portátil con hibernación). `doctor` ahora lo comprueba. | `cli/doctor.go` |
| 12 | Media | Los recuentos de control usaban una lista fija de 15 tablas en una sola consulta: una migración upstream que quite una tabla (imágenes `latest`) impedía **cualquier** snapshot. Recuentos ilegibles pasaban como 0. | `snap/snap.go` |
| 13 | Media | `doctor --fix` verificaba restic contra `<fichero>.sha256`, que no existe (404 comprobado; restic publica `SHA256SUMS`): la instalación automática fallaba siempre. | `cli/doctor.go` |
| 14 | Media | `save_interval_min = 0` → panic de `time.NewTicker` con todo levantado; `keep_* = 0` → el siguiente `forget` borraba todos los snapshots. | `config/config.go` |
| 15 | Media | `id[:8]` sin comprobar: `up --snapshot 3f2a` (prefijo corto) reventaba tras restaurar; ídem con ids vacíos a mitad de sesión. | `cli/*.go` |
| 16 | Media | `down` con la sesión muerta decía haber limpiado, pero ni paraba el daemon ni podía borrar el data-root ni soltaba el bloqueo. | `cli/down.go` |
| 17 | Baja | `StartDaemon` nunca detectaba la muerte temprana del daemon (consultaba `ProcessState` sin `Wait`): 90 s de espera y un error sin pistas. | `dock/dock.go` |
| 18 | Baja | `passwd`: si no se actualizaba el llavero, este conservaba la frase antigua; además creaba una copia de la frase en tmpfs sin necesidad. | `cli/passwd.go` |
| 19 | Baja | Con una frase guardada que no abría el almacén no había forma de teclear la buena (el README lo documentaba como trampa). | `cli/common.go` |
| 20 | Baja | `doctor` moría (nil) con una `config.toml` inválida; el test del llavero fallaba sin D-Bus, así que `go test ./...` no podía estar en verde. | `cli/doctor.go`, tests |

Además: aviso al crear una frase de menos de 16 caracteres (M3 de la auditoría
del 20/11), tests unitarios en 8 paquetes y CI con build + vet + test.

## Segunda tanda (mismo día): lo que quedaba abierto

Tras la primera entrega se cerró también lo pendiente de seguridad y se añadió
lo que le faltaba a la CLI.

| Tema | Qué se hizo | Estado real |
|------|-------------|-------------|
| **A2** socket de Docker en api/celery | Proxy filtrado (`internal/sockproxy`) montado por defecto: solo pull/inspect de imágenes y contenedores sin montajes, privilegios ni namespaces compartidos; solo se operan los contenedores creados por el proxy. Lista sacada del código de Flowsint (`dockertool.py`). Clave `docker_socket`. | Tests contra un daemon simulado con HTTP real (flujo de enricher pasa; 23 abusos denegados). ⚠ **Sin probar con los enrichers reales**: si alguno falla, `up` imprime la petición denegada. |
| **A1** imágenes por etiqueta móvil | `flowsint_version` configurable; digest anotado al descargar y guardado en el manifest; aviso en `up` si este equipo ejecuta otras imágenes que el snapshot; `images sync` las iguala por digest. | No se fijan digests en el código (no verificables desde aquí): se fija por almacén. ⚠ Sin probar contra un registro real. |
| **M2** descargas de `setup` | Firma de los paquetes de Arch verificada con `gpgv` y el llavero del sistema (sin ella no se instala); versión vigente consultada a archlinux.org; `dockerd-rootless.sh` del paquete del sistema o de la etiqueta de moby de la versión instalada; restic contra `SHA256SUMS`. | ⚠ Descargas no ejecutadas desde aquí (solo parsers probados). |
| **M3** frase corta | Aviso por debajo de 16 caracteres al crearla. | Hecho. |
| Apagado del equipo | `up` escucha `PrepareForShutdown` de logind y lanza el snapshot final al empezar el apagado; `doctor` comprueba `InhibitDelayMaxSec` y da los comandos para subirlo. | ⚠ Sin probar contra logind real. Con los 5 s de fábrica sigue sin dar tiempo: hay que subirlo. |
| Bloqueos huérfanos de restic | `unlock --restic`. | Hecho. |
| Frase en tmpfs tras `init` | Ya no se deja si se guardó en el llavero. | Hecho. |
| Endurecer compose | `no-new-privileges` en los seis servicios. | ⚠ Sin probar con el stack real. `cap_drop` y red interna para las BD, no: riesgo de romper el arranque sin poder medirlo. |
| Menores | `snapshots` con una llamada a restic por snapshot; `up` ya no pisa `config.toml`. | Hecho. |

Añadido a la CLI: asistente `setup` de principio a fin (requisitos por
distribución Debian/Arch, detección de pCloud, primer/segundo equipo, imágenes,
prueba), `selftest`, `verify`, `images`, `version`.

## Sigue pendiente

1. **Validar en real**, por este orden: `flowsint-desk selftest`; la batería de
   `ENCARGO.md` §7; un enricher (subfinder) con `docker_socket = "filtrado"`;
   `forget` con `--dry-run` de restic; apagar el equipo con una sesión abierta.
2. Los nombres de paquete de Debian que sugiere `doctor` (uidmap, rootlesskit,
   slirp4netns, fuse-overlayfs, docker.io, docker-compose) no se han probado en
   una Debian 13.
3. `Session.Alive()` solo mira el pid (reutilización de pid tras un kill -9,
   improbable).
4. Podman como segundo backend: `docs/research/2026-11-26-podman-reanalisis.md`.

## Cómo reproducir las comprobaciones

```sh
go build -o dist/flowsint-desk . && go vet ./... && go test ./...
```

Escenarios del simulacro que pasaron (69 comprobaciones, incluidas las de la segunda tanda: `verify`, `images`, `selftest` y el asistente `setup` en primer y segundo equipo): init nuevo / init
repetido / init con frase mala; `up` + `status` + `backup` + `snapshots` +
`logs`; segundo `up` rechazado; cierre con snapshot final fallido (no se
destruye) y cierre tras arreglarlo; segundo cierre dentro del plazo (cierra sin
guardar); Ctrl+C y SIGUSR1 durante el arranque; fallo de compose y daemon que
muere al arrancar (todo desmontado); `kill -9` → `down` limpia y el siguiente
`up` arranca; frase incorrecta; bloqueo de otro equipo y de otro equipo con el
mismo hostname; `forget`; `doctor`; config inválida.
