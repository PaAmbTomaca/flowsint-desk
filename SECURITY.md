# Seguridad

## Avisar de una vulnerabilidad

Si encuentras un problema de seguridad en flowsint-desk, **no abras una issue
pública con los detalles**. Usa el aviso privado de GitHub
(*Security → Report a vulnerability* en
<https://github.com/PaAmbTomaca/flowsint-desk>) si está activado en el
repositorio; si no lo está, abre una issue **sin detalles** pidiendo un canal
privado.

Incluye, si puedes: versión (`flowsint-desk version`), distribución, motor
(`flowsint-desk engine`), pasos para reproducirlo y qué consigue un atacante.
No incluyas tu frase de paso, secretos ni datos de tus investigaciones.

## Qué entra

- Datos del usuario que acaben en claro fuera de la RAM por culpa de la CLI.
- Secretos o la frase de paso en argumentos de programas, logs o ficheros en
  disco.
- Formas de saltarse el socket filtrado de los enrichers.
- Servicios accesibles fuera de `127.0.0.1`.
- Pérdida de datos por un cierre, un arranque fallido o una restauración.
- Fallos en la verificación de lo que descarga `setup` o de la caché de
  imágenes.

## Qué no entra

- Vulnerabilidades de Flowsint: van a <https://github.com/reconurge/flowsint>.
- Vulnerabilidades de restic, Docker o Podman: a sus proyectos.
- Un atacante que ya controla tu sesión de usuario o es root en tu máquina.
- Los límites documentados en
  [`docs/manual/07-seguridad.md`](docs/manual/07-seguridad.md).

## Modelo de seguridad y auditorías

- Qué garantiza y qué no: [`docs/manual/07-seguridad.md`](docs/manual/07-seguridad.md).
- Auditorías: [`docs/security/`](docs/security/).
