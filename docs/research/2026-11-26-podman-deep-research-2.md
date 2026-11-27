# Deep research: Podman rootless como backend — qué ganamos, qué perdemos, complejidad y riesgos (2026-11-26)

> **Nota posterior.** Podman se implementó ese mismo día siguiendo la recomendación de esta investigación de orquestar directamente, sin compose ni healthchecks del motor. De los riesgos de abajo, los que se pudieron medir en el CI (Podman 5.7): el borrado necesita desmontar antes el overlay; el proceso pause queda vivo y es compartido por usuario; un `XDG_RUNTIME_DIR` propio rompe el DNS. Los enrichers a través del proxy funcionan. Podman 5.4 y 6.x siguen sin probar. Ver `docs/decisions/0008-two-container-engines.md`.

- **Pregunta**: cambiar Docker rootless por Podman rootless (daemonless) en
  flowsint-desk: ganancias, pérdidas, complejidad y riesgos de seguridad y
  operativos, con el estado de Podman a finales de 2026.
- **Sustituye en lo que discrepe** al research del 18/11 y al reanálisis de
  esta misma fecha (`2026-11-26-podman-reanalisis.md`), que era solo sobre el
  código y resulta **optimista en un punto clave: los healthchecks**.

## 0. Fiabilidad de este documento (leer primero)

- La investigación se hizo con búsqueda web, pero **no se pudieron abrir las
  páginas**: la red del entorno bloqueaba docs.podman.io, github.com (fuera de
  este repo), los rastreadores de paquetes, etc. Todo sale de **fragmentos de
  resultados de búsqueda**, no de páginas leídas enteras.
- **[NC]** = no confirmado contra fuente primaria. **[mem]** = conocimiento del
  diseño de Podman no re-verificado hoy.
- Los CVE de 2026 con numeración alta vienen de agregadores: úsalos como pista,
  no como cita.
- No se ejecutó Podman. Nada de lo que sigue está medido. La sección 9 son los
  experimentos que lo zanjan en una tarde.

## 1. Conclusión

**Viable y arregla de verdad los tres dolores actuales** (borrado del estado,
procesos huérfanos, número de piezas a instalar), **pero no es un reemplazo
directo**. Dos riesgos pueden tumbarlo y solo se resuelven probando:

1. **Healthchecks**: en Podman los ejecuta `systemd --user` con temporizadores
   transitorios. Con almacenamiento en una ruta propia (`--root` en la tmpfs)
   no está confirmado que el temporizador encuentre el contenedor; si no lo
   encuentra, `compose up --wait` se cuelga.
2. **`docker compose` contra una API emulada** que además es distinta en tus dos
   sistemas: Debian 13 lleva Podman 5.4 (API 1.41) y Arch/CachyOS 6.x (API 1.44).

**Recomendación**: no migrar todavía. Hacer el experimento de una tarde
(sección 9) en los dos equipos y decidir con datos. Si se migra, lo más robusto
no es "compose sobre el socket de Podman" sino **que la CLI lance los 6
contenedores con `podman` directamente**, con su propio orden y sondeo de salud,
dejando el socket solo para los enrichers.

Confianza: alta en que Podman resuelve limpieza, recuperación y piezas; media
(~60 %) en que el conjunto funcione de punta a punta con cambios moderados; baja
en los puntos [NC].

## 2. Estado de Podman a finales de 2026

- Upstream va por la serie **6.x** (6.0 en junio de 2026; 6.1 en agosto; 6.2 en
  otoño [NC fecha]). El repositorio aparece ahora como
  `github.com/podman-container-tools/podman`.
- **Podman 6.0 elimina**: slirp4netns (solo pasta), CNI (solo netavark),
  cgroups v1, el backend iptables (nftables obligatorio) y BoltDB (migra a
  SQLite). Sube la API compatible con Docker a **v1.44**.
- **Por distro**:

  | Distro | Podman | Nota |
  |---|---|---|
  | Debian 13 (trixie) | **5.4.2** | congelada; parches vía Debian, no upstream |
  | Arch / CachyOS | **6.x** | 6.0.1 en verano; hoy probablemente 6.2 [NC] |
  | Fedora 44 / 45 | 5.8.x / **6.1.x** | Podman 6 se aplazó a Fedora 45 |

