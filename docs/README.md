# Documentación de flowsint-desk

| Quieres… | Ve a |
|----------|------|
| Instalar y usar la herramienta | [`manual/`](manual/README.md) |
| Saber por qué algo es como es | [`decisions/`](#decisiones-decisions) |
| Saber qué se ha auditado y qué falta | [`security/`](#seguridad-security) |
| Ver la arquitectura de un vistazo | [`mapa-arquitectura.html`](mapa-arquitectura.html) (ábrelo en el navegador) |
| Tocar el código | [`manual/09-desarrollo.md`](manual/09-desarrollo.md) y [`../AGENTS.md`](../AGENTS.md) |
| Ver qué ha cambiado | [`../CHANGELOG.md`](../CHANGELOG.md) |
| Avisar de un problema de seguridad | [`../SECURITY.md`](../SECURITY.md) |

## Organización

```
docs/
├── README.md               este índice
├── manual/                 manual de uso y mantenimiento (9 capítulos)
├── decisions/              decisiones tomadas y en vigor (ADR)
├── proposals/              propuestas; al aceptarse pasan a decisions/
├── research/               investigaciones que sustentan decisiones
├── security/               auditorías
├── mapa-arquitectura.html  mapa visual
└── template-adr.md         plantilla para nuevas decisiones
```

En la raíz del repositorio: `README.md` (y `README.en.md`), `CHANGELOG.md`,
`SECURITY.md`, `CONTRIBUTING.md`, `AGENTS.md` y `ENCARGO.md` (especificación
original, histórica).

## Reglas

- `decisions/` recoge lo que **está decidido y en vigor**. No se reescribe la
  historia: cuando una decisión evoluciona, se le añade una sección
  "Actualización" con fecha, o se escribe un ADR nuevo que la sustituye.
- `proposals/` es para lo que aún no está aprobado.
- Cada entrada usa `template-adr.md`.
- **No se afirma lo que no se ha probado.** Cada documento distingue lo
  validado con software real, lo probado con dobles y lo no ejecutado.
- Ningún secreto en estos ficheros (nombres de variables sí, valores no).
- Idioma: español. Excepciones: `README.en.md` y el ADR 0008, en inglés.

## Manual (`manual/`)

| # | Capítulo |
|---|----------|
| 1 | [Instalación](manual/01-instalacion.md) |
| 2 | [Uso diario](manual/02-uso-diario.md) |
| 3 | [Copias y recuperación](manual/03-copias-y-recuperacion.md) |
| 4 | [Comandos](manual/04-comandos.md) |
| 5 | [Configuración](manual/05-configuracion.md) |
| 6 | [Problemas frecuentes](manual/06-problemas.md) |
| 7 | [Seguridad](manual/07-seguridad.md) |
| 8 | [Arquitectura](manual/08-arquitectura.md) |
| 9 | [Desarrollo](manual/09-desarrollo.md) |

## Decisiones (`decisions/`)

| # | Tema | Notas |
|---|------|-------|
| [0001](decisions/0001-docker-rootless-daemon-efimero.md) | Docker rootless con daemon efímero por sesión, data-root en tmpfs | actualizada 26/11 |
| [0002](decisions/0002-restic-pcloud-sin-git.md) | Persistencia: restic cifrado en pCloud | actualizada 26/11 |
| [0003](decisions/0003-datos-en-claro-solo-tmpfs.md) | Datos en claro solo en tmpfs; proceso endurecido | actualizada 26/11 |
| [0004](decisions/0004-exposicion-minima-loopback.md) | Exposición mínima: solo `127.0.0.1:5273`; Redis sin persistencia | actualizada 26/11 |
| [0005](decisions/0005-unica-llave-frase-snapshot.md) | Una sola llave: la frase de paso; secretos dentro del snapshot | actualizada 26/11 |
| [0006](decisions/0006-snapshot-neo4j-apoc-manifest.md) | Snapshot de Neo4j con APOC y verificación por manifest | actualizada 26/11 |
| [0007](decisions/0007-nunca-como-root.md) | Nunca ejecutar la CLI como root | |
| [0008](decisions/0008-two-container-engines.md) | Dos motores: Docker rootless y Podman rootless | en inglés |
| [0009](decisions/0009-socket-filtrado-enrichers.md) | Flowsint no recibe el socket real del motor: proxy filtrado | |
| [0010](decisions/0010-no-cerrar-sin-guardar.md) | Si no se puede guardar, no se cierra; un arranque fallido se desmonta | |
| [0011](decisions/0011-imagenes-version-digest-cache.md) | Imágenes: versión fijable, digest anotado, caché verificada | |
| [0012](decisions/0012-dos-copias-y-verificacion.md) | Segunda copia opcional y comprobación periódica del almacén | |
| [0013](decisions/0013-pruebas-reales-en-ci.md) | Las pruebas que cuentan se ejecutan con software real en el CI | |

## Propuestas (`proposals/`)

| # | Tema | Estado |
|---|------|--------|
| [P001](proposals/P001-podman-backend-opcional.md) | Podman como backend opcional | aceptada e implementada → ADR 0008 |

## Investigaciones (`research/`)

| Fichero | Tema |
|---------|------|
| [2026-11-podman-deep-research.md](research/2026-11-podman-deep-research.md) | Primera investigación sobre Podman (18/11): estimaba el cambio en 10–15 días |
| [2026-11-26-podman-reanalisis.md](research/2026-11-26-podman-reanalisis.md) | Reanálisis sobre el código tras la auditoría. Optimista en coste; corregido por el siguiente |
| [2026-11-26-podman-deep-research-2.md](research/2026-11-26-podman-deep-research-2.md) | Investigación con fuentes: qué se gana y se pierde, riesgos y experimentos. Sus fuentes solo pudieron leerse como fragmentos de búsqueda |

Las tres son anteriores a la implementación. Lo que de verdad pasó al ejecutar
Podman está en el ADR 0008 ("Validation status") y en el ADR 0013.

## Seguridad (`security/`)

| Fichero | Tema |
|---------|------|
| [2026-11-auditoria-seguridad.md](security/2026-11-auditoria-seguridad.md) | Auditoría del 20/11: superficie de ataque. Sus hallazgos A1, A2, M2 y M3 están resueltos; ver el siguiente |
| [2026-11-26-auditoria-repo.md](security/2026-11-26-auditoria-repo.md) | Auditoría completa del 26/11: 20 fallos corregidos, cuatro tandas de mejoras, qué está validado en real y qué sigue pendiente |
