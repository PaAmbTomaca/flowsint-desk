# Contribuir

Gracias por mirar el código. Lo esencial:

1. **Lee [`docs/manual/09-desarrollo.md`](docs/manual/09-desarrollo.md)**: cómo
   compilar, qué prueba cada cosa y qué debe seguir en verde según lo que toques.
2. **Lee [`AGENTS.md`](AGENTS.md)** aunque no seas un agente: recoge las reglas
   del proyecto y una lista de trampas ya medidas que ahorran horas.
3. Antes de enviar nada:

   ```bash
   gofmt -l .
   ```

   ```bash
   go vet ./... && go test ./...
   ```

## Reglas

- Un commit por cambio lógico, con mensaje que diga **qué** y **por qué**.
- Una decisión de diseño nueva va con su registro en `docs/decisions/`.
- No se afirma lo que no se ha probado: si algo no se pudo ejecutar, dilo.
- Ningún secreto en el repositorio, en argumentos ni en logs.
- Si cambias comandos u opciones, actualiza `docs/manual/04-comandos.md`, los
  dos README y `AGENTS.md`.
- El código de Flowsint no se modifica: todo se configura desde fuera.

## Qué no cambiar sin pensarlo dos veces

- **El cierre no destruye si no puede guardar.** No es un descuido.
- **El socket de los enrichers se filtra por lista de permitidos.**
- **`assets/compose.yml` y `podmanSpec` describen lo mismo.** Un test lo exige.
- **Todo desmontaje pasa por `teardown()`.**

Problemas de seguridad: [`SECURITY.md`](SECURITY.md).
