# flowsint-desk

> English version: [README.en.md](README.en.md)

**Repositorio público en GitHub** · Generado con **Mistral Large 4**

Flowsint (OSINT de grafos) en tu PC de escritorio, **efímero y cifrado**:

- Los datos solo existen **en claro en RAM** (tmpfs) mientras la app está abierta.
  Al cerrar (o al apagar el PC) no queda nada en el disco local.
- La persistencia son **copias cifradas con restic** en una carpeta de tu pCloud.
  Con la frase de paso, cualquier equipo con esta CLI recupera todo (datos y los
  secretos internos de Flowsint: `AUTH_SECRET`, `MASTER_VAULT_KEY_V1`, contraseñas
  de Postgres/Neo4j y el vault de claves de API).
- **Sin modificar Flowsint**: usa las imágenes oficiales tal cual; todo se configura
  desde fuera (compose embebido en el binario).
- **Sin sudo**: Docker rootless, con su propio daemon por sesión, data-root en tmpfs.
- Solo se publica `127.0.0.1:5273`. Ni la API, ni Neo4j, ni Postgres, ni Redis salen
  de loopback. Redis va sin persistencia (`--save "" --appendonly no`).

## Por qué existe flowsint-desk

Flowsint está pensado para un **servidor**: su `docker-compose.prod.yml` publica
puertos en `0.0.0.0` y asume estado persistente en disco. En un PC de escritorio
con datos OSINT sensibles eso no vale: no queremos exponer nada fuera de loopback
ni dejar datos en claro en el disco local. `flowsint-desk` envuelve Flowsint con
esas garantías **desde fuera, sin tocar su código**: todo el estado vive en
tmpfs (RAM) mientras la sesión está abierta —incluidos los volúmenes de Postgres
y Neo4j y los logs de los contenedores, porque el data-root del Docker rootless
está en tmpfs—; Redis no persiste nada; y lo único que sobrevive en disco es el
repositorio restic **cifrado** en pCloud, protegido solo por la frase de paso.
Al cerrar la sesión (o al apagar el PC) la tmpfs se va con la RAM: no queda nada
en claro en el disco local.

## Créditos

