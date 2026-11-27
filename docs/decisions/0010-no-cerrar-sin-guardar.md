# ADR 0010 — Si no se puede guardar, no se cierra; y un arranque fallido se desmonta

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-26
- **Contexto**: Los datos de la sesión solo existen en RAM. Al cerrar, `up`
  hacía un snapshot final y después lo borraba todo. Si el snapshot fallaba
  (pCloud desmontado o sin sesión, bloqueo perdido, error de restic), imprimía
  un aviso y **borraba igualmente**: lo trabajado desde el último snapshot
  bueno se perdía sin remedio. Era el fallo más grave de la auditoría. En el
  otro extremo, un arranque que fallaba o se cancelaba dejaba contenedores y
  datos restaurados vivos en RAM.

## Decisión

1. **Un snapshot final fallido no destruye la sesión.** `up` sigue abierto,
   dice qué falló y hace cuánto fue el último snapshot bueno.
2. **Cerrar sin guardar es una decisión explícita**: repetir la orden de cierre
   dentro de 2 minutos (`closeGrace`). Pasado ese plazo, una nueva orden vuelve
   a intentar guardar.
3. `down` detecta el fallo mediante un marcador en la tmpfs y devuelve el mismo
   aviso en lugar de agotar su espera.
4. **Un arranque que falla tras tomar el bloqueo, o que se cancela con Ctrl+C,
   se desmonta entero** antes de salir.
5. **Todo desmontaje pasa por `teardown()`**, que tolera cualquier estado.
6. `down` sin sesión viva desmonta los restos de una sesión muerta y suelta su
   bloqueo.
7. Las señales se registran al entrar en `up` y `session.json` existe desde el
   principio con fase `arrancando`.

## Consecuencias

- Cerrar puede "no funcionar" a la primera. Es lo que se quiere: la alternativa
  es perder trabajo en silencio.
- Al **apagar el equipo** no hay segunda oportunidad: la RAM se pierde igual.
  Ahí cuenta el último snapshot automático (ver ADR 0003, actualización).
- Tras un arranque fallido no queda el log del motor en disco: el error incluye
  sus últimas líneas.

## Validación

CI real, ambos motores: almacén inaccesible al cerrar (`down` avisa, la sesión
sigue viva, al recuperarlo se guarda y cierra), `kill -9` + `down`, `kill -9` +
`up`. Cancelación con Ctrl+C durante el arranque: solo con dobles.

## Referencias

`internal/cli/up.go` (`teardown`, `closeGrace`), `internal/cli/down.go`,
`docs/manual/02-uso-diario.md`.
