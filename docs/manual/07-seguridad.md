# 7 · Seguridad

## Qué promete la herramienta

1. **Los datos solo existen en claro en RAM**, mientras la sesión está abierta.
2. **Lo que persiste está cifrado** (restic) y sale del equipo solo así.
3. **Una sola llave**: la frase de paso.
4. **Flowsint no se modifica**: imágenes oficiales, configuradas desde fuera.
5. **Sin root**: ni la CLI ni los contenedores.

## Contra qué protege

| Amenaza | Cómo |
|---------|------|
| Alguien se lleva el equipo o el disco, apagado | En el disco no hay datos: configuración sin secretos e imágenes públicas |
| El proveedor de nube, o quien entre en tu cuenta, lee el almacén | Es un repositorio restic cifrado; sin la frase no hay nada legible |
| Alguien en tu red local intenta acceder a Flowsint | Solo se publica `127.0.0.1:5273`. Ni la API, ni Postgres, ni Neo4j, ni Redis salen del equipo |
| Un fallo en Flowsint o en un enricher intenta leer tus ficheros | No tiene el socket real del motor sino un proxy filtrado |
| Un contenedor comprometido intenta escalar | Sin capacidades de más, sin poder ganar privilegios, y fuera del contenedor es tu usuario sin privilegios, no root |
| Una imagen de la caché se corrompe o se cambia | Se verifica su sha256 antes de cargarla |
| Un binario descargado por `setup` viene manipulado | restic se verifica contra su `SHA256SUMS`; los paquetes de Arch, contra su firma |
| Un cierre con fallo te deja sin lo trabajado | Si no puede guardar, no cierra |
| Otro usuario del mismo PC prepara una trampa en `/tmp` | El directorio de la sesión debe ser un directorio real, tuyo y privado, o la CLI se niega |
| Un secreto acaba en un log o en la lista de procesos | Nunca van en argumentos; los logs que muestra la CLI se enmascaran |

## Contra qué NO protege

- **Alguien con acceso a tu sesión de usuario mientras trabajas.** Los datos
  están descifrados en RAM y cualquier programa tuyo puede leerlos.
- **Root en tu máquina.** Puede leer la RAM.
- **Otros usuarios locales en el puerto 5273.** `127.0.0.1` es de todo el
  equipo, y Flowsint permite registrarse. En un equipo compartido, importa.
- **Una frase de paso débil o apuntada en un sitio inseguro.**
- **Los metadatos del almacén.** Quien vea la carpeta sabe cuántos snapshots
  hay, su tamaño aproximado, cuándo se hicieron y qué equipo tiene el bloqueo.
- **Dos personas a la vez.** El bloqueo es para una persona con dos equipos.
- **Lo que hagan los enrichers en internet.** Consultan servicios externos:
  quien investigas puede dejar rastro de que lo investigas. Eso es de Flowsint
  y de cómo lo uses.

## Lo que puede romper la garantía de "solo en RAM"

Son del sistema, no de la CLI. `doctor` avisa de las dos primeras.

| Riesgo | Por qué | Qué hacer |
|--------|---------|-----------|
| **Swap en disco** | El sistema puede mandar memoria de `/tmp` al swap. Si está en disco sin cifrar, tus datos acaban ahí en claro | Usa solo zram, cifra el swap, o `sudo swapoff <dispositivo>` mientras trabajas |
| **Hibernar** | Suspender a disco escribe toda la RAM en el disco | Cierra la sesión antes |
| **`/tmp` en disco** | Entonces nada está en RAM | La CLI se niega a arrancar |
| **Volcados de memoria** | Un proceso que se cae puede dejar su memoria en disco | La CLI y los contenedores lo tienen desactivado (`core=0`); `selftest` lo comprueba |
| **El diario del sistema** | Podman manda por defecto los logs de los contenedores al diario, que está en disco | La CLI los fuerza a fichero dentro de la tmpfs |

## El socket filtrado

Seis enrichers de Flowsint lanzan contenedores hablando con el motor por un
socket. Quien tiene ese socket puede pedirle al motor **cualquier cosa**, por
ejemplo un contenedor con tu carpeta personal montada. Por eso Flowsint no
recibe el socket real, sino uno que atiende la propia CLI y que:

- solo deja pasar: consultar versión, descargar e inspeccionar imágenes, y
  crear, arrancar, esperar, leer la salida y borrar contenedores;
- rechaza cualquier contenedor que pida montar rutas, privilegios, capacidades,
  dispositivos, publicar puertos, compartir red o procesos con otros, o entrar
  en la red de las bases de datos;
- acepta solo una lista de opciones conocidas: una opción nueva del motor se
  deniega por defecto;
- solo deja operar sobre contenedores creados a través de él: los de Postgres y
  Neo4j no se pueden ni inspeccionar;
- crea cada contenedor sin capacidades, sin poder ganar privilegios y, si el
  sistema lo permite, con tope de memoria (2 GiB) y de procesos;
- registra cada denegación, sin el contenido.

`selftest` lo prueba de verdad en tu máquina: lanza un enricher y tres abusos.

Lo que el filtro **no** impide: que un contenedor de enricher tenga salida a
internet (la necesita) ni qué imagen se descarga.

## Imágenes

Las imágenes van por etiqueta. Para saber qué se ejecuta exactamente:

- al descargar se anota el digest de cada imagen; el manifest de cada snapshot
  lo guarda;
- al restaurar, `up` compara y avisa si este equipo ejecuta otras;
- `images sync` descarga por digest las del último snapshot;
- `flowsint_version` permite fijar la versión de Flowsint.

Riesgo que queda: la **primera** descarga confía en el registro en ese momento.

## La frase de paso y los secretos

- La frase se pide sin eco, o se lee del llavero del sistema si lo pediste.
- A restic le llega por un fichero `0600` en la tmpfs, nunca por argumento.
- Los comandos de consulta usan su propio fichero y lo borran al salir; no
  tocan el de la sesión.
- Los secretos internos de Flowsint se generan al crear el almacén y viajan
  dentro de cada snapshot. A los contenedores les llegan por ficheros de
  entorno en la tmpfs.
- Dentro de un contenedor los secretos son variables de entorno: así los espera
  Flowsint.

## Recomendaciones

1. Frase de 5–6 palabras al azar; en el llavero o en un gestor de contraseñas.
2. Swap solo en RAM o cifrado; no hibernar con la sesión abierta.
3. Segunda copia en otro soporte (`mirror`).
4. `docker_socket = "filtrado"` siempre.
5. Fijar `flowsint_version`, o al menos `images sync` al cambiar de equipo.
6. Ampliar la espera de apagado, y aun así cerrar antes de apagar.
7. `selftest` tras instalar y tras actualizar el sistema o las imágenes.
8. Cifrado de disco completo: no lo sustituye nada de lo anterior, y cubre lo
   que esta herramienta no puede (swap, hibernación, caché).

## Auditorías

- [`../security/2026-11-auditoria-seguridad.md`](../security/2026-11-auditoria-seguridad.md):
  superficie de ataque.
- [`../security/2026-11-26-auditoria-repo.md`](../security/2026-11-26-auditoria-repo.md):
  comportamiento ante fallos, correcciones y estado de validación.

Para avisar de un problema de seguridad: [`../../SECURITY.md`](../../SECURITY.md).
