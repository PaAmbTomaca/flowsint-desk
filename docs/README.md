# Documentación de flowsint-desk

Estructura de decisiones de arquitectura (estilo ADR), en Markdown plano dentro
del árbol y versionada con el resto del repo.

## Organización

```
docs/
├── README.md            ← este índice
├── template-adr.md      ← plantilla para nuevas entradas
├── decisions/           ← EXISTENTE: decisiones ya tomadas y vigentes
│   ├── 0001-....md      (numeradas 000N, inmutables salvo correcciones menores)
│   └── ...
├── proposals/           ← PROPUESTAS: ideas pendientes de decidir
│   └── P001-....md      (numeradas P00N; al aprobarse pasan a decisions/)
├── research/            ← INVESTIGACIONES a fondo que sustentan propuestas
│   └── 2026-11-....md
├── security/            ← AUDITORÍAS de seguridad
│   ├── 2026-11-auditoria-seguridad.md
│   └── 2026-11-26-auditoria-repo.md
└── mapa-arquitectura.html  ← 🗺️ MAPA VISUAL de toda la arquitectura (ábrelo en el navegador)
```

## Reglas

- `decisions/` registra lo que **ya está decidido y en vigor** (refleja el
  comportamiento real del código). No se reabren salvo que aparezca un
  problema real; si aparece, se documenta y se para (ver ENCARGO.md).
- `proposals/` es para ideas que **aún no están aprobadas**. Una propuesta pasa
  a `decisions/` cuando se implementa y se valida, con número correlativo.
- Cada entrada usa `template-adr.md`: estado, contexto, decisión/propuesta,
  consecuencias, alternativas, referencias.
- Nada de secretos en estos ficheros: ni frases, ni claves, ni valores de
  configuración sensibles (nombres de variables sí, valores no).

## Índice

### Decisiones vigentes (`decisions/`)

| # | Tema |
|---|------|
| 0001 | Docker rootless con daemon efímero por sesión, data-root en tmpfs |
| 0002 | Persistencia: restic en pCloud (no git, no BD sobre unidad sincronizada) |
| 0003 | Datos en claro solo en tmpfs; endurecido de proceso y logs |
| 0004 | Exposición mínima: solo `127.0.0.1:5273`, Redis sin persistencia |
| 0005 | Una sola llave: la frase de paso; secretos dentro del snapshot cifrado |
| 0006 | Método de snapshot Neo4j (APOC cypher) + verificación por manifest |
| 0007 | Nunca ejecutar la CLI como root (guarda euid=0 → exit 1) |
| 0008 | Dos motores de contenedores: Docker rootless y Podman rootless, elegidos por equipo (en inglés) |

### Propuestas pendientes (`proposals/`)

| # | Tema | Estado |
|---|------|--------|
| P001 | Podman como backend opcional (daemonless) en vez de Docker rootless — research del 18/11 (XL, ~10–15 días) y **reanálisis del 26/11** (~4–6 días, como segundo backend) en `research/` | propuesta: siguiente paso tras validar la auditoría |

### Research (`research/`)

| Fichero | Tema |
|---|---|
| 2026-11-26-podman-deep-research-2.md | **Deep research con fuentes**: qué ganamos/perdemos con Podman, complejidad (5–11 días), riesgos operativos y de seguridad, y los 8 experimentos que lo zanjan. Límite: fuentes leídas solo como fragmentos de búsqueda; nada medido |
| 2026-11-26-podman-reanalisis.md | Podman, segunda vuelta tras la auditoría: gana en simplicidad/instalación/limpieza (no en seguridad), riesgos por medir (healthchecks sin systemd de usuario) y plan en fases de ~4–6 días como segundo backend |
| 2026-11-podman-deep-research.md | Podman rootless 2026: arquitectura, compose vía socket, socket para enrichers, matriz S/M/L/XL por componente |

### Seguridad (`security/`)

| Fichero | Tema |
|---|---|
| 2026-11-auditoria-seguridad.md | Auditoría severa 2026-11-20: 2 hallazgos altos (imágenes sin digest pin; socket rootless en api/celery), 3 medios (swap/zram, firmas en --fix, entropía de frase), riesgos aceptados y 8 controles verificados en verde |
| 2026-11-26-auditoria-repo.md | Auditoría completa del repo 2026-11-26: 20 fallos corregidos (1 crítico de pérdida de datos al cerrar, 7 altos), lo pendiente por orden de riesgo y qué se pudo y no se pudo verificar |
