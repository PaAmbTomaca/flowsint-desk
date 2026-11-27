# Auditoría completa del repo — flowsint-desk (2026-11-26)

- **Alcance**: todo el árbol (CLI Go, compose embebido, migraciones, docs).
  Vara de medir: las cinco garantías de `ENCARGO.md` §1 — datos en claro solo
  en RAM, persistencia = snapshots cifrados, una sola llave, Flowsint sin
  tocar, sin sudo.
- **Método**: lectura completa del código + tests unitarios nuevos + un
  simulacro del flujo (`init/up/backup/down/kill -9/señales`) con dobles de
  `docker`, `restic` y `dockerd-rootless.sh`, como usuario sin privilegios y
  con `/tmp` en tmpfs.
- **Cómo leer este documento**: está escrito por tandas, en el orden en que
  ocurrió. La primera tanda se hizo **sin Docker ni restic reales** (solo
  compilación contra dobles y un simulacro, que no está en el repositorio); por
  eso sus filas llevan ⚠. Desde la tercera tanda el CI ejecuta el producto con
  software auténtico, y buena parte de aquellos ⚠ quedó comprobada: ver
  "Validado en real" y "Cuarta tanda". Lo que **hoy** sigue sin validar está en
  "Sigue pendiente", al final.

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

## Validado en real (tercera tanda, mismo día)

El límite que encabeza este documento ("sin Docker ni restic reales") dejó de
ser cierto para el flujo principal: el CI ejecuta ahora, en runners de GitHub
con software auténtico, tres cosas.

| Qué | Dónde | Resultado |
|-----|-------|-----------|
| Ciclo completo: asistente `setup` en equipo nuevo + prueba de humo (crear almacén, arrancar Flowsint, registrar usuario, snapshot, cerrar, restaurar, volver a entrar, cerrar), y después nada en `/tmp` ni ningún proceso del motor vivo | job `e2e`, Ubuntu, **Docker rootless** y **Podman 5.7** | ✔ los dos |
| `doctor --fix` en sistema pelado (restic contra `SHA256SUMS`; en Arch, binarios con firma), instalar la línea de `doctor --packages` y exigir requisitos en verde | job `distro`, contenedores de **Arch, Debian 13 y Fedora 45** | ✔ los tres |
| build + vet + test + gofmt | job `go` | ✔ |

Eso da por comprobados contra restic y contenedores reales, entre otros: el
parseo de `backup --json`, `forget` con `--group-by ""`, el volcado de Postgres,
el restore con verificación de manifest, el cierre con snapshot final y el
desmontaje. Probar en real destapó además dos fallos propios (llavero de Arch en
ASCII; proceso *pause* de Podman vivo tras cerrar), ya corregidos.

Se añadió también **Podman como segundo motor**, elegible en `setup`
(`docs/decisions/0008-two-container-engines.md`).

## Cuarta tanda: las doce mejoras propuestas tras la revisión

| # | Mejora | Estado |
|---|--------|--------|
| 1 | Enricher real en la prueba | `selftest` lanza subfinder desde el contenedor de la API con la librería que usa Flowsint. **Probado en real con ambos motores.** |
| 2 | Proxy: puertos, red del stack, rutas con trucos; lista de PERMITIDOS | Hecho. Los tres abusos (montar `/etc`, publicar un puerto, privilegiado) salen `DENEGADO` en el CI real con ambos motores. Además cada contenedor de enricher se crea sin capacidades. |
| 3 | Volcados de memoria | `core=0` en los seis servicios; `selftest` lo comprueba en un contenedor real. **Probado.** |
| 4 | Permisos y límites | Capacidades mínimas por servicio (**probado: el stack arranca así en ambos motores**). Tope de memoria/procesos solo para los enrichers y solo si el sistema delega los cgroups; al stack no se le pone tope de memoria (depende demasiado de la máquina). |
| 5 | Integridad de la caché | sha256 de cada `.tar`, verificado antes de cargar. |
| 6 | Acciones del CI por hash | Las cuatro, fijadas al commit que ya se ejecutaba. |
| 7 | Segunda copia | `mirror [--to RUTA]`, incremental; `up` la actualiza al cerrar. **Probado en real.** |
| 8 | Verificación periódica | `restic check` cada `verify_every_days` al arrancar; `status` lo muestra. |
| 9 | Casos feos en real | Frase incorrecta, `kill -9` + `down`, `kill -9` + `up`, almacén inaccesible al cerrar, segunda copia, segundo equipo: en el job `e2e` con ambos motores. |
| 10 | Compose ↔ Podman | Test que falla si divergen (imagen, variables, volúmenes, puerto, capacidades, comando). |
| 11 | Versiones publicadas | Flujo `release` (binarios + SHA256SUMS al etiquetar). **Sin ejecutar**: hace falta empujar una etiqueta. |
| 12 | Podman 5.4 / 6.x | **No conseguido.** Se intentó dentro de contenedores de Debian y Arch: `newuidmap` falla en ese entorno anidado, antes de llegar al producto. Queda confirmado solo que sus paquetes se instalan. Sigue probado únicamente Podman 5.7. |

Probar en real destapó además que los comandos de consulta dejaban un
directorio vacío en `/tmp` (corregido) y que `doctor` daba por bueno un Podman
que no podía crear su user namespace (ahora lo comprueba de verdad).

## Sigue pendiente

1. **Validar en TU máquina** (el CI no es un escritorio): `flowsint-desk
   selftest` (incluye ya el enricher y los abusos al socket); apagar el equipo
   con una sesión abierta; pCloud y llavero reales.
2. Los nombres de paquete de Debian que sugiere `doctor` (uidmap, rootlesskit,
   slirp4netns, fuse-overlayfs, docker.io, docker-compose) no se han probado en
   una Debian 13.
3. `Session.Alive()` solo mira el pid (reutilización de pid tras un kill -9,
   improbable).
4. Podman 6.x (Arch, Fedora) y 5.4 (Debian): el CI probó 5.7. SELinux en Fedora.
5. El flujo `release` no se ha ejecutado nunca.
6. Sin arreglo desde la CLI: cualquier usuario o programa local puede abrir
   `127.0.0.1:5273` y Flowsint permite registrarse; los secretos viajan como
   variables de entorno de los contenedores.

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
