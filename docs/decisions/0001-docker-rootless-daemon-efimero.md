# ADR 0001 — Docker rootless con daemon efímero por sesión, data-root en tmpfs

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11 (fase 0–1 del encargo)
- **Contexto**: Flowsint se despliega con contenedores, pero en un PC de
  escritorio sin servidor y **sin sudo permitido**. El Docker rootful del
  sistema está en uso por otros proyectos y está prohibido tocarlo.

## Decisión

- Docker **rootless** (`rootlesskit` + `slirp4netns` + `dockerd-rootless.sh`),
  con un daemon **por sesión**: al hacer `up` se levanta en
  `/tmp/flowsint-desk-<boot_id>/` con el data-root en esa tmpfs, y al cerrar
  muere y se borra todo.
- Todo comando docker del proyecto usa `DOCKER_HOST=unix:///tmp/flowsint-desk-<boot_id>/docker.sock`.
  La única excepción permitida es `docker ps` SIN `DOCKER_HOST`, como foto de
  control de que el rootful del sistema no se toca.
- Las imágenes públicas se cachean en disco (`~/.cache/flowsint-desk/images/`)
  porque no contienen datos de usuario; todo lo demás vive solo en tmpfs.

## Consecuencias

- **+** Sin privilegios: el daemon corre como el usuario dentro de un user
  namespace; el socket rootless equivale a los permisos del usuario, no a
  root del host. Al cerrar la sesión no queda rastro en disco.
- **−** Existe un daemon (sin privilegios) mientras la sesión está abierta.
  Alternativa daemonless: ver propuesta `proposals/P001-podman-backend-opcional.md`.
- **Trampas ya medidas** (no redescubrir): `rm -rf` desde el host no puede
  borrar el data-root (uids mapeados por subuid) → la limpieza se hace con un
  contenedor alpine dentro del propio user-ns (`dock.WipeSubdirs`); ficheros
  escritos por el host en un bind-mount aparecen como `nobody` dentro del
  contenedor → para el import de Neo4j usar siempre `docker cp`; tras
  `kill -9`, el siguiente `up` mata los daemons huérfanos escaneando
  `/proc/*/cmdline`.

## Alternativas consideradas

- Docker rootful del sistema: **rechazada** (prohibido sudo; hay contenedores
  de otros proyectos ahí).
- Podman rootless daemonless: **propuesta P001** (v2), no bloqueante.

## Referencias

- `ENCARGO.md` §2 (decisiones tomadas) y reglas duras; `AGENTS.md`.
- `internal/dock/`.
