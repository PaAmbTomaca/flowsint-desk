# 8 · Arquitectura

## Idea

Flowsint está pensado para un servidor: estado en disco, puertos abiertos. Aquí
se envuelve **desde fuera**, sin tocar su código, para que en un PC de
escritorio todo el estado viva en RAM y lo único que persista sean copias
cifradas.

```
              tu equipo                                    nube / disco externo
┌──────────────────────────────────────────┐
│ navegador ── 127.0.0.1:5273              │
│                  │                       │
│  ┌───────────────▼────────────────────┐  │
│  │ /tmp/flowsint-desk-<boot_id>/ (RAM)│  │
│  │  app ─ api ─ celery                │  │
│  │         │       │                  │  │        ┌──────────────────┐
│  │  postgres  neo4j  redis            │  │        │ almacén (restic) │
│  │  staging/ (volcados en claro)      │──┼──────► │ cifrado          │
│  └────────────────────────────────────┘  │ backup └──────────────────┘
│  flowsint-desk up  (proceso de sesión)   │             │ mirror
│  ~/.config  (config, sin secretos)       │        ┌────▼─────────────┐
│  ~/.cache   (imágenes públicas)          │        │ segunda copia    │
└──────────────────────────────────────────┘        └──────────────────┘
```

## Piezas

| Pieza | Qué es |
|-------|--------|
| `flowsint-desk` | Un binario Go. No es un servicio: `up` es un proceso en primer plano que dura lo que la sesión |
| Motor de contenedores | Docker rootless o Podman rootless, con **todo su estado en la tmpfs** de la sesión |
| El stack | Seis contenedores: `postgres`, `redis`, `neo4j`, `api`, `celery`, `app` (nginx). Imágenes oficiales |
| Proxy del socket | Parte del proceso `up`. Es el "Docker" que ve Flowsint |
| restic | Cifra, deduplica y versiona. La CLI lo invoca como programa externo |
| Almacén | Carpeta con el repositorio restic y el fichero de bloqueo |

## Los dos motores

El código pide siempre "docker" a `internal/dock`; con Podman se ejecuta
`podman`, que acepta la misma sintaxis para todo lo que se usa. Lo que difiere
está en dos ficheros.

| | Docker | Podman |
|---|---|---|
| Arranque | `dockerd-rootless.sh` con `--data-root` y `--exec-root` en la tmpfs | No hay daemon. `storage.conf` y `containers.conf` propios apuntan todo a `<runDir>/podman-data` |
| Levantar el stack | `docker compose up --wait` con el compose embebido | La CLI ejecuta seis `podman run` (`podmanSpec`) y espera ella misma a que cada servicio responda |
| Socket para los enrichers | El del daemon | `podman system service`, arrancado solo para eso |
| Borrar el estado | Contenedor auxiliar dentro del user namespace | `podman unshare`: desmonta el overlay y borra |
| Logs de contenedores | `json-file` en el data-root | Forzados a fichero (por defecto irían al diario, en disco) |

Con Podman **no** se usa `docker compose` contra su API emulada ni sus
healthchecks. El porqué: [`../decisions/0008-two-container-engines.md`](../decisions/0008-two-container-engines.md).

El compose y `podmanSpec` describen el mismo stack y se mantienen a mano; un
test falla si divergen.

## Qué pasa en `up`

1. Carga y valida la configuración. Se niega como root. Fija `umask 077` y
   desactiva los volcados de memoria.
2. Crea `/tmp/flowsint-desk-<boot_id>/` y exige que sea un directorio real,
   propio, privado y en tmpfs.
3. Registra las señales (desde aquí Ctrl+C cancela limpiamente).
4. Limpia restos de sesiones muertas y escribe `session.json` en fase
   `arrancando`.
5. Comprueba que el puerto está libre.
6. Resuelve la frase (fichero de `init` → llavero → terminal) y abre el almacén.
7. Toma el bloqueo. **Desde aquí, cualquier error desmonta todo antes de salir.**
8. Cada N días, comprueba la integridad del almacén.
9. Restaura el último snapshot a la tmpfs y saca de él los secretos, **antes**
   de crear las bases de datos.
