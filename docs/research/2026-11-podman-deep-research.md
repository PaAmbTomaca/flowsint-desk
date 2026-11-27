# Deep research: Podman como backend de flowsint-desk (nov 2026)

> **Nota (2026-11-26).** Investigación previa a la implementación. Podman es ya el segundo motor; lo que ocurrió al ejecutarlo de verdad está en `docs/decisions/0008-two-container-engines.md`. La estimación de 10–15 días de abajo no se cumplió: el ciclo de vida quedó encapsulado y la validación la hizo el CI.

- **Fecha**: 2026-11-18
- **Pregunta**: ¿qué costaría (en complejidad) sustituir Docker rootless por Podman
  rootless daemonless en flowsint-desk, y qué se ganaría realmente?
- **Fuentes**: docs oficiales de Podman 2026 (podman(1), podman-system-service(1),
  podman-network(1)), ArchWiki Podman, rootlesscontaine.rs (actualizado mayo 2026),
  guías de migración compose 2026, issues de GitHub de podman. URLs al final.

## 1. Qué es Podman rootless en 2026 (estado del arte)

- **Daemonless de verdad**: no hay proceso con estado global; cada comando es
  fork-exec con `crun` + `conmon`. El "socket" (`podman system service`) es solo
  un proceso ligero en espacio de usuario que expone la **API Docker v1.40
  compatible** (`unix://$XDG_RUNTIME_DIR/podman/podman.sock`) para herramientas
  Docker (compose, SDKs).
- **Rootless nativo**: user namespace automático con `/etc/subuid` + `/etc/subgid`
  (ya los tenemos). Sin rootlesskit: no hace falta.
- **Storage configurable por sesión**: `CONTAINERS_STORAGE_CONF` o
  `--root`/`--runroot` → `graphroot`/`runroot` pueden apuntar a tmpfs
  (`/tmp/flowsint-desk-<boot_id>/...`), igual que nuestro data-root actual.
  Con kernel ≥ 5.13 (tenemos 7.3) usa **overlay nativo en rootless** (más rápido
  que fuse-overlayfs); fuse-overlayfs solo haría falta en kernels viejos o NFS.
- **Networking**: desde Podman 5 el backend por defecto es **pasta** (no
  slirp4netns; este se elimina en 6.0). Pasta no hace NAT (copia la IP del host);
  el problema container→host se arregló en 5.3. Las redes custom de compose
  funcionan vía netavark.
- **Compose**: la vía con mejor paridad es **`docker compose` v2 apuntando con
  `DOCKER_HOST` al socket de Podman** (no `podman-compose`, que tiene menos
  paridad: p. ej. healthchecks en depends_on). Nuestro compose es simple
  (imágenes, env, volúmenes nombrados, red custom, healthchecks, depends_on,
  `127.0.0.1:5273:8080`, `healthcheck: disable`) → todo soportado por la API
  Docker de Podman, pero hay que validar campo a campo.

## 2. Puntos críticos para flowsint-desk (con evidencia)

### 2.1 Socket para los enrichers (el punto más delicado)
Flowsint monta el socket de Docker en `api`/`celery` para que subfinder/naabu/httpx
**lancen contenedores** (docker SDK contra `/var/run/docker.sock`).
- Docs oficiales de `podman-system-service`: para usar el socket desde dentro de
  un contenedor hay que montarlo como volumen **y** `--security-opt label=disable`
  (en sistemas con SELinux; CachyOS/Arch no lleva SELinux activo, pero el problema
  real en rootless es el **mapeo de UIDs del socket** dentro del contenedor:
  reportado como "root:root 0660 dentro del contenedor" → permission denied).
- Workarounds documentados: `--userns=keep-id`, o montar el socket con permisos
  ajustados. Issue upstream #10203 dice "should just work" en rootless-to-rootless.
- **Veredicto**: viable pero hay que probarlo; es el bloque con más riesgo.

### 2.2 naabu (raw sockets)
En rootless, el kernel bloquea raw sockets sobre la red del host aunque el
contenedor tenga CAP_NET_ADMIN dentro de su userns. **Igual en Docker rootless que
en Podman rootless** (pasta no lo cambia). No empeora; documentado como no funcional.

### 2.3 Volúmenes y permisos
En rootless ambos motores mapean UIDs vía subuid. En Podman, los volúmenes
nombrados bajo el graphroot en tmpfs tienen los mismos semantics. Sin SELinux no
hay que preocuparse de `:Z`. Nuestra trampa medida (ficheros del host en bind
mounts viéndose como `nobody`) aplica igual; seguimos usando `docker cp`→
`podman cp` para el import de Neo4j.

### 2.4 Sesión efímera
`podman system service --time 0 unix:///tmp/flowsint-desk-<boot_id>/podman.sock`
sin systemd (queremos efímero por sesión, no `systemctl --user enable`). Requiere
`XDG_RUNTIME_DIR` definido. Al cerrar: matar el proceso service + `rm -rf` del
graphroot en tmpfs (más simple que nuestro wipe con contenedor alpine).

