# ENCARGO: flowsint-desk — Flowsint de escritorio, efímero y con copias cifradas en pCloud

REGLAS DURAS (léelas antes que nada; cada una evita un desastre que ya ocurrió):
- ~~GIT PROHIBIDO ENTERO~~ **RETIRADO POR ORDEN DEL OPERADOR DIRECTA (2026-11-17)**: el agente
  puede crear el repo remoto `github.com/PaAmbTomaca/flowsint-desk` y publicar el contenido
  de este árbol (vía gh/git/API de GitHub). El resto de reglas duras siguen vigentes.
- Si algo te sale mal, NO INTENTES REVERTIRLO. Descríbelo y para.
- NUNCA imprimas claves, contraseñas ni la frase de paso, ni depurando ni con set -x.
- NUNCA pongas un secreto en la LÍNEA DE COMANDO (PGPASSWORD=x psql..., restic -p x...):
  queda en logs y en la lista de procesos. Usa ficheros 0600 en tmpfs o `set -a; . f; set +a`.
- Para comprobar que una variable existe: `test -n "$VAR" && echo DEFINIDA`. Nunca su valor.
- PROHIBIDO sudo. PROHIBIDO tocar el Docker del sistema (rootful) y sus contenedores,
  imágenes, volúmenes o redes: hay contenedores de trabajo del usuario ahí.
  No ejecutes ningún `docker` sin DOCKER_HOST apuntando al socket rootless de este proyecto.
- NUNCA ejecutar flowsint-desk como root (la CLI se niega con euid=0 → exit 1).
  [Añadido 2026-11-17, orden del operador]
- HARDRULE commits: granulares, un cambio lógico por commit, mensaje con QUÉ y POR QUÉ;
  si hace falta, engrosar `docs/` (ADR) con el porqué. Prohibidos los commits masivos.
  [Añadido 2026-11-17, orden del operador]
- El código de referencia de Flowsint se descarga SIN git, como tarball, en dist/flowsint-ref
  (ver sección 3). Es solo para leer: NO lo modifiques.
- NO uses pkill -f ni pgrep -f (se matan a sí mismos). Usa pkill -x o filtra por `comm`.
- Un comando RECHAZADO por el sistema no es un comando en verde. Pega el rechazo y NO
  des el paso por hecho.
- TODO CERO NECESITA UN CONTROL POSITIVO EN EL MISMO COMANDO: antes de afirmar que algo
  no aparece (un secreto en disco, un texto en claro en pCloud), demuestra que ese mismo
  comando SÍ lo encuentra donde debe estar.
- Si algo no existe, REPÓRTALO; no lo crees ni lo inventes.
- No cites un fichero, paquete, endpoint o flag sin haberlo abierto o probado.
- `go build`/`go vet`/`go test` SOLO en los puntos de control indicados, y pega solo
  las últimas líneas (`2>&1 | tail -15`).
- Pega la salida LITERAL y CRUDA de cada comando de verificación. No la resumas.
- Trabaja por FASES. Al final de cada fase PARA y entrega el informe con el esquema del
  final. No empieces la siguiente fase sin que te lo digan.

---

## 1. Qué hay que construir

