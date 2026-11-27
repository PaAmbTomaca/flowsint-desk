# ADR 0003 — Datos en claro solo en tmpfs; proceso endurecido

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11
- **Contexto**: Garantía nº1 del producto: mientras la app está abierta los
  datos solo existen en claro en RAM; al cerrar (o al apagar el PC) no queda
  nada en el disco local.

## Decisión

- Antes de escribir cualquier dato en claro se comprueba con `statfs` que el
  path está en tmpfs (`TMPFS_MAGIC 0x01021994`); si no lo está, se niega
  (`internal/run`).
- El proceso fija `umask 077` y `RLIMIT_CORE=0` al arrancar (`root.go`).
- Logs de los contenedores con driver que no escriba fuera de tmpfs.
- Persistente en disco local solo: `~/.config/flowsint-desk/config.toml` (0600,
  **sin secretos**) y la caché de imágenes públicas en `~/.cache/flowsint-desk/`.
- Tras `kill -9` o corte de luz, el siguiente `up` detecta los restos del mismo
  `boot_id`, los limpia y restaura el último snapshot.

## Consecuencias

- **+** Al cerrar no queda nada en claro en el disco local; los cores no
  existen; los permisos de cualquier fichero creado son de solo el dueño.
- **−** Si `/tmp` no es tmpfs, la CLI se niega a arrancar (comportamiento
  intencionado; `doctor` lo detecta y da el fix).

## Alternativas consideradas

- Cifrado en disco local (LUKS/tmpfs cifrada gestionada a mano): descartado;
  tmpfs + restic ya cumplen la garantía con menos piezas que mantener.

## Referencias

- `ENCARGO.md` §5 requisitos de seguridad; `internal/run/`, `internal/cli/root.go`.

## Actualización 2026-11-26

La garantía se ha reforzado y se han documentado sus límites.

- **El directorio de ejecución se valida**: debe ser un directorio real (no un
  enlace), propiedad del usuario, sin permisos para otros, y estar él mismo en
  tmpfs. `/tmp` es de todos y el nombre es predecible.
- **Volcados de memoria**: además de la CLI, todos los contenedores llevan
  `core=0`. `selftest` lo comprueba.
- **Swap**: el sistema puede paginar tmpfs a swap. `doctor` avisa de cualquier
  swap que no sea zram. La CLI no puede impedirlo.
- **Hibernación**: escribe la RAM en disco. Documentado; no evitable desde aquí.
- **Podman y el diario**: por defecto mandaría los logs de los contenedores a
  journald (disco). Se fuerzan a fichero dentro de la tmpfs.
- **Comandos de consulta**: usan un fichero de frase propio que se borra al
  salir, y no dejan el directorio de ejecución creado.
- **`init`** no deja la frase en la tmpfs si se guardó en el llavero.
- **Apagado del equipo**: `up` escucha `PrepareForShutdown` de logind y lanza el
  snapshot final al empezar el apagado. La ventana la fija `InhibitDelayMaxSec`
  (5 s de fábrica, insuficiente): `doctor` lo comprueba y da los comandos.
- **Si el snapshot final falla, no se borra nada**: [ADR 0010](0010-no-cerrar-sin-guardar.md).
- Los avisos y logs enmascaran la frase venga de donde venga, y ya no tapan
  valores que no son secretos (`flowsint`).
