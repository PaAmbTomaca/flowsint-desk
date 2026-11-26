# P001 — Podman como backend opcional (daemonless) en vez de Docker rootless

- **Estado**: **propuesta** — reanalizada el 2026-11-26 (`docs/research/2026-11-26-podman-reanalisis.md`): pasa a ser el siguiente paso tras validar las correcciones de la auditoría, como segundo backend seleccionable; coste revisado ~4–6 días (antes 10–15)
- **Fecha**: 2026-11-17
- **Contexto**: Surgió la duda de si Podman (rootless y daemonless) sería más
  seguro que el Docker rootless actual. Esta propuesta analiza la viabilidad
  real y el coste de migrar. La decisión vigente sigue siendo ADR 0001 (Docker
  rootless); esta propuesta no la reabre salvo que se demuestre un problema
  real con la arquitectura actual.

## Motivación

- Eliminar el daemon de Docker aunque sea rootless: arquitectura **daemonless**
  (cada comando es fork-exec con `crun`/`conmon`; no hay proceso con estado
  global corriendo).
- Rootless nativo **sin rootlesskit**: Podman usa user namespaces directamente
  (mismo `/etc/subuid`/`/etc/subgid` que Docker rootless).
- Networking rootless con **pasta** (en vez de slirp4netns): menos latencia y
  mejor rendimiento que slirp4netns.

## Verificación técnica (fuentes consultadas, nov 2026)

- `podman system service --time=0 unix://$XDG_RUNTIME_DIR/podman/podman.sock`
  expone una **API compatible con Docker v1.40** más la capa nativa Libpod
  (docs oficiales de `podman-system-service`).
- `docker compose` (v2) funciona contra ese socket con
  `DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock` — confirmado en la
  documentación de Podman y guías actuales de 2026 ("Podman really does play
  nicely with Docker Compose"). La paridad de compose es buena pero **no del
  100 %** (p. ej. `build:` y pods no existen en la API Docker; ciertos campos
  `deploy` se ignoran).
- Podman rootless: userns por defecto, runtime `crun`, sin ningún proceso
  privilegiado. El socket rootless de Podman está acotado al usuario (igual que
  el de Docker rootless: ambos equivalen a los permisos del usuario, **no a
  root del host**).
- Versión de referencia 2026: Podman 5.x/6.x, Docker Engine 29.x.

## Impacto real en flowsint-desk (no es un flag)

| Componente | Hoy (ADR 0001, Docker rootless) | Con Podman |
|---|---|---|
| `internal/dock` arranque | `rootlesskit` + `dockerd-rootless.sh`, data-root en tmpfs vía flags | `podman system service` por sesión (socket en tmpfs/run dir); storage en tmpfs vía `containers-storage.conf` (`graphroot`/`runroot` bajo `/tmp/flowsint-desk-<boot_id>/`) |
| Wipe del data-root | Contenedor alpine dentro del user-ns (`dock.WipeSubdirs`) porque `rm -rf` del host falla con uids mapeados | `rm -rf` directo del storage en tmpfs (más simple; ya no hay daemon vivo que lo sujete si se mata antes) |
| Caché de imágenes | `docker save`/`docker load` en `~/.cache/flowsint-desk/images/*.tar` | `podman save`/`podman load` (formato OCI/docker-archive compatible) |
| Compose embebido | `docker compose` con `DOCKER_HOST` al socket rootless | Igual, con `DOCKER_HOST` al socket de `podman system service`; revisar el compose campo a campo (nuestro compose es simple: healthchecks, depends_on, red custom — soportados) |
| Socket de los enrichers | `api`/`celery` montan el socket rootless como `/var/run/docker.sock` | Montar el socket de podman como `/var/run/docker.sock`; los enrichers (subfinder/naabu/httpx) usan el SDK de Docker → API compatible, **pero hay que re-probarlo** (socket forwarding dentro de contenedores rootless tiene caveats de permisos en Podman) |
| Import Neo4j | `docker cp` (`dock.CP`) | `podman cp` (existe, mismo comportamiento) |
| Limpieza tras `kill -9` | Escaner `/proc/*/cmdline` para matar daemons huérfanos | Buscar procesos `podman`/`conmon`/`crun` del boot_id; más simple al no haber daemon central |

## Riesgos e incógnitas

- **naabu** (escaneo de puertos con raw sockets) seguirá sin funcionar en
  rootless — igual que ahora con Docker rootless; no empeora, no mejora.
- Paridad de compose: nuestro `assets/compose.yml` usa healthchecks,
  `depends_on`, red custom y bind mounts → soportados por la API Docker de
  Podman, pero hay que validar uno a uno.
- Hay que **repetir la fase 0** (el método de volcado de Neo4j de ADR 0006 no
  cambia) y **toda la fase 4** de aceptación (9 pruebas del ENCARGO §7) con
  Podman antes de darlo por válido.
- Podman no está instalado en esta máquina hoy (`command -v podman` → vacío);
  habría que instalarlo sin sudo (paquete `podman` + `crun` + `pasta` de Arch,
  extraídos a `~/.local/bin` como se hizo con rootlesskit, o paquete del
  sistema).

## Análisis de seguridad honesto

- **Docker rootless ya corre sin privilegios**: `dockerd` es un proceso del
  usuario dentro de un user namespace; un escape de contenedor cae como
  usuario sin privilegios en el host, igual que con Podman rootless.
- En flowsint-desk el daemon solo existe **durante la sesión `up`** y su
  data-root está en tmpfs: muere y se borra al cerrar. La ventana de
  exposición es la sesión activa — la misma que tendría `podman system
  service` (que también es un proceso, solo que sin estado global).
- Ganancia real de Podman aquí: menos superficie teórica (sin daemon con
  estado global, sin rootlesskit) y networking más rápido (pasta). **Coste
  real**: reescribir `internal/dock`, storage, wipe y caché, y revalidar todo
  (fases 0 y 4).

## Recomendación

- **v1**: quedarse con Docker rootless (ADR 0001), ya implementado y validado.
- **v2**: evaluar Podman solo si se quiere eliminar el daemon por completo o si
  aparece un problema real con Docker rootless.

## Criterios de aceptación (si se aborda)

1. `doctor` detecta y guía la instalación de podman/crun/pasta.
2. Fase 0 completa en verde con Podman (método Neo4j idéntico, ADR 0006).
3. Fase 4 completa (las 9 pruebas de aceptación) en verde con Podman.
4. Los enrichers lanzan contenedores desde `api`/`celery` vía socket (subfinder
   y httpx OK; naabu documentado como no funcional en rootless, igual que hoy).
5. Rendimiento igual o mejor que Docker rootless (pasta vs slirp4netns).

## Referencias

- **Deep research completa (2026-11-18) con matriz de complejidad por componente
  (S/M/L/XL, total ~10–15 días = escala XL)**: `docs/research/2026-11-podman-deep-research.md`.
- docs.podman.io — `podman-system-service(1)` (API Docker v1.40 compatible en
  socket unix; socket dentro de contenedor: volumen + `label=disable`).
- Guías 2026: `DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock` +
  `docker compose` v2 contra Podman rootless.
- Comparativas 2026 daemonless (Podman/crun) vs client-servidor (Docker):
  misma seguridad en rootless, distinta superficie (daemon con estado vs
  fork-exec).
- `docs/decisions/0001-docker-rootless-daemon-efimero.md` (decisión vigente).
- `ENCARGO.md` §3 (docker.sock de los enrichers) y §7 (pruebas de aceptación).