Una CLI en Go, `flowsint-desk`, para usar Flowsint (herramienta OSINT de grafos,
https://github.com/reconurge/flowsint) en un PC de escritorio sin servidor, con estas garantías:

1. **Los datos solo existen en claro en RAM** mientras la app está abierta.
   Al cerrar, o al apagar el PC, no queda nada en el disco local.
2. **La persistencia son copias de seguridad cifradas con restic** en una carpeta de pCloud
   (pCloud Drive montado como unidad local). Así el usuario trabaja desde dos equipos
   (torre y portátil) con los mismos datos.
3. **Una sola llave: la frase de paso.** Con ella, cualquier equipo con la CLI recupera
   todo, incluidos los secretos internos de Flowsint.
4. **Sin modificar el código de Flowsint.** Se usan las imágenes oficiales tal cual;
   todo se configura desde fuera.
5. **Sin sudo**: Docker rootless.

Experiencia de uso:
```
$ flowsint-desk up
Ruta del almacén (pCloud): /ruta/pcloud/flowsint-vault      ← solo la primera vez
Frase de paso: ********                                     ← o desde el llavero si se pidió
✔ almacén desbloqueado · ✔ sin bloqueo de otro equipo · ✔ restaurado snapshot 3f2a… (hace 2 h, torre)
✔ Flowsint listo en http://127.0.0.1:5273  (Ctrl+C para guardar y cerrar)
```

## 2. Decisiones YA TOMADAS (no las reabras; si ves un problema real, repórtalo y para)

- **La base de datos NO vive en pCloud.** Un Postgres o un Neo4j sobre una unidad sincronizada
  se corrompe. En pCloud solo va el repositorio restic.
- **restic, no git** (git no deduplica ficheros cifrados y su `.git` se rompe sobre
  una unidad sincronizada). restic cifra, deduplica y versiona.
- Los volcados van **en claro (texto) a restic**, que cifra después. No los cifres ni
  los comprimas antes: eso rompe la deduplicación. El volcado en claro solo puede tocar tmpfs.
- **Docker rootless**, con data-root en tmpfs. Las imágenes públicas sí pueden cachearse en
  disco (`docker save` en `~/.cache/flowsint-desk/`): no contienen datos del usuario.
- Flowsint escucha **solo en 127.0.0.1**, en un único puerto. Ni la API, ni Neo4j,
  ni Postgres, ni Redis se publican.
- **Redis sin persistencia** (`--save "" --appendonly no`): su configuración por defecto
  hace snapshots a disco, y los resultados de Celery pueden llevar datos OSINT.
- Los secretos de Flowsint (`AUTH_SECRET`, `MASTER_VAULT_KEY_V1`, las contraseñas de
  Postgres y Neo4j) se generan al hacer `init` y viajan **dentro del snapshot cifrado**.
- Bloqueo entre equipos: un fichero de bloqueo con latido en la carpeta del almacén.
- Fuera de alcance en la v1: GUI, Authelia/2FA, backend WebDAV/rclone, parches al código
  de Flowsint.

## 3. Hechos verificados sobre Flowsint (comprobados en un despliegue real; no los redescubras)

Código de referencia, solo lectura. Descárgalo así (sin git) como primer paso de la fase 0:
```
mkdir -p dist/flowsint-ref && curl -fsSL https://github.com/reconurge/flowsint/archive/refs/heads/main.tar.gz | tar -xz -C dist/flowsint-ref --strip-components=1
```
Ficheros clave: `docker-compose.prod.yml`, `flowsint-api/`, `flowsint-core/`, `flowsint-enrichers/`, `flowsint-app/nginx.conf`.
Las rutas de código de este encargo son relativas a `dist/flowsint-ref`.

**Servicios** (copia la estructura de `docker-compose.prod.yml`):
| Servicio | Imagen | Notas |
|---|---|---|
| postgres | `postgres:15-alpine` | estado: usuarios, investigaciones, sketches, permisos, vault de claves de API, scans, chats |
| neo4j | `neo4j:5` | estado: **el grafo completo**. `NEO4J_PLUGINS=["apoc"]`, `NEO4J_apoc_export_file_enabled=true`, `NEO4J_apoc_import_file_enabled=true`, `NEO4J_apoc_import_file_use__neo4j__config=true`. La contraseña debe tener 8 caracteres o más. Community: el `neo4j-admin database dump` oficial exige la base de datos parada |
| redis | `redis:7-alpine` | sin estado (broker y backend de Celery, pub/sub de eventos) |
| api | `ghcr.io/reconurge/flowsint-api` | puerto 5001, `/health`. Ejecuta las migraciones al arrancar |
| celery | misma imagen que api | `celery -A flowsint_core.core.celery worker --loglevel=info --pool=threads --concurrency=10`, `SKIP_MIGRATIONS=true` |
| app | `ghcr.io/reconurge/flowsint-app` | nginx en el 8080; sirve la interfaz y pasa `/api/` a `api:5001`. El compose de prod monta `flowsint-app/nginx.conf` (resolver para Podman): prueba primero SIN ese montaje |

**Variables de entorno** de api y celery: `DATABASE_URL=postgresql://U:P@postgres:5432/DB`,
`NEO4J_URI_BOLT=bolt://neo4j:7687`, `NEO4J_USERNAME`, `NEO4J_PASSWORD`, `AUTH_SECRET`,
`MASTER_VAULT_KEY_V1=base64:<32 bytes en base64>`, `REDIS_URL=redis://redis:6379/0`.
- Si se pierde `MASTER_VAULT_KEY_V1`, se pierden las claves de API cifradas (AES-256-GCM, HKDF por usuario).
- Si cambia `AUTH_SECRET`, todos los tokens dejan de valer.

**docker.sock**: api y celery montan el socket de Docker porque los enrichers **subfinder,
naabu y httpx** lanzan contenedores (`flowsint-enrichers/src/tools/network/*.py`,
`dockertool.py`). Monta el socket del Docker **rootless** de este proyecto, nunca el del
sistema. Es posible que naabu (escaneo de puertos con sockets raw) no funcione en rootless:
compruébalo y documenta el resultado, sin intentar arreglarlo con privilegios.

**API** (base `http://127.0.0.1:<puerto>/api`, o directa a api:5001 dentro de la red):
- `POST /auth/register` con JSON `{"email","password"}`
- `POST /auth/token`: formulario `username=<email>&password=...` → `{"access_token"}`. Bearer.
- `POST /investigations/create` con `{"name","description"}` → `{"id"}`
- `POST /sketches/create` con `{"title","description","investigation_id"}` → `{"id"}`
- `POST /sketches/{id}/nodes/add` con `{"id":null,"nodeLabel","nodeType","nodeMetadata":{},"nodeProperties":{...},"x","y"}`
- `POST /sketches/{id}/relations/add` con `{"source","target","type":"one-way","label"}`
- `GET /sketches/{id}/graph` → `{"nds":[...],"rls":[...]}`
- `POST /keys/...`: vault de claves de API (lee `flowsint-api/app/api/routes/keys.py` para la forma exacta)

**Trampas ya encontradas** (no vuelvas a caer en ellas):
- `nodeType` de teléfono = `phone`. `number` se valida con `phonenumbers`: los 555 españoles
  son inválidos y **rompen `GET /graph` con un 500 para todo el sketch**. Usa `+12025550123`
  (rango ficticio de EE. UU. 555-01xx, que sí valida).
- El monedero de criptomonedas es `nodeType: "cryptowallet"`, no `wallet` (un tipo desconocido también rompe el grafo).
- Los nodos se guardan en Neo4j con la etiqueta = nodeType y propiedades aplanadas con punto
  (`nodeProperties.number`, `nodeMetadata.created_at`), más `sketch_id`, `x`, `y` y `nodeLabel`.
  Los ids de nodo son `elementId()` de Neo4j: **cambian al restaurar**. Verifica (fase 0) si
  algo en Postgres guarda elementIds de Neo4j; si es así, una restauración por export de Cypher
  dejaría referencias colgando y hay que decidir el método con esa evidencia.
- `merge` de nodos ya falla en la versión upstream (error de sintaxis de Cypher): no lo uses en los tests.

## 4. Entorno de la máquina

- CachyOS (Arch), kernel 7.x, shell fish (los scripts en sh/bash), Go disponible (compruébalo).
- `/tmp` es tmpfs de 16 GB. `$XDG_RUNTIME_DIR` es tmpfs pero pequeño. RAM: 31 GB.
- Docker 29 rootful instalado y EN USO por otros proyectos (NO TOCAR). Compose v5.
- **El puerto por defecto de flowsint-desk es 5273**, configurable. Comprueba que está libre antes de usarlo.
- Lo que no sepas (rootless instalado, `/etc/subuid`, versión de restic, si pCloud está
  montado y dónde): compruébalo en la fase 0 y repórtalo.

## 5. Especificación de la CLI

Go con cobra. Para compose, invoca el binario `docker compose` con `DOCKER_HOST` apuntando al
socket rootless; para restic, invoca el binario `restic` (versión mínima comprobada en `doctor`).
El compose y la configuración se **incrustan en el binario** (`embed`) y se escriben en el
directorio de ejecución en tmpfs.

| Comando | Qué hace |
|---|---|
| `doctor` | Comprueba los requisitos y para cada uno dice cómo arreglarlo: rootless disponible, restic ≥ versión, `docker compose`, el directorio de ejecución es tmpfs (statfs, `TMPFS_MAGIC 0x01021994`), el almacén existe y se puede escribir en él, RAM libre ≥ 4 GB y puerto libre |
| `init` | Pide la ruta del almacén y la frase de paso (dos veces), crea el repositorio restic, genera los secretos y hace un primer snapshot vacío con `secrets.env` + `manifest.json`. Pregunta si recordar la frase en el llavero |
| `up` | Desbloquea → comprueba el bloqueo → coge el bloqueo → arranca rootless con data-root en tmpfs → carga las imágenes de la caché (o hace pull y la crea) → levanta el compose → restaura el último snapshot → espera a los healthchecks → imprime la URL. Queda en primer plano: guarda automáticamente cada N minutos (por defecto 10) y renueva el latido del bloqueo cada minuto. Con Ctrl+C, SIGTERM o al apagar el equipo: snapshot final → tirar todo → borrar el directorio de ejecución → soltar el bloqueo |
| `down` | Si hay un `up` vivo, le pide que se cierre de forma ordenada |
| `status` | Estado: corriendo o no, bloqueo (host, antigüedad del latido), último snapshot y su tamaño |
| `backup` | Snapshot inmediato de una sesión en marcha |
| `snapshots` | Lista los snapshots (fecha, host, tamaño, recuento de nodos del manifest) |
| `up --snapshot ID` | Arranca desde un snapshot antiguo |
| `unlock --force` | Rompe un bloqueo caducado de otro host, pidiendo confirmación |
| `forget` | Aplica la retención (por defecto: 24 por hora, 30 diarios, 12 mensuales) + prune. Se ejecuta automáticamente como mucho una vez al día, nunca en cada snapshot |
| `passwd` | Cambia la frase de paso (`restic key add` + quitar la clave antigua) |

**Contenido del snapshot** (se prepara en un directorio tmpfs con permisos 0700 y se pasa a restic como directorio):
- `postgres.sql`: `pg_dump` en texto plano, ejecutado dentro del contenedor.
- `neo4j.cypher` (o el formato que justifiques en la fase 0): export del grafo completo en texto.
- `secrets.env`: los secretos de Flowsint.
- `manifest.json`: versión de la CLI, **digests** de las imágenes, hostname, fecha, y recuentos
  de control (filas por tabla principal, nodos por etiqueta, número de relaciones).

Tras cada restauración, compara los recuentos con el manifest. **Si no coinciden, NO arranques
la interfaz**: avisa y para.

**Bloqueo**: `<almacén>/flowsint-desk.lock.json` con `{host, pid, boot_id, iniciado, latido}`.
- Al arrancar: si hay un bloqueo de otro host con latido de menos de 15 minutos, niégate.
  Si es más antiguo, avisa de que está caducado y sugiere `unlock --force`.
- Antes de cada snapshot, relee el bloqueo. Si ya no es tuyo (otro host lo cogió mientras
  pCloud sincronizaba), deja de guardar y avisa a gritos.
- Documenta que pCloud no es atómico: es una protección pensada para una persona con dos equipos, no para concurrencia real.

**Configuración local**: `~/.config/flowsint-desk/config.toml`, con la ruta del almacén,
el puerto, el intervalo y la retención. **Sin ningún secreto.** Frase de paso: opcional en el
llavero (`github.com/zalando/go-keyring`, que usa libsecret). Si no, se pide con `golang.org/x/term`.

**Requisitos de seguridad (todos obligatorios):**
- Ningún dato del usuario en claro fuera de tmpfs. Comprueba con statfs que es tmpfs antes de escribir; si no lo es, niégate.
- Ningún secreto en argv. A restic, la frase le llega con `RESTIC_PASSWORD_FILE` apuntando a un fichero 0600 en tmpfs que se borra al terminar, o por un descriptor de fichero.
- `umask 077`, `RLIMIT_CORE=0` y ningún secreto en los logs.
- Logs de los contenedores con un driver que no escriba fuera de tmpfs.
- Tras un `kill -9` o un corte de luz: en el siguiente `up` se detectan los restos del mismo arranque (boot_id), se limpian y se restaura el último snapshot.
- Al apagar el equipo: usa `systemd-inhibit --what=shutdown --mode=delay` para intentar el snapshot final. Si no da tiempo, vale el último snapshot automático: documéntalo.

## 6. Fases (PARA al final de cada una)

**Fase 0: comprobar supuestos, sin escribir código del producto.** Responde con evidencia cruda:
1. ¿Está disponible Docker rootless (`dockerd-rootless-setuptool.sh`, `newuidmap`, entradas en `/etc/subuid` para el usuario)? Si falta algo que requiere root, lista exactamente qué tendría que ejecutar el usuario con sudo y PARA.
2. ¿Funciona overlay con data-root en `/tmp` en modo rootless? Prueba mínima con `alpine`.
3. Versión de restic. ¿Dónde está montado pCloud? (no escribas en él todavía).
4. **El punto crítico**: ¿qué método de volcado y restauración de Neo4j conserva el grafo de forma que Flowsint lo lea idéntico? Levanta un stack desechable (rootless, puerto 5273), siembra un grafo pequeño con los endpoints de la sección 3, vuelca, destruye, restaura y compara `GET /graph` antes y después (nodeLabel, nodeType, propiedades, x/y, relaciones). Comprueba si Postgres guarda elementIds de Neo4j. Compara al menos APOC export Cypher (en línea) frente a `neo4j-admin database dump` (sin conexión), y mide para cada uno el tiempo y la deduplicación en restic (tamaño añadido por un segundo snapshot tras un cambio pequeño).
   Criterio: **si el grafo restaurado no es idéntico, el método NO ES VÁLIDO**, dígase lo que se diga del resto.

**Fase 1: arranque y parada.** `doctor`, `init` (sin restic todavía: secretos en tmpfs), `up` y `down` con rootless + compose + caché de imágenes. `go build` + `go vet`.

**Fase 2: snapshots.** restic, snapshot automático y final, restauración con verificación por manifest, `snapshots`, `up --snapshot`, `forget`.

**Fase 3: bloqueo, llavero, señales, limpieza tras un fallo, `systemd-inhibit`.**

**Fase 4: batería de aceptación** (sección 7) y README de uso para el usuario.

## 7. Pruebas de aceptación (fase 4; pega la salida cruda de cada una)

Usa un almacén de prueba en `dist/vault-test` y, al final, uno en la ruta real de pCloud que te indique el usuario.
1. `doctor` todo en verde.
2. `init` y luego `grep -ri` de la frase de paso y de los secretos en `~/.config/flowsint-desk`: 0 resultados. **Control positivo**: el mismo grep encuentra la frase en un fichero señuelo que crees a propósito en tmpfs.
3. `up` → siembra datos ficticios (dominios `.example`, IPs 192.0.2.0/24, teléfonos +1 202 555 01xx; incluye un `cryptowallet` y una clave de API en el vault) → `backup` → `down` → comprobar que el directorio de ejecución ha desaparecido, que no queda ningún contenedor rootless y que `find ~ -newer <marca> -type f` solo lista la configuración y la caché de imágenes.
4. `up` otra vez → el grafo es idéntico (comparación sección por sección), el login funciona con la misma contraseña y la clave de API se descifra.
5. **Fuga en claro**: `grep -r` de una cadena marcador de los datos sembrados en la carpeta del almacén → 0 resultados. **Control positivo** en el mismo comando: el marcador sí aparece en el volcado de staging o en la respuesta de la API.
6. Bloqueo: simula un segundo host (variable de entorno que fuerce el hostname) → se niega a arrancar. Con un latido caducado → sugiere `unlock --force`.
7. `kill -9` a la CLI con la sesión en marcha → el siguiente `up` limpia los restos y restaura el último snapshot.
8. Frase de paso incorrecta → error claro, no arranca nada, no se toca el bloqueo.
9. El Docker del sistema sigue intacto: `docker ps` (rootful, sin DOCKER_HOST) muestra los mismos contenedores que había antes de empezar. Toma esa foto en la fase 0.

## 8. Esquema OBLIGATORIO del informe de cada fase

```
fase: <n>
hecho: <lista breve>
ficheros_creados_o_tocados: <lista completa con rutas>
comandos_rechazados: si/no (+ el rechazo literal)
pruebas: <comando → salida cruda>
supuestos_refutados: <lo que resultó distinto de este encargo>
lo_que_NO_hice: <texto; no lo dejes vacío por defecto>
riesgos_abiertos: <texto>
```

Recordatorio de los parámetros clave (repetidos a propósito):
- Proyecto en este directorio.
- Referencia de solo lectura: dist/flowsint-ref (descargada como tarball, sin git).
- NO TOCAR: el Docker rootful del sistema ni sus contenedores.
- Puerto 5273. Sin sudo. Sin git. Ningún secreto en argv ni en logs.
- Los datos solo en tmpfs. PARA al final de cada fase.