## 3. Matriz de complejidad (escala S/M/L/XL)

Leyenda: **S** ≤ 1 día · **M** 1–3 días · **L** 3–7 días · **XL** > 1 semana.

| Componente | Qué cambia | Escala | Días est. | Notas |
|---|---|---|---|---|
| `internal/dock` arranque/parada | rootlesskit+`dockerd-rootless.sh` → `podman system service`; data-root flags → `storage.conf` con graphroot/runroot en tmpfs | **XL** | 3–5 | Toca StartDaemon/StopDaemon/Env/Alive/KillOrphanDaemons |
| `assets/compose.yml` | Revisar campo a campo contra la API Docker de Podman vía `docker compose` con DOCKER_HOST | **M** | 1–2 | healthchecks, depends_on, red custom, volúmenes nombrados OK; validar `--wait` |
| Socket de los enrichers | Montar `podman.sock` como `/var/run/docker.sock` en api/celery; probar subfinder/httpx lanzando contenedores; naabu documentado como no funcional | **L–XL** | 2–4 | Riesgo por permisos del socket en rootless-to-rootless |
| Caché de imágenes | `docker save/load` → `podman save/load` (formato compatible) | **S** | 0.5 | |
| Wipe / limpieza kill -9 | Wipe con alpine → `rm -rf` del graphroot tmpfs (o `podman system reset`); huérfanos: buscar `conmon`/`crun`/`podman` del boot_id en /proc | **S–M** | 1 | Más simple que ahora |
| `doctor`/`setup` | Chequeos de podman/crun/pasta en vez de rootlesskit/slirp4netns; instalación sin sudo (paquetes Arch) | **S** | 0.5–1 | Reutiliza la estructura actual |
| Re-validación | Repetir fase 0 (método Neo4j no cambia) y fase 4 completa (9 pruebas) con Podman | **M–L** | 2–4 | Obligatorio antes de darlo por válido |
| Docs / ADRs | P001 → ADR aceptada, research, README | **S** | 0.5 | |
| **TOTAL** | | **XL** | **~10–15 días** (~2–3 semanas a tiempo parcial) | |

## 4. Qué se gana realmente (honesto)

- **+** Sin daemon con estado global (el socket service es un proceso plano);
  rootless por defecto sin rootlesskit; pasta más rápido que slirp4netns; overlay
  nativo más rápido que fuse-overlayfs; wipe más simple.
- **−** Nada de seguridad **real** nuevo en nuestro caso: Docker rootless ya corre
  sin privilegios (daemon del usuario en userns) y aquí solo vive durante `up`
  (data-root en tmpfs, muere al cerrar). La ventana de exposición es la misma.
- **Coste**: ~2–3 semanas de reescritura de `internal/dock` + revalidación completa,
  a cambio de una ganancia arquitectónica marginal para un uso de escritorio
  efímero y mono-usuario.

## 5. Riesgos principales (ordenados)

1. Socket rootless-to-rootless para los enrichers (permisos del socket) — bloqueante si falla.
2. Paridad compose campo a campo (`--wait`, healthchecks con `disable`, depends_on).
3. Diferencias de red custom con netavark/pasta (DNS entre contenedores del proyecto).
4. Permisos de volúmenes con mapeo subuid (pg_data/neo4j_data con usuarios internos distintos).
5. Cambios de comportamiento en limpieza tras kill -9 (conmon huérfanos).

## 6. Recomendación

- **v1**: quedarse con Docker rootless (ADR 0001, verificado en fase 4: 9/9).
- **v2 (opcional)**: Podman si se quiere eliminar el daemon por completo.
  Criterios de aceptación: fase 0 y fase 4 completas en verde con Podman;
  subfinder/httpx lanzando contenedores vía socket OK; rendimiento ≥; doctor/setup
  guían la instalación de podman/crun/pasta sin sudo.

## 7. Fuentes

- docs.podman.io — podman(1) (rootless, --root/--runroot, CONTAINERS_STORAGE_CONF), 2026
- docs.podman.io — podman-system-service(1) (API Docker v1.40, socket dentro de contenedor: volumen + label=disable)
- docs.podman.io — podman-network(1) (pasta por defecto, slirp4netns eliminado en 6.0)
- wiki.archlinux.org/title/Podman — rootless, subuid, overlay nativo (kernel ≥5.13), pasta vs slirp4netns
- rootlesscontaine.rs/getting-started/podman (mayo 2026) — tabla de features rootless
- oneuptime.com (2026-03-18) — guía de problemas compose con Podman (redes, volúmenes, depends_on+healthchecks); recomienda `docker compose` v2 con DOCKER_HOST al socket de Podman
- github.com/containers/podman issues #10203 (socket rootless-to-rootless), #28542 (pasta), exasol/ai-lab#535 (permisos del socket montado)
- redhat.com — "Podman is gaining rootless overlay support" (overlay nativo ≥5.13)
- research previa: docs/proposals/P001-podman-backend-opcional.md