El mérito del motor OSINT es de **[Flowsint](https://github.com/reconurge/flowsint)**
y su equipo: API, grafos en Neo4j, enrichers que lanzan contenedores, vault de
claves cifrado... un curro enorme. Nuestra decisión de desplegarlo en local con
Docker rootless y efímero se apoya en su arquitectura (p. ej. el socket de Docker
que montan `api`/`celery` para los enrichers). Esta CLI solo añade la capa de
escritorio efímera y cifrada por fuera, usando las imágenes oficiales tal cual.

## Requisitos e instalación

**Un solo comando deja el equipo listo: `flowsint-desk setup`.** Es un
asistente que se puede repetir (lo que ya está bien, se salta):

1. **Requisitos**: comprueba todo; instala en `~/.local/bin` lo que no necesita
   sudo (restic con su sha256 oficial; en Arch/CachyOS, los binarios rootless
   con la firma del paquete verificada) y, para lo que sí lo necesita, te da el
   comando exacto de tu distribución (`apt` en Debian, `pacman` en Arch),
   espera a que lo ejecutes y vuelve a comprobar.
2. **Almacén**: localiza pCloud Drive, propone la ruta y distingue **primer
   equipo** (crea el almacén) de **segundo equipo** (se conecta al que ya hay,
   sin crear nada).
3. **Imágenes**: las descarga a la caché; en un segundo equipo, las mismas (por
   digest) que usó el equipo que guardó por última vez.
4. **Prueba de humo** opcional (`selftest`): un ciclo completo real con un
   almacén desechable, sin tocar tus datos.
5. Arranque.

La CLI nunca ejecuta sudo. `setup --yes` no pregunta; `setup --vault RUTA`
indica el almacén a mano (si no usas pCloud Drive o no lo detecta).

`flowsint-desk doctor` comprueba **todos** los requisitos y, si falta algo,
imprime el paso exacto para arreglarlo (`→ arreglar: ...`). Con
`flowsint-desk doctor --fix` arregla automáticamente lo que no requiera sudo:
descarga restic a `~/.local/bin` verificando su sha256 oficial, los binarios
rootless (`rootlesskit`, `slirp4netns`, `fuse-overlayfs`, `dockerd-rootless.sh`)
desde los mirrors oficiales, y crea la caché de imágenes y `config.toml` con
permisos 0600. Lo que sí requiera sudo (`/etc/subuid`/`/etc/subgid`, montar
`/tmp` como tmpfs, sysctls) solo se imprime.

Chequeos: binarios rootless y `newuidmap`/`newgidmap`; entradas en
`/etc/subuid` y `/etc/subgid`; user namespaces habilitados; kernel ≥ 5.11;
restic ≥ 0.17; `docker compose` v2; `/tmp` en tmpfs con ≥ 8 GiB libres;
≥ 4 GB de RAM libre; puerto libre; almacén existente, escribible y sobre el
montaje FUSE de pCloud; repo restic inicializado; estado del bloqueo entre
equipos; `config.toml` con permisos 0600 y sin claves con pinta de secreto;
llavero Secret Service; `systemd-inhibit`; caché de imágenes escribible con
≥ 6 GiB libres en `$HOME`; conectividad HTTPS a `ghcr.io` y `github.com`.

Instalación mínima sin sudo (Arch/CachyOS):

```sh
# restic (binario estático)
curl -LO https://github.com/restic/restic/releases/download/v0.19.1/restic_0.19.1_linux_amd64.bz2
bzip2 -d restic_0.19.1_linux_amd64.bz2 && chmod +x restic_0.19.1_linux_amd64
mv restic_0.19.1_linux_amd64 ~/.local/bin/restic
# rootless (paquetes Arch extraídos a mano, sin root)
for p in rootlesskit-3.3.0-1-x86_64 slirp4netns-1.3.7-1-x86_64 fuse-overlayfs-1.18-1-x86_64; do
  curl -O "https://geo.mirror.pkgbuild.com/extra/os/x86_64/$p.pkg.tar.zst"
  tar --zstd -xf "$p.pkg.tar.zst" && cp usr/bin/* ~/.local/bin/ 2>/dev/null
done
curl -o ~/.local/bin/dockerd-rootless.sh https://raw.githubusercontent.com/moby/moby/master/contrib/dockerd-rootless.sh
chmod +x ~/.local/bin/dockerd-rootless.sh
# subuid (esto sí necesita sudo una vez)
echo "$(whoami):100000:65536" | sudo tee -a /etc/subuid | sudo tee -a /etc/subgid >/dev/null
```

(Ese bloque es justo lo que hace `flowsint-desk setup` automáticamente salvo el
paso de `sudo`, que siempre queda para ti.)

## Uso

```sh
flowsint-desk setup                           # asistente: deja este equipo listo de principio a fin
flowsint-desk doctor                          # todos los requisitos, con cómo arreglar cada uno
flowsint-desk doctor --fix                    # arregla automáticamente lo que no requiera sudo
flowsint-desk init --vault ~/pCloudDrive/flowsint-vault
#   → PRIMER equipo: pide la frase dos veces (mín. 8; avisa por debajo de 16),
#     crea el repo restic y el primer snapshot, y ofrece el llavero.
#   → SEGUNDO equipo (el almacén ya tiene snapshots): pide la frase una vez, la
#     comprueba y NO crea ni modifica nada en el almacén.
flowsint-desk up                              # desbloquea, restaura y abre la UI
#   ✔ almacén desbloqueado · ✔ sin bloqueo de otro equipo · ✔ restaurado snapshot 3f2a…
#   ✔ Flowsint listo en http://127.0.0.1:5273  (Ctrl+C para guardar y cerrar)
flowsint-desk status                          # sesión, bloqueo (host/latido), último snapshot
flowsint-desk backup                          # snapshot inmediato de la sesión en marcha
flowsint-desk snapshots                       # fecha, host, tamaño, recuentos del manifest
flowsint-desk up --snapshot 3f2a9c…           # arrancar desde un snapshot antiguo
flowsint-desk down                            # cierre ordenado; si la sesión murió (kill -9), limpia sus restos
flowsint-desk unlock --force                  # romper un bloqueo caducado de otro equipo
flowsint-desk forget                          # retención (24 h cubiertas, 30 d, 12 m) + prune
#   (keep-hourly=N de restic = N horas cubiertas, 1 snapshot por hora, más el
#    más antiguo de cada bucket; keep-daily/keep-monthly igual por día/mes)
flowsint-desk passwd                          # cambiar la frase de paso
flowsint-desk verify [ID]                     # ¿la copia está entera? restic check + sha256 del snapshot, sin abrir sesión
flowsint-desk selftest                        # prueba de humo: ciclo completo real con un almacén desechable
flowsint-desk images [pull|update|sync]       # imágenes de este equipo vs. las del último snapshot; sync las iguala por digest
flowsint-desk unlock --restic                 # quitar bloqueos huérfanos de restic (tras un kill -9 en mitad de un backup)
flowsint-desk version
flowsint-desk logs [--tail N] [filtro]        # logs de los contenedores de la sesión viva, saneados (secretos → ***)
```

> Nota: la frase se resuelve en este orden: `runDir/passphrase` (tmpfs, la deja
> `init` hasta el primer `up`) → llavero → prompt. Si la guardada no abre el
> almacén (llavero con la frase de otro almacén, o desfasado tras un `passwd`
> hecho en el otro equipo), se pide por terminal en vez de fallar.

En `up` queda en primer plano: guarda automáticamente cada 10 min (configurable),
renueva el latido del bloqueo cada minuto y, con Ctrl+C/SIGTERM o al apagar el equipo
(`systemd-inhibit --what=shutdown --mode=delay`), hace un **snapshot final** antes de
tirar todo y borrar el directorio de ejecución.

**Si el snapshot final falla, la sesión NO se cierra** (pCloud desmontado, bloqueo
perdido, error de restic...): cerrar borraría de la RAM lo único que queda del
trabajo sin guardar. `up` dice qué ha fallado y hace cuánto fue el último snapshot
bueno; arréglalo y repite Ctrl+C o `down`. Para cerrar **sin guardar**, repite la
orden antes de 2 minutos. Al apagar el equipo no hay segunda oportunidad: si no da
tiempo o pCloud ya se ha ido, vale el último snapshot automático.

Un arranque que falla o que cancelas con Ctrl+C se desmonta entero (daemon,
contenedores, secretos y volcados restaurados): no queda nada descifrado en RAM.

## Qué hay en un snapshot (cifrado por restic, solo en pCloud)

- `postgres.sql` — `pg_dump` en texto plano (restic cifra y deduplica después).
- `neo4j.cypher` — export APOC del grafo completo (formato plain; restaurado con
  `cypher-shell -f` en una BD vacía; verificado en pruebas: idéntico salvo `elementId`,
  que nada persiste fuera de Neo4j). Tras restaurar se re-aplican las migraciones
  Neo4j idempotentes (el export no trae índices range).
- `secrets.env` — los secretos internos de Flowsint.
- `manifest.json` — versión de la CLI, digests de las imágenes, host, fecha y
  recuentos de control (filas por tabla de Postgres, nodos por etiqueta y relaciones
  por tipo en Neo4j) + sha256 de cada fichero. Tras restaurar se comparan; **si no
  coinciden, no se arranca la interfaz**.

## Seguridad

- **Nunca como root**: la CLI se niega a arrancar con `euid=0` (prohibido
  `sudo flowsint-desk ...`), así que desde aquí no se puede tocar el Docker
  rootful del sistema.
- **Todo el estado en RAM**: el data-root del Docker rootless está en tmpfs, así
  que los volúmenes de Postgres/Neo4j y los logs de los contenedores solo viven
  en RAM mientras la sesión está abierta; Redis va con `--save "" --appendonly no`.
  Lo único persistente en disco es el repo restic **cifrado** en pCloud (más la
  config sin secretos y la caché de imágenes públicas). Al apagar, la tmpfs
  desaparece: los datos quedan blindados solo en los exports cifrados.
- `umask 077`, `RLIMIT_CORE=0`, ningún secreto en argv ni en logs. La frase de paso
  viaja a restic por `RESTIC_PASSWORD_FILE` (fichero 0600 en tmpfs, borrado al final).
- **Avisos y logs no comprometedores**: antes de imprimir cualquier salida externa
  (compose, restic, `docker logs`) se enmascaran los secretos y la frase (`***`);
  si falla el arranque, `up` vuelca automáticamente los logs de los contenedores
  implicados ya saneados, y `flowsint-desk logs` los muestra en vivo.
- **Socket de Docker filtrado**: Flowsint necesita un socket de Docker para sus
  enrichers (subfinder, httpx...). No ve el real, sino un proxy de la CLI que
  solo deja descargar imágenes y lanzar contenedores **sin montajes, sin
  privilegios y sin compartir red** con otros contenedores, y que no deja
  inspeccionar los de las bases de datos. Así un fallo en Flowsint no llega a tu
  home. `docker_socket = "directo"` restaura el socket sin filtrar (avisa en
  cada arranque); `"no"` lo cierra del todo.
- **Imágenes**: cada snapshot anota el digest de las imágenes que lo crearon;
  al restaurar, `up` avisa si este equipo ejecuta otras y `images sync` las
  iguala. Fija `flowsint_version` para no depender de `latest`.
- **Apagado del equipo**: `up` se entera de que el apagado empieza (logind) y
  lanza el snapshot final en ese momento. La ventana la fija logind (5 s de
  fábrica): `doctor` te dice cómo ampliarla. Si no cabe, vale el último
  snapshot automático.
- **Swap**: tmpfs puede paginarse a swap. Si tu swap está en disco y sin cifrar
  (típico con hibernación), datos de la sesión pueden acabar en claro en el
  disco. `doctor` avisa de cualquier swap que no sea zram; no hibernes con la
  sesión abierta.
- 🔎 **Auditoría completa del repo** (2026-11-26): [`docs/security/2026-11-26-auditoria-repo.md`](docs/security/2026-11-26-auditoria-repo.md) — 20 fallos corregidos (pérdida de datos al cerrar, `init` sobre un almacén existente, retención que no borraba, bloqueo con hostnames repetidos...) y lo que sigue pendiente.
- 🛡 **Auditoría de seguridad severa** (2026-11-20): [`docs/security/2026-11-auditoria-seguridad.md`](docs/security/2026-11-auditoria-seguridad.md) — hallazgos con severidad, evidencia y mitigaciones (2 altos: imágenes sin pin de digest y socket rootless en api/celery; 3 medios; 8 controles verificados en verde).
- Antes de escribir nada se comprueba con `statfs` que el directorio de ejecución
  está en tmpfs (`TMPFS_MAGIC`); si no, se niega.
- El Docker del sistema (rootful) no se toca nunca: todo va por `DOCKER_HOST`
  al socket rootless de la sesión.
- Bloqueo entre equipos: `flowsint-desk.lock.json` en el almacén con host, pid,
  boot_id y latido (15 min). pCloud no es atómico: es una protección para una
  persona con dos equipos, no para concurrencia real. Antes de cada snapshot se
  relee el bloqueo; si otro equipo lo cogió, se deja de guardar y se avisa.
- Tras un `kill -9` o corte de luz, el siguiente `up` detecta los restos del mismo
  `boot_id`, mata los daemons huérfanos, los barre y restaura el último snapshot.
- La caché de imágenes (`~/.cache/flowsint-desk/images/`) sí vive en disco: las
  imágenes públicas no contienen datos de usuario.

## Configuración

`~/.config/flowsint-desk/config.toml` (0600, **sin ningún secreto**):

```toml
vault_path = "/home/pollo/pCloudDrive/flowsint-vault"
port = 5273
save_interval_min = 10
keep_hourly = 24
keep_daily = 30
keep_monthly = 12
docker_socket = "filtrado"     # socket de Docker que ve Flowsint: "filtrado" | "directo" | "no"
flowsint_version = "latest"    # etiqueta de las imágenes de Flowsint; fíjala (p. ej. "1.2.10") para no cambiar de versión sin querer
```

## Documentación

🗺️ **Mapa visual de toda la arquitectura** (componentes, flujo de `up`, dónde vive
cada dato, snapshots, seguridad, comandos): [`docs/mapa-arquitectura.html`](docs/mapa-arquitectura.html)
(ábrelo en el navegador).

Las decisiones de arquitectura viven en [`docs/`](docs/README.md), estilo ADR:
`docs/decisions/` son las decisiones **ya vigentes** (Docker rootless efímero,
restic en pCloud, datos solo en tmpfs, exposición solo en loopback, frase de
paso única, método de snapshot Neo4j) y `docs/proposals/` las **propuestas
pendientes** (p. ej. P001: Podman como backend opcional en v2).

## Desarrollo

```sh
go build -o dist/flowsint-desk .
go vet ./...
go test ./...     # sin Docker ni restic: lógica de bloqueo, snapshots, config, saneado...
```

Los tests unitarios no levantan nada; la batería de aceptación real (sección 7
de `ENCARGO.md`) sigue siendo manual, contra Docker rootless y un almacén de
prueba. CI (`.github/workflows/ci.yml`) ejecuta build + vet + test en cada push.
