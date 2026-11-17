# ADR 0007 — Nunca ejecutar flowsint-desk como root (guarda euid=0)

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-17 (orden del operador)
- **Contexto**: Si la CLI corriera como root podría tocar el Docker rootful del
  sistema (prohibido), los datos en tmpfs pasarían a ser de root y los permisos
  dejarían de proteger nada. Además, "funciona con sudo" invita a costumbre
  peligrosa.

## Decisión

- `PersistentPreRun` de la CLI comprueba `os.Geteuid() == 0` y hace `exit(1)`
  con un mensaje claro **antes** de umask/rlimit o cualquier otra cosa.
- Regla dura añadida a `AGENTS.md` y `ENCARGO.md`: prohibido `sudo flowsint-desk ...`.

## Consecuencias

- **+** Imposible usar la CLI para operar el Docker rootful del sistema o
  crear ficheros de root en los paths de datos; la superficie queda acotada al
  usuario.
- **−** Ninguna: el flujo normal siempre es como usuario (Docker rootless).

## Alternativas consideradas

- Confiar en que el operador no use sudo: descartado (la regla existe por
  incidentes; mejor un guardarraíl en el propio binario).

## Referencias

- `internal/cli/root.go` (guarda en `PersistentPreRun`); `AGENTS.md`; `ENCARGO.md`.
