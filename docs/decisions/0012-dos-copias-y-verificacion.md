# ADR 0012 — Segunda copia opcional y comprobación periódica del almacén

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-26
- **Contexto**: Todo dependía de una única carpeta en un único proveedor, y de
  que alguien se acordara de comprobarla. Un repositorio dañado no se
  descubría hasta el día de restaurar.

## Decisión

- **`mirror`**: segunda copia en otro repositorio restic, con la misma frase,
  mediante `restic copy` (creado con los mismos parámetros de troceado para
  que deduplique). Incremental. La ruta se recuerda en `mirror_path`.
- `up` actualiza la segunda copia al cerrar **si el destino es accesible**. Si
  no, o si falla, avisa y cierra igual: la segunda copia nunca bloquea un cierre.
- Se rechaza un destino dentro del almacén.
- **`up` ejecuta `restic check` cada `verify_every_days`** (7). Si falla, avisa
  en grande pero **arranca**: el usuario necesita sus datos, y negarse a abrir
  no los protege.
- `verify` hace esa comprobación a demanda y además restaura un snapshot a la
  tmpfs y contrasta sus sha256 con el manifest.
- `status` muestra cuándo fue la última comprobación y la última copia.

## Consecuencias

- La segunda copia es otro repositorio: cambiar la frase con `passwd` no cambia
  la suya.
- La comprobación periódica alarga un arranque de cada siete.
- `restic check` sin `--read-data` verifica la estructura, no cada byte. La
  lectura real la hace cada restauración.
- Para volver a la segunda copia basta `init --vault <ruta de la copia>`.

## Validación

CI real, ambos motores: creación de la segunda copia y actualización
incremental. La comprobación periódica al arrancar y la actualización al
cerrar, con dobles.

## Referencias

`internal/cli/mirror.go`, `internal/cli/verify.go`, `internal/store/store.go`,
`docs/manual/03-copias-y-recuperacion.md`.
