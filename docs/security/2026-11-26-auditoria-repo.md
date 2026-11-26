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

## Pendiente (no tocado; por orden de riesgo)

1. **Validar todo lo anterior en real** (ENCARGO §7, las 9 pruebas, más: cerrar
   con pCloud desmontado, `init` en el segundo equipo, `forget --dry-run`).
2. **A1 — imágenes por tag mutable** (`latest`, `15-alpine`, `neo4j:5`). No se
   han fijado digests porque no se han podido verificar desde aquí y fijar un
   digest sin comprobar es peor que no fijarlo. Efecto añadido que la auditoría
   anterior no recogía: **torre y portátil pueden tener versiones distintas de
   Flowsint** (cada una con su caché de `latest`); la API migra el esquema al
   arrancar, y el equipo con la imagen vieja restauraría un volcado de esquema
   más nuevo. Recomendado: fijar digests y guardar/comparar el id de imagen en
   el manifest (hoy `image_digests` queda vacío cuando la imagen viene de la
   caché `docker load`).
3. **A2 — socket rootless en api/celery** (`:ro` no limita la API de Docker):
   un RCE en Flowsint equivale a todo lo que el usuario puede leer. Sigue
   pendiente el socket-proxy.
4. **Apagado del equipo**: el `systemd-inhibit --mode=delay` lanzado como
   `sleep infinity` no gana tiempo útil: logind espera `InhibitDelayMaxSec`
   (5 s por defecto) y después llegan los SIGTERM a la vez a `up` y al cliente
   de pCloud. Para un snapshot final fiable hay que escuchar
   `PrepareForShutdown` por D-Bus y subir `InhibitDelayMaxSec` (root). Hoy, al
   apagar, cuenta con el último snapshot automático (≤ 10 min).
5. **M2 — `doctor --fix`**: los paquetes de Arch se instalan sin verificar
   firma y con versión fija en el código (dejarán de existir en el mirror);
   `dockerd-rootless.sh` se baja de la rama `master` de moby, sin fijar.
6. **Bloqueos de restic huérfanos** tras un `kill -9` en mitad de un backup:
   `forget`/`prune` pueden negarse hasta un `restic unlock`. No hay comando.
7. `init` deja la frase en `<runDir>/passphrase` (tmpfs, 0600) hasta el primer
   `up` o el reinicio. Es cómodo y está documentado, pero es la frase en claro
   en RAM sin sesión abierta; valorar quitarlo si se usa el llavero.
8. Endurecer el compose (`no-new-privileges`, `cap_drop`, red interna para las
   BD). No se ha tocado porque no se puede probar aquí y romper un arranque
   medido no compensa.
9. Menores: `snapshots` hace dos llamadas a restic por snapshot (lento con
   cientos); `up` guarda `config.toml` al aplicar la retención y pisa ediciones
   hechas a mano durante la sesión; `Session.Alive()` solo mira el pid
   (reutilización de pid tras un kill -9, improbable).

## Cómo reproducir las comprobaciones

```sh
go build -o dist/flowsint-desk . && go vet ./... && go test ./...
```

Escenarios del simulacro que pasaron (51 comprobaciones): init nuevo / init
repetido / init con frase mala; `up` + `status` + `backup` + `snapshots` +
`logs`; segundo `up` rechazado; cierre con snapshot final fallido (no se
destruye) y cierre tras arreglarlo; segundo cierre dentro del plazo (cierra sin
guardar); Ctrl+C y SIGUSR1 durante el arranque; fallo de compose y daemon que
muere al arrancar (todo desmontado); `kill -9` → `down` limpia y el siguiente
`up` arranca; frase incorrecta; bloqueo de otro equipo y de otro equipo con el
mismo hostname; `forget`; `doctor`; config inválida.
