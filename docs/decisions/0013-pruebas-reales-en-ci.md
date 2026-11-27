# ADR 0013 — Las pruebas que cuentan se ejecutan con software real en el CI

- **Estado**: aceptada (en vigor)
- **Fecha**: 2026-11-26
- **Contexto**: El proyecto se validó a mano en la máquina del autor, con una
  batería de aceptación (`ENCARGO.md` §7) cuyos resultados quedaron en informes
  de sesión, fuera del repositorio. No había tests ni CI. Durante la auditoría
  muchos cambios solo pudieron comprobarse con dobles de `docker` y `restic`,
  y varias suposiciones resultaron falsas en cuanto se ejecutaron de verdad.

## Decisión

Tres niveles, todos en cada push:

1. **Tests unitarios** de toda la lógica que no necesita un motor.
2. **Comprobación por distribución** en contenedores reales de Arch, Debian 13
   y Fedora.
3. **Ciclo completo real** con Docker rootless y con Podman: el asistente
   `setup` en un equipo nuevo, `selftest` incluida, seis casos de fallo y, al
   final, la exigencia de que no quede nada en `/tmp` ni ningún proceso del
   motor vivo.

Y una regla: **no se afirma lo que no se ha ejecutado.** Los commits y los
documentos dicen qué se probó de verdad, qué con dobles y qué no.

`selftest` forma parte del producto, no solo del CI: el usuario puede repetir
la prueba en su máquina, que es donde están las cosas que el CI no tiene.

## Lo que el CI real destapó

- El llavero de paquetes de Arch va en ASCII y `gpgv` lo quiere binario.
- `doctor --fix` verificaba restic contra un `.sha256` que no existe.
- El overlay de Podman sigue montado al borrar ("resource busy").
- Podman deja vivo su proceso *pause*, compartido por usuario.
- Un `XDG_RUNTIME_DIR` propio rompe el DNS entre contenedores de Podman.
- Los comandos de consulta dejaban un directorio vacío en `/tmp`.

Ninguna de las seis aparecía en los tests con dobles.

## Consecuencias

- Cada push tarda 10–15 minutos en dar un veredicto completo.
- El CI depende de servicios externos (registros de imágenes, GitHub, mirrors).
- Quedan fuera pCloud, el llavero, el apagado del equipo, SELinux y las
  versiones de Podman distintas de la del runner.
- Se intentó ejecutar Podman 5.4 y 6.x dentro de contenedores de Debian y Arch
  y se retiró: `newuidmap` falla en ese entorno anidado.

## Referencias

`.github/workflows/ci.yml`, `ci/e2e.sh`, `ci/distro-check.sh`,
`internal/cli/selftest.go`, `docs/manual/09-desarrollo.md`.
