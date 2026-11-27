# Manual de flowsint-desk

Documentación de uso y de mantenimiento. Si acabas de llegar, lee en este orden.

| # | Documento | Para qué |
|---|-----------|----------|
| 1 | [Instalación](01-instalacion.md) | Dejar un equipo listo: requisitos por distribución y motor, asistente `setup`, primer y segundo equipo |
| 2 | [Uso diario](02-uso-diario.md) | Abrir, guardar, cerrar; qué pasa al apagar el PC; qué significan los mensajes |
| 3 | [Copias y recuperación](03-copias-y-recuperacion.md) | Qué se guarda, retención, comprobar la copia, segunda copia, y qué hacer en cada desastre |
| 4 | [Comandos](04-comandos.md) | Referencia de todos los comandos y opciones |
| 5 | [Configuración](05-configuracion.md) | Todas las claves de `config.toml` y las variables de entorno |
| 6 | [Problemas frecuentes](06-problemas.md) | Síntoma → causa → arreglo |
| 7 | [Seguridad](07-seguridad.md) | Qué garantiza la herramienta, contra qué protege y contra qué no |
| 8 | [Arquitectura](08-arquitectura.md) | Cómo está hecho por dentro: componentes, flujos, dónde vive cada dato |
| 9 | [Desarrollo](09-desarrollo.md) | Compilar, probar, CI, publicar una versión, reglas del repo |

Otros documentos:

- Decisiones de diseño y su porqué: [`../decisions/`](../decisions/) (ADR).
- Auditorías: [`../security/`](../security/).
- Investigaciones (Podman): [`../research/`](../research/).
- Mapa visual: [`../mapa-arquitectura.html`](../mapa-arquitectura.html).
- Reglas para agentes de código: [`../../AGENTS.md`](../../AGENTS.md).
- Especificación original del proyecto (histórica): [`../../ENCARGO.md`](../../ENCARGO.md).

## Qué es, en cuatro líneas

flowsint-desk levanta [Flowsint](https://github.com/reconurge/flowsint) (OSINT
de grafos) en tu PC dentro de contenedores sin privilegios, con **todos los
datos en RAM** mientras trabajas. Al cerrar no queda nada en el disco local. Lo
único que persiste son **copias cifradas** (restic) en una carpeta de pCloud,
que abres con una frase de paso desde cualquiera de tus equipos.

## Estado de validación

Lo que está probado con software real (Flowsint, restic y contenedores
auténticos, en el CI de GitHub) y lo que solo puede probarse en tu máquina está
detallado en [Desarrollo § Qué prueba cada cosa](09-desarrollo.md#qué-prueba-cada-cosa).
Resumen honesto: el ciclo completo y los casos de fallo pasan con Docker rootless
y con Podman 5.7; **no** se han probado Podman 5.4/6.x, pCloud real, el llavero,
el apagado del equipo ni SELinux.
