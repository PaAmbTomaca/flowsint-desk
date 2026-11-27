# ADR 0011 — Imágenes: versión fijable, digest anotado, caché verificada

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-26
- **Contexto**: Las imágenes van por etiqueta móvil (`latest`, `15-alpine`,
  `neo4j:5`). Hallazgo A1 (alto) de la auditoría del 20/11. Tres problemas:
  no se sabía qué imagen exacta se ejecutaba (el manifest decía guardar los
  digests pero quedaban vacíos, porque `docker load` desde la caché no conserva
  los RepoDigests); torre y portátil podían tener versiones distintas de
  Flowsint, y la API migra el esquema al arrancar; y los `.tar` de la caché se
  cargaban sin ninguna comprobación.

## Decisión

- **No se fijan digests en el código.** Se quedarían viejos y no se podían
  verificar al escribirlos. Se fija **por almacén**: manda el último snapshot.
- Al descargar una imagen se anota su digest junto al `.tar` y el sha256 del
  propio `.tar`.
- El manifest de cada snapshot recoge los digests.
- Al restaurar, `up` compara y **avisa** de las imágenes que difieren.
- `images sync` descarga por digest las del último snapshot.
- Antes de cada `load` se verifica el sha256 del `.tar`; si no coincide, no se
  carga.
- `flowsint_version` permite fijar la etiqueta de las imágenes de Flowsint.
- Con Podman los nombres van completos (`docker.io/library/…`); al comparar se
  ignora ese prefijo.

## Consecuencias

- La primera descarga confía en el registro en ese momento.
- La divergencia entre equipos se avisa, no se impide: es el usuario quien
  ejecuta `images sync`.
- Una caché de una versión anterior, sin sumas, se acepta y se anota entonces.

## Validación

Tests unitarios (comparación de digests, verificación de la caché). El CI real
pasa por la descarga, el guardado y la carga verificada con ambos motores.
`images sync` contra un registro, solo con dobles.

## Referencias

`internal/dock/dock.go`, `internal/cli/images.go`.