- **Consecuencia**: la CLI tendría que convivir con dos generaciones a la vez
  (5.4 con slirp4netns y netavark 1; 6.x sin slirp4netns y con netavark 2).

Fuentes: blog.podman.io (6.1.0), linuxiac.com (6.0), packages.debian.org,
archlinux.org/packages, packages.fedoraproject.org, pagure.io/fesco/issue/3530.

## 3. Lo que ganamos

| Ganancia | Por qué importa aquí | Solidez |
|---|---|---|
| **Borrado del estado sin contenedor auxiliar**: `podman unshare rm -rf <dir>` | Hoy hace falta un contenedor alpine dentro del user-ns, con el daemon vivo. Si el daemon murió, no se puede y quedan `.stale` | Documentado en la man page de `podman-unshare` |
| **Recuperación tras `kill -9` sin daemon que resucitar** | El estado está en disco (SQLite en el graphroot): `podman --root … rm -fa` lo recoge | Diseño de Podman; restos de pasta/aardvark [NC] |
| **Menos piezas**: fuera dockerd, containerd, rootlesskit, slirp4netns y el script `dockerd-rootless.sh` | Un paquete en vez de cinco y un script descargado (el origen del hallazgo M2) | Sólido |
| **Cero roce con tu Docker de trabajo** | Podman rootless no toca el firewall ni el socket del sistema | Sólido si NO se instala `podman-docker` (ver riesgos) |
| Menor superficie residente | No hay daemon del que dependan los contenedores | Sólido |
| Sin `CAP_NET_RAW` por defecto; el contenedor no alcanza el loopback del host | Los enrichers no llegan a servicios locales de tu PC | Fuentes secundarias |
| pasta más rápido que slirp4netns | Rendimiento de red | Documentación oficial (cifras de blogs: no fiables) |
| SELinux real en Fedora | Confinamiento adicional | Sólido |

## 4. Lo que perdemos

| Pérdida | Detalle |
|---|---|
| **La API de Docker real** | Pasa a ser una emulación (1.41 en Debian, 1.44 en Arch). Upstream sigue por detrás de Docker. Más sorpresas con compose y con el SDK de Python de los enrichers |
| **Healthchecks autónomos** | Dependen de `systemd --user`. Sin sesión de usuario (cron, `su`, ssh sin lingering) no se ejecutan y el estado queda en `starting` para siempre |
| **"Sin unidades systemd"** | Cada contenedor con healthcheck crea un temporizador transitorio; tras un `kill -9` siguen disparándose hasta limpiarlos |
| **Una sola versión objetivo** | Dos generaciones de Podman según el equipo |
| **Aislamiento total respecto a "tu" Podman** | El proceso *pause* y el user namespace son por usuario [mem]: si usas Podman para otras cosas, se comparten salvo que se aísle `--tmpdir` |
| **`CAP_NET_RAW`** | naabu pasaría a escaneo CONNECT (hoy, con slirp4netns, el SYN tampoco es real: cambio de comportamiento más que pérdida) |
| Madurez de la ruta de red | netavark 2 / aardvark-dns 2 tienen meses; hay issues abiertos de DNS intermitente |

## 5. Lo que queda igual

- **Quien accede al socket ejecuta como tú** (puede montar tu home), en ambos.
  La man page de `podman system service` lo dice literalmente. **El proxy
  filtrado sigue siendo imprescindible**, y habría que ampliarlo: Podman expone
  además los endpoints `/libpod/*` y `/build`.
- User namespaces con subuid; `newuidmap`/`newgidmap`; un `sudo` para instalar.
- Riesgo de escape por el runtime OCI (runc/crun comparten la clase de fallos:
  los tres CVE de runc de noviembre de 2025 tenían equivalentes en crun).
- La caché de imágenes en tarballs (con posible mejora: un *imagestore*
  persistente fuera de la tmpfs [mem]).

## 6. Complejidad