10. Arranca el motor y el proxy del socket.
11. Carga las imágenes (caché verificada, o descarga).
12. Levanta `postgres`, `redis`, `neo4j` y espera a que respondan.
13. Carga los volcados, reaplica los índices de Neo4j y **verifica contra el
    manifest**.
14. Levanta `api`, `celery`, `app`.
15. Marca la sesión como `lista` y vigila el apagado del equipo.
16. Bucle: snapshot cada N minutos, latido del bloqueo cada minuto, y a la
    espera de señales.

## Qué pasa al cerrar

1. Llega Ctrl+C, `down` (SIGTERM) o el aviso de apagado.
2. Snapshot final: volcado de Postgres, export de Neo4j, secretos, manifest →
   `restic backup`.
3. **Si falla, no sigue**: la sesión continúa. Solo una segunda orden dentro de
   2 minutos cierra sin guardar.
4. Si hay segunda copia configurada y accesible, la actualiza.
5. `teardown`: para los contenedores, borra el estado del motor desde dentro de
   su user namespace, para el motor, mata lo que cuelgue de ese directorio y
   borra el directorio.
6. Suelta el bloqueo.

`teardown` es el único camino de desmontaje: lo usan el cierre normal, un
arranque fallido o cancelado, y `down` sobre una sesión muerta.

## Dónde vive cada cosa

| Dato | Dónde | En claro | Sobrevive al cierre |
|------|-------|----------|---------------------|
| Bases de datos vivas | tmpfs (volúmenes del motor) | sí | no |
| Volcados para el snapshot | tmpfs (`staging/`, `restore/`) | sí | no |
| Secretos de Flowsint | tmpfs (`secrets.env`, ficheros de entorno) | sí | no (van en el snapshot) |
| Frase de paso | tmpfs (`restic-pass`) y, si quieres, llavero | sí | solo en el llavero |
| Logs de contenedores | tmpfs | sí | no |
| Snapshots | almacén | **no** (cifrados) | sí |
| Bloqueo entre equipos | almacén | sí (equipo, pid, fechas) | hasta que se suelta |
| Configuración | `~/.config` | sí, sin secretos | sí |
| Imágenes | `~/.cache` | sí, públicas | sí |

## El bloqueo entre equipos

`flowsint-desk.lock.json` en el almacén: nombre del equipo, identificador de
máquina (hash), pid, arranque, inicio y último latido.

- Al abrir: si es de otro equipo y su latido tiene menos de 15 minutos, se
  niega; si es más viejo, pide `unlock`.
- Cada minuto se renueva el latido.
- Antes de cada snapshot se relee: si ya no es nuestro, no se guarda.
- Se escribe de forma atómica (temporal + renombrado).

No es exclusión mutua real: pCloud no es atómico entre equipos.

## El código

```
main.go
internal/
  cli/         comandos (cobra) y el asistente
  config/      config.toml: carga, validación, guardado
  run/         directorio de ejecución en tmpfs y sus comprobaciones
  passphrase/  frase de paso: llavero y terminal
  secrets/     secretos de Flowsint y enmascarado
  dock/        motor: Docker/Podman, imágenes, caché, stack
  sockproxy/   proxy filtrado del socket
  snap/        contenido del snapshot, manifest y verificación
  store/       envoltorio de restic
  lock/        bloqueo entre equipos
  session/     session.json
assets/
  compose.yml          stack para el motor Docker (embebido)
  migrations/*.cypher  índices de Neo4j que se reaplican al restaurar
ci/            scripts de las pruebas reales
```

## Decisiones

Cada elección importante tiene su registro en [`../decisions/`](../decisions/):
por qué contenedores sin privilegios, por qué restic, por qué el grafo se
exporta con APOC, por qué no se cierra si no se puede guardar, por qué dos
motores.
