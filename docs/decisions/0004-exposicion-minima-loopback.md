# ADR 0004 — Exposición mínima: solo 127.0.0.1:5273; Redis sin persistencia

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11
- **Contexto**: Es una app de escritorio local con datos OSINT sensibles; el
  compose de producción upstream de Flowsint publica puertos en 0.0.0.0 y deja
  Redis con RDB en disco.

## Decisión

- El compose **embebido** (`assets/compose.yml`, escrito en el run dir en cada
  `up`) publica solo `127.0.0.1:${DESK_PORT}:8080` (puerto por defecto 5273).
  Ni la API, ni Neo4j, ni Postgres, ni Redis salen de loopback/red interna.
- Redis va con `--save "" --appendonly no`: los resultados de Celery pueden
  llevar datos OSINT y no deben tocar disco.
- El socket que montan `api`/`celery` para los enrichers es el del Docker
  **rootless de la sesión**, nunca el del sistema; `DOCKER_HOST` se inyecta en
  ambos servicios.
- Healthcheck de Neo4j deriva user/pass de `$NEO4J_AUTH`; el de Celery se
  desactiva (`healthcheck: disable: true`) porque el del Dockerfile falla
  siempre.

## Consecuencias

- **+** Superficie de ataque mínima: un solo puerto en loopback.
- **−** Nada accesible desde otros equipos de la LAN (intencionado).

## Alternativas consideradas

- Usar el compose prod upstream tal cual: rechazado (publica en 0.0.0.0,
  monta el socket rootful y deja Redis con persistencia).

## Referencias

- `ENCARGO.md` §2 y trampas; `assets/compose.yml`; `AGENTS.md`.

## Actualización 2026-11-26

- El socket del motor que se monta en `api` y `celery` ya no es el real sino un
  proxy filtrado: [ADR 0009](0009-socket-filtrado-enrichers.md). Entre otras
  cosas, impide que un contenedor lanzado por Flowsint publique un puerto fuera
  de loopback o entre en la red de las bases de datos.
- Un test exige que, en el compose, solo `app` publique puerto y solo en
  `127.0.0.1`; y que la versión para Podman sea idéntica.
- Límite documentado: `127.0.0.1:5273` es accesible para cualquier usuario o
  programa local, y Flowsint permite registrarse.
