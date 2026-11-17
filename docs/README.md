# Documentación de flowsint-desk

Estructura de decisiones de arquitectura (estilo ADR). **Sin git**: estos
ficheros son Markdown plano dentro del árbol; el control de versiones lo lleva
quien coordina el proyecto, no este directorio.

## Organización

```
docs/
├── README.md            ← este índice
├── template-adr.md      ← plantilla para nuevas entradas
├── decisions/           ← EXISTENTE: decisiones ya tomadas y vigentes
│   ├── 0001-....md      (numeradas 000N, inmutables salvo correcciones menores)
│   └── ...
└── proposals/           ← PROPUESTAS: ideas pendientes de decidir
    └── P001-....md      (numeradas P00N; al aprobarse pasan a decisions/)
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

### Propuestas pendientes (`proposals/`)

| # | Tema | Estado |
|---|------|--------|
| P001 | Podman como backend opcional (daemonless) en vez de Docker rootless | propuesta, objetivo v2 |
