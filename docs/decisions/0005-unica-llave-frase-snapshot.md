# ADR 0005 — Una sola llave: la frase de paso; secretos dentro del snapshot cifrado

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11 (fase 1–3 del encargo)
- **Contexto**: Flowsint necesita sus propios secretos (`AUTH_SECRET`,
  `MASTER_VAULT_KEY_V1`, contraseñas de Postgres y Neo4j). Si se pierden, se
  pierden los tokens y las claves de API cifradas del vault.

## Decisión

- En `init` se generan todos los secretos y viajan **dentro del snapshot
  cifrado** (`secrets.env` en el staging tmpfs → restic).
- La única llave del sistema es la **frase de paso** del repo restic. Con ella,
  cualquier equipo con la CLI recupera datos y secretos.
- La frase puede recordarse opcionalmente en el llavero Secret Service
  (`go-keyring`, colección **login**, atributo `username`); si no, `up` la
  pide por terminal sin eco. Nota: `secret-tool store` usa otra colección
  ("default"/`account") y **no interoperan** — guardar siempre con
  `passphrase.SaveKeyring`.
- Nunca en argv, nunca impresa, nunca en logs; en pruebas entra por stdin.

## Consecuencias

- **+** Un solo secreto que recordar/gestionar; el llavero es comodidad, no
  requisito.
- **−** Si se pierde la frase y no está en el llavero, el almacén es
  irrecuperable (propiedad intencionada de restic).

## Alternativas consideradas

- Contraseña por servicio / gestor de contraseñas externo para cada secreto:
  rechazado (múltiples llaves que gestionar; el snapshot ya cifra todo).

## Referencias

- `ENCARGO.md` §1 y §5; `internal/passphrase/`, `internal/secrets/`.