| Bloque | Qué implica | Estimación |
|---|---|---|
| Experimento previo | Sección 9, en los dos equipos | 0,5–1 día |
| Backend detrás de `internal/dock` | Arranque del service con `--root/--runroot/--tmpdir/--network-config-dir/--volumepath`, `unshare rm`, save/load | 2–3 días |
| **Orquestación directa** (si compose o los healthchecks fallan, que es lo probable en alguna de las dos versiones) | Lanzar los 6 contenedores con `podman run`, orden de arranque y sondeo de salud propios; el compose embebido deja de usarse | +3–4 días |
| Proxy filtrado | Denegar `/libpod/*` y `/build`; permisos del socket (SELinux en Fedora) | 0,5–1 día |
| `doctor`/`setup` | Paquetes por distro, versión mínima, `aardvark-dns` explícito en Debian | 1 día |
| Validación | `selftest` + §7 en Debian 13 y CachyOS | 1–2 días |

**Total realista: 5–7 días si compose funciona sobre el socket; 8–11 si hay que
orquestar directo.** El reanálisis de hoy decía 4–6: se quedaba corto porque no
contaba con el problema de los healthchecks ni con las dos generaciones.

## 7. Riesgos operativos

| # | Riesgo | Probabilidad | Mitigación |
|---|---|---|---|
| 1 | Los healthchecks no corren con almacenamiento en ruta propia o sin bus de usuario → `up --wait` se cuelga | **Media-alta** | Experimento 1. Plan B: desactivar el temporizador y que la CLI sondee |
| 2 | `docker compose` falla contra la API emulada (`--wait`, `service_healthy`, `healthcheck: disable`) o por versión mínima de API en Debian | Media | Experimento 2 en ambas distros. Plan B: orquestación directa |
| 3 | Divergencia Podman 5.4 / 6.x | **Segura** | Detectar versión, mínimo soportado explícito, probar en las dos |
| 4 | **Nombres cortos**: `podman pull postgres:15-alpine` sin terminal puede fallar ("short-name resolution enforced") | Alta si no se cualifican | Nombres completos siempre (`docker.io/library/postgres:15-alpine`), también en los enrichers |
| 5 | El SDK de Python de los enrichers se comporta distinto (`remove=True`, logs, wait) | Media | Experimento 3 con el código real |
| 6 | Estado compartido con tu Podman | Media si lo usas | Aislar `--tmpdir`, redes y volúmenes |
| 7 | Overlay nativo sobre tmpfs falla | Baja-media | Respaldo fuse-overlayfs (lo de hoy) |
| 8 | Restos tras `kill -9`: conmon, pasta, aardvark-dns, temporizadores | Media | `rm -fa` + parar temporizadores + el escaneo de `/proc` como red de seguridad |
| 9 | Los binarios estáticos de Podman (instalación sin sudo) están compilados sin soporte systemd: **sus healthchecks no corren nunca** | Segura si se usan | No ofrecer instalación sin sudo: paquete del sistema |

## 8. Riesgos de seguridad

| # | Riesgo | Probabilidad | Mitigación |
|---|---|---|---|
| 1 | El proxy filtrado no cubre `/libpod/*` ni `/build` | Media si se reutiliza tal cual | Lista permitida estricta; denegar libpod por defecto |
| 2 | **Un borrado mal dirigido destruye tus datos**: `podman system reset` borra todo, incluido `~/.local/share/containers` si faltan los flags | Baja, impacto alto | No usar nunca `system reset` ni `system migrate`; validar que la ruta empieza por `/tmp/flowsint-desk-` |
| 3 | Debian 13 congelada en 5.4.2 (crun, netavark, aardvark antiguos) | Media | Depender de las actualizaciones de seguridad de Debian; avisar bajo un mínimo |
| 4 | En Fedora hay que poner `label=disable` a api/celery para que usen el socket → pierden el confinamiento SELinux | Segura en Fedora | Solo en esos dos servicios |
| 5 | **Instalar `podman-docker`** (el *shim* que provee `/usr/bin/docker`) entra en conflicto con Docker y rompería tu Docker de trabajo | Baja si se evita | `doctor` no debe sugerirlo nunca |
| 6 | Añadir `NET_RAW` para recuperar el escaneo SYN | Baja | No concederla |

**Comparación de fondo**: ninguno de los dos historiales de CVE es claramente
mejor. Los fallos graves de los últimos dos años están en los runtimes OCI, que
ambos comparten. Podman reduce superficie (menos procesos residentes, menos
capacidades por defecto); Docker ofrece la API original y una ruta de red más
rodada. **El cambio no se justifica por seguridad**, sino por robustez de la
limpieza y simplicidad de instalación.

