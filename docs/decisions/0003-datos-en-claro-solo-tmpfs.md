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
