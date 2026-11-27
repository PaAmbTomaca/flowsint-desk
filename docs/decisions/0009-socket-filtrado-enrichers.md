# ADR 0009 — Flowsint no recibe el socket real del motor: proxy filtrado

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-26
- **Contexto**: Seis enrichers de Flowsint (subfinder, httpx, naabu, dnsx,
  asnmap, mapcidr) lanzan contenedores con el SDK de Python de Docker contra
  `/var/run/docker.sock`. El compose montaba ahí el socket del motor de la
  sesión. Quien habla con ese socket puede pedir cualquier cosa: un RCE en la
  API, o un enricher manipulado, podía crear un contenedor con el home del
  usuario montado, o inspeccionar los contenedores de las bases de datos y leer
  sus contraseñas. `:ro` en un socket no limita nada. Era el hallazgo A2 (alto)
  de la auditoría del 20/11.

## Decisión

Lo que se monta en `api` y `celery` es un socket que atiende la propia CLI
(`internal/sockproxy`), delante del real.

- **Lista de endpoints permitidos**, sacada de leer
  `flowsint-enrichers/src/tools/dockertool.py`: ping, versión, pull e inspect
  de imágenes, y create / start / wait / logs / attach / stop / kill / delete
  de contenedores. Lo demás, 403.
- **`HostConfig` contra una lista de claves permitidas.** Cualquier otra clave
  debe venir vacía. Es deliberado que sea lista de lo permitido: una opción
  nueva del motor se deniega sin que nadie tenga que acordarse.
- Se rechaza: montajes, privilegios, capacidades, dispositivos, publicar
  puertos, compartir namespaces, políticas de reinicio, la red del stack, y
  rutas con `..` o barras codificadas.
- **Solo se opera sobre contenedores creados a través del proxy.**
- **Cada create se reescribe**: sin capacidades, `no-new-privileges` y, si el
  sistema delega los controladores de cgroup, tope de memoria y de procesos.
- Clave `docker_socket`: `filtrado` (por defecto), `directo`, `no`.

## Consecuencias

- Un fallo en Flowsint ya no equivale a "todo lo que el usuario puede leer".
- Si Flowsint cambia cómo lanza contenedores, puede hacer falta ampliar la
  lista. `up` imprime cada denegación; la respuesta correcta es añadir la
  opción con su test, no pasar a `directo`.
- El proxy vive en el proceso `up`: si `up` muere, los enrichers pierden el
  socket (y la sesión ya está muerta de todos modos).
- No limita la salida a internet de esos contenedores ni qué imagen descargan.
- Vale igual para Docker y para Podman, y resuelve de paso el problema de
  permisos del socket para el usuario sin privilegios del contenedor (el socket
  del proxy es accesible dentro; fuera lo protege un directorio `0700`).

## Alternativas descartadas

- **tecnativa/docker-socket-proxy**: filtra por endpoint, no por el contenido
  del create; no impide un montaje.
- **Quitar el socket**: deja sin funcionar seis enrichers. Es la opción `no`.
- **Parchear Flowsint**: contra la regla del proyecto.

## Validación

Tests contra un daemon simulado con HTTP real. En el CI, con ambos motores y
software real: subfinder lanza su contenedor, y montar `/etc`, publicar un
puerto y un contenedor privilegiado salen denegados por el proxy. La red del
stack y las rutas con trucos, solo en test unitario.

## Referencias

`internal/sockproxy/`, `docs/manual/07-seguridad.md`,
`docs/security/2026-11-auditoria-seguridad.md` (A2).