## 9. Experimentos que lo zanjan (una tarde, en Debian 13 y en CachyOS)

No se han ejecutado: son los comandos propuestos. Preparación común:

```sh
D=/tmp/fd-podman-test; mkdir -p $D/{root,run,tmp,net,vol}
P="podman --root $D/root --runroot $D/run --tmpdir $D/tmp --network-config-dir $D/net --volumepath $D/vol"
$P info --format '{{.Store.GraphRoot}} {{.Store.GraphDriverName}} {{.Host.NetworkBackend}}'
```

1. **Healthcheck con almacenamiento propio (el decisivo)**:
   ```sh
   $P run -d --name hc --health-cmd true --health-interval 2s docker.io/library/alpine:3 sleep 600
   sleep 8; $P inspect hc --format '{{.State.Health.Status}}'     # debe decir healthy, no starting
   systemctl --user list-timers --all | grep "$($P inspect hc --format '{{.Id}}')"
   ```
2. **El compose real contra el socket**:
   ```sh
   $P system service --time 0 unix://$D/podman.sock &
   curl -s --unix-socket $D/podman.sock http://d/version
   DOCKER_HOST=unix://$D/podman.sock docker compose -f compose.yml --env-file compose.env up -d --wait --wait-timeout 300; echo rc=$?
   ```
3. **Enrichers: el SDK de Python como no-root contra el socket** (lanzar
   subfinder con `containers.run(..., remove=True, network_mode="bridge")`).
4. **Borrado**: `$P rm -fa -t 0; $P unshare rm -rf $D/root $D/run; rm -rf $D`.
5. **`kill -9` al service** y contar lo que queda (`conmon`, `pasta`,
   `aardvark-dns`, temporizadores `systemctl --user`).
6. **Red**: DNS entre contenedores de una red propia y bind solo en 127.0.0.1.
7. **Imágenes**: `pull` con nombre corto sin terminal; `save`/`load` y si el
   digest sobrevive.
8. **No interferencia**: `docker ps` y las reglas de firewall, iguales antes y
   después; `~/.local/share/containers` intacto.

**Regla de decisión**: si 1, 2 y 3 pasan en los dos equipos → backend Podman
sobre compose, con Docker de respaldo una versión. Si falla 1 o 2 → orquestación
directa o no cambiar. Si Debian 5.4 diverge demasiado de 6.x → no cambiar hasta
que Debian actualice.

## 10. Alternativas descartadas

- **Quadlet** (unidades systemd): lo idiomático en Podman 6, pero choca con el
  diseño (efímero, sin unidades, tmpfs por sesión).
- **`podman kube play`**: sin compose ni socket, pero sin equivalente a
  `depends_on: service_healthy` y con un CVE reciente de escritura en el host.
- **`podman compose`**: es un envoltorio sobre `docker-compose` o
  `podman-compose`; no aporta nada. `podman-compose` tiene `up --wait` roto.
- **nerdctl rootless**: mantiene daemon (containerd) y rootlesskit: no arregla
  ninguno de los tres dolores.

## 11. Fuentes principales (vía fragmentos de búsqueda)

- man `podman-system-service(1)`, `podman-unshare(1)`, `podman-system-reset(1)`,
  `podman-compose(1)` — docs.podman.io / man.archlinux.org / manpages.debian.org
- Código de healthchecks de Podman (`libpod/healthcheck_linux.go`,
  `healthcheck_unsupported.go`) — sources.debian.org
- Issues de Podman: healthchecks sin systemd (#28192, PR #27033), API mínima con
  Docker 29 (#27890), epic de API (#30159), pasta y loopback (#24045), DNS de
  aardvark (#29130, #26917), `HEALTHCHECK NONE` (#29976)
- podman-static #183 (binarios sin soporte systemd)
- Red Hat: nombres cortos de imagen; grupos suplementarios; overlay rootless
- CNCF (nov. 2025): vulnerabilidades de escape en runc
- Notas de versión: Podman 6.0 y 6.1 (blog.podman.io, linuxiac.com), Docker 29
  (docs.docker.com)
- Paquetes: packages.debian.org, archlinux.org/packages,
  packages.fedoraproject.org
