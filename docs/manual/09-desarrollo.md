# 9 · Desarrollo

## Compilar y probar

```bash
go build -o dist/flowsint-desk .
```

```bash
go vet ./...
```

```bash
go test ./...
```

Los tests unitarios no necesitan Docker, Podman, restic ni red. El del llavero
se salta solo si no hay Secret Service.

`dist/` no va al repositorio.

## Qué prueba cada cosa

| Nivel | Qué cubre | Dónde |
|-------|-----------|-------|
| **Tests unitarios** | Bloqueo entre equipos, validación de config, parseo de la salida de restic, comparación con el manifest, enmascarado de secretos, directorio de ejecución, proxy del socket (contra un daemon simulado con HTTP real), distro y paquetes, verificación de la caché, **compose ≡ podmanSpec** | `internal/*/*_test.go` |
| **Por distribución** | En contenedores reales de Arch, Debian 13 y Fedora: `doctor --fix` en sistema pelado (restic contra `SHA256SUMS`; en Arch, firma de paquetes), instalar la línea de `doctor --packages` y exigir los requisitos en verde | `ci/distro-check.sh`, job `distro` |
| **Ciclo completo real** | En un runner con Docker rootless y con Podman, con Flowsint y restic auténticos: el asistente `setup` entero en un equipo nuevo, incluida `selftest`; después seis casos de fallo; al final, nada en `/tmp` y ningún proceso del motor vivo | `ci/e2e.sh`, job `e2e` |
| **En tu máquina** | Lo que el CI no puede: pCloud real, llavero, apagado del equipo, SELinux, tu versión de Podman | `flowsint-desk selftest` |

Los seis casos de fallo de `ci/e2e.sh`:

1. frase incorrecta → error claro, nada arrancado, sin bloqueo;
2. `kill -9` a la sesión → `down` no deja directorio, ni procesos, ni bloqueo;
3. `kill -9` → el siguiente `up` limpia solo y restaura;
4. almacén inaccesible al cerrar → `down` avisa, la sesión sigue viva y, al
   recuperarlo, se guarda el snapshot final;
5. segunda copia, creada y luego incremental;
6. segundo equipo sobre el mismo almacén → se conecta sin crear snapshots.

**No cubierto por ninguna prueba automática**: Podman 5.4 y 6.x (el runner
trae 5.7; ejecutarlo dentro de un contenedor de Debian o Arch falla en
`newuidmap` por el entorno anidado), SELinux en modo enforcing, y el flujo de
publicación.

Durante la auditoría se usó además un simulacro local con dobles de `docker` y
`restic`. Sirvió cuando no había forma de ejecutar el producto de verdad; **no
está en el repositorio** y el job `e2e` lo sustituye con ventaja.

## Si cambias…

| Cambias | Tiene que seguir en verde | Y además |
|---------|---------------------------|----------|
| Arranque, cierre, señales, `teardown` | `e2e` con los dos motores | No hay otro camino de desmontaje que `teardown()` |
| `assets/compose.yml` | `TestPodmanSpecIgualQueElCompose` | Cambia también `podmanSpec` en `internal/dock/stack.go` |
| El proxy del socket | Sus tests y los pasos 5–6 de `selftest` | Las opciones se **permiten** una a una (`hostConfigAllowed`), nunca se vuelve a una lista de prohibidos |
| Snapshots, restic | Tests de `store` y `snap`, y `e2e` | La salida `--json` de restic son líneas JSON; los avisos van por stderr |
| Mensajes de `doctor` | `ci/distro-check.sh` busca textos concretos | Actualiza el script |
| Paquetes por distribución | Job `distro` | `installHint`/`pkgs` en `internal/cli/distro.go` |
| Comandos u opciones | — | `docs/manual/04-comandos.md`, README (es y en) y `AGENTS.md` |

## CI

`.github/workflows/ci.yml`, en cada push y pull request:

| Job | Qué hace |
|-----|----------|
| `go` | gofmt, build, vet, test; compila el binario estático que usan los demás |
| `distro` | Matriz Arch / Debian 13 / Fedora |
| `e2e` | Matriz docker / podman, ciclo completo real (unos 10–15 minutos) |

Las acciones están fijadas por hash de commit, no por etiqueta. El workflow
solo tiene permiso de lectura.

## Publicar una versión

`.github/workflows/release.yml` se dispara al empujar una etiqueta `vX.Y.Z`:
pasa vet y tests, compila binarios estáticos reproducibles para `amd64` y
`arm64` con la versión grabada, genera `SHA256SUMS` y crea la release.

```bash
git tag v0.1.0
```

```bash
git push origin v0.1.0
```

> Este flujo **no se ha ejecutado nunca**. La primera vez, revisa el resultado.

Las compilaciones locales se identifican como `0.1.0-dev`
(`flowsint-desk version`).

## Reglas del repositorio

- **Commits granulares**: uno por cambio lógico, con mensaje que diga **qué** y
  **por qué**. Formato `tipo(ámbito): resumen` (`fix`, `feat`, `docs`, `test`,
  `ci`, `refactor`, `perf`, `style`).
- Si un cambio toma una decisión de diseño, va con su registro en
  `docs/decisions/` (plantilla: `docs/template-adr.md`).
- **No se afirma lo que no se ha probado.** Si algo no se pudo ejecutar, se
  dice en el commit y en el documento.
- Nada de secretos en el repositorio, en argumentos de programas ni en logs.
- La documentación de usuario está en español; el README también en inglés.

Las reglas operativas completas, y las trampas ya medidas, están en
[`../../AGENTS.md`](../../AGENTS.md).

## Dependencias

| Módulo | Para qué |
|--------|----------|
| `github.com/spf13/cobra` | Comandos |
| `github.com/BurntSushi/toml` | `config.toml` |
| `github.com/zalando/go-keyring` | Llavero del sistema |
| `github.com/godbus/dbus/v5` | Aviso de apagado de logind |
| `golang.org/x/sys`, `golang.org/x/term` | statfs, límites, lectura sin eco |

En tiempo de ejecución, como programas externos: `restic` y `docker` (con
compose) o `podman`.
