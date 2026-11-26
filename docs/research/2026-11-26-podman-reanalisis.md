# Podman, segunda vuelta (2026-11-26)

> **Corregido el mismo día** por `2026-11-26-podman-deep-research-2.md`, hecho con
> búsqueda web: este análisis (solo sobre el código) acierta en qué se gana, pero
> **se queda corto en coste y en un riesgo**: los healthchecks de Podman dependen
> de `systemd --user` y pueden no funcionar con almacenamiento en ruta propia, y
> Debian 13 (Podman 5.4) y Arch (6.x) son dos generaciones distintas. Coste
> revisado: 5–7 días, u 8–11 si hay que orquestar sin compose.

- **Pregunta**: tras la auditoría del 26/11, ¿sigue valiendo el "quedarse con
  Docker rootless, Podman es XL y no aporta" de P001 y del research del 18/11?
- **Respuesta corta**: no del todo. En **seguridad** la conclusión anterior se
  mantiene (ganancia marginal). En **simplicidad, instalación y robustez de la
  limpieza**, que es justo donde han salido los fallos de esta auditoría,
  Podman gana con claridad. Y el coste ya no es XL.
- **Límite**: análisis sobre el código y sobre lo que se conoce de Podman; en
  el entorno de la auditoría no había Podman para medir nada. Lo marcado
  **[por medir]** es hipótesis hasta que se pruebe en la máquina.

## Qué ha cambiado desde el análisis anterior

1. **Ahora se sabe dónde duele Docker rootless en este proyecto.** De los 20
   fallos corregidos, los de arranque/cierre/limpieza (8, 16, 17 de la
   auditoría) vienen de gestionar a mano un daemon:
   `dockerd-rootless.sh` + rootlesskit + pidfile + escaneo de `/proc` para
   matar huérfanos + directorios `.stale` + un contenedor alpine solo para
   poder borrar el data-root (ficheros de uids mapeados). Es la parte más
   frágil del código y la que más reglas tiene en `AGENTS.md`.
2. **El acoplamiento se ha reducido.** Todo desmontaje pasa por `teardown()`;
   todo lo de contenedores, por `internal/dock`. Cambiar de motor ya no toca
   `up`/`down`/`snap`.
3. **Existe `selftest`.** La revalidación (lo que hacía "XL" la estimación) es
   ahora un comando, más la batería §7.
4. **El punto de más riesgo del research (socket rootless→rootless para los
   enrichers, permisos del socket dentro del contenedor) ya no depende del
   motor**: lo que se monta en api/celery es el socket del proxy filtrado
   (`internal/sockproxy`), que crea la CLI con permisos abiertos dentro de un
   directorio 0700. Con Podman sería el mismo socket.

## Dónde gana Podman aquí

| Tema | Docker rootless (hoy) | Podman rootless |
|---|---|---|
| Qué hay que instalar | `docker` + compose + rootlesskit + slirp4netns + fuse-overlayfs + `dockerd-rootless.sh` (que en Arch hay que bajar de moby: hallazgo M2) | un paquete, `podman`, en Debian 13 y en Arch (arrastra crun, netavark, passt). **[por medir]** en Debian 13 |
| Asistente `setup` | descargas verificadas a `~/.local/bin`, firma de paquetes de Arch, script de moby | `sudo apt install podman` / `sudo pacman -S podman` y nada que descargar |
| Convivencia con el Docker del sistema | regla dura "no tocar el rootful", todo por `DOCKER_HOST`; un descuido toca los contenedores de trabajo | no comparten nada: ni socket, ni binario, ni almacenamiento |
| Borrar el estado al cerrar | contenedor alpine dentro del user-ns, con el daemon vivo y la imagen alpine cargada; si el daemon murió, no se puede y quedan `.stale` | `podman unshare rm -rf <dir>`: sin daemon, sin imagen, funciona también tras un `kill -9` |
| Procesos huérfanos tras `kill -9` | dockerd, rootlesskit, containerd, shims; se buscan por cmdline | conmon por contenedor; `podman --root … rm -fa` los recoge **[por medir]** |
| Red | slirp4netns | pasta (más rápida) |

Lo que **no** cambia: el aislamiento es el mismo (user namespaces sin
privilegios en ambos); los enrichers siguen necesitando un socket con API de
Docker (Flowsint usa el SDK de Docker), así que `podman system service` estaría
vivo durante la sesión. "Daemonless" aquí significa "sin daemon que gestione los
contenedores", no "sin ningún proceso de servicio".

## Riesgos reales (los que decidirían)

1. **Healthchecks.** Podman los ejecuta con temporizadores de systemd de
   usuario. El compose depende de `service_healthy` y de `--wait`. Con sesión
   systemd de usuario (CachyOS y Debian de escritorio la tienen) debería ir;
   sin ella no hay healthchecks. **[por medir]**: es el primer experimento.
2. **Compose.** `docker compose` contra el socket de Podman funciona para un
   compose simple como el nuestro, pero sigue exigiendo instalar el plugin de
   compose de Docker. Si se quiere soltar Docker del todo hay que sustituir el
   compose por llamadas directas (`podman run` de 6 servicios con sus esperas):
   unas 300 líneas, y a cambio desaparece una dependencia entera.
3. **Caché de imágenes y digests** con `podman save/load` **[por medir]**.
4. **Permisos de volúmenes** de Postgres/Neo4j con el mapeo de subuid: mismo
   modelo que hoy, pero hay que verlo arrancar.

## Coste revisado

| Fase | Qué | Estimación |
|---|---|---|
| 0 | Experimento de 1 tarde: `podman system service` en tmpfs + el compose actual + `selftest`. Responde a los riesgos 1, 3 y 4 | 0,5 día |
| 1 | Backend `podman` detrás de `internal/dock` (arranque/parada del service, `unshare rm`, save/load), seleccionable con `backend = "podman"` en `config.toml`; Docker sigue siendo el valor por defecto | 2–3 días |
| 2 | `doctor`/`setup` para Podman; `selftest` + batería §7 en los dos equipos | 1–2 días |
| 3 (opcional) | Sustituir compose por llamadas directas y retirar Docker | 2–3 días |

Total fases 0–2: **~4–6 días**, frente a los 10–15 del research anterior. La
diferencia sale de que ya no hay que reescribir el ciclo de vida (está
encapsulado) y de que la validación está automatizada.

## Recomendación

- **Sí merece la pena, pero como segundo backend, no como sustitución en
  caliente**, y **después** de validar en real lo que trae esta rama. Mezclar
  20 correcciones sin probar en tu máquina con un cambio de motor haría
  imposible saber qué rompe qué.
- Orden: (1) validar esta rama con Docker rootless (`selftest` + §7);
  (2) fase 0 con Podman; si los healthchecks y `--wait` funcionan, (3) fases
  1–2; (4) cuando Podman lleve unas semanas siendo el que usas, fase 3 y retirar
  Docker.
- El motivo para hacerlo no es "más seguro", es **menos piezas**: un paquete en
  vez de cinco y un script descargado, cero roce con el Docker de trabajo, y
  una limpieza que no depende de que un daemon siga vivo. Para una herramienta
  cuyo contrato es "al cerrar no queda nada", eso último pesa.

P001 pasa de "v2, solo si aparece un problema" a "siguiente paso tras validar
esta rama".
