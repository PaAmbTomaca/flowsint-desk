# 6 · Problemas frecuentes

Antes de nada:

```bash
flowsint-desk doctor
```

```bash
flowsint-desk status
```

## Al instalar

| Síntoma | Causa | Arreglo |
|---------|-------|---------|
| `flowsint-desk NUNCA se ejecuta como root` | Lo lanzaste con `sudo` | Ejecútalo como tu usuario. Solo los comandos que `doctor` te dicta llevan sudo |
| `✖ /tmp es tmpfs` | `/tmp` está en disco | Monta `/tmp` como tmpfs (línea en `/etc/fstab`: `tmpfs /tmp tmpfs rw,noatime,size=16G`) y reinicia |
| `✖ /etc/subuid tiene entrada para …` | Falta el rango de sub-UIDs | Los dos comandos de [Instalación § Paso 1](01-instalacion.md#paso-1--requisitos) |
| `✖ Podman puede crear su user namespace` | `newuidmap` sin permisos o subuid inválido | Reinstala el paquete que da `newuidmap` (`uidmap`/`shadow`/`shadow-utils`) y ejecuta `podman system migrate` |
| `✖ aardvark-dns` | Falta el DNS interno de Podman | `flowsint-desk doctor --packages` y ejecuta la línea |
| `la FIRMA del paquete … no es válida: no se instala` | El paquete descargado no pasa la verificación | No lo fuerces. Instala con `pacman` (línea de `doctor --packages`) |
| `⚠ swap fuera de RAM` | Tienes swap en disco | Ver [Seguridad](07-seguridad.md#lo-que-puede-romper-la-garantía-de-solo-en-ram) |
| `⚠ SELinux en modo enforcing` | Fedora/RHEL | No probado. Ejecuta `selftest`; si falla por permisos, `sudo ausearch -m avc -ts recent` |
| `pCloud no está montado` | El cliente no está abierto o sin sesión | Ábrelo, o indica la ruta: `setup --vault RUTA` |
| `distribución no reconocida` | Ni Debian, ni Arch, ni Fedora | Instala a mano lo que lista el mensaje |

## Al abrir (`up`)

| Síntoma | Causa | Arreglo |
|---------|-------|---------|
| `frase de paso INCORRECTA` | Eso | Vuelve a intentarlo. No se ha tocado nada |
| `la frase guardada en el llavero no abre este almacén; tecléala` | El llavero tiene la frase de otro almacén, o la cambiaste en el otro equipo | Tecléala. Para actualizar el llavero: `passwd` |
| `no hay terminal ni frase por stdin` | Lo lanzaste sin terminal y la frase no está en el llavero | Lánzalo desde una terminal, o guarda la frase en el llavero |
| `almacén bloqueado por otro equipo` | El otro equipo lo tiene abierto, o cerró hace menos de 15 min sin soltar | Ciérralo allí. Si está apagado: `unlock --force` |
| `hay un bloqueo CADUCADO de otro host` | El otro equipo murió sin cerrar | `flowsint-desk unlock` |
| `… (OTRO equipo con el mismo nombre de host)` | Dos equipos se llaman igual | Funciona igual: se distinguen por identificador de máquina |
| `ya hay una sesión viva` / `ya hay un up arrancando` | Hay otra sesión en este equipo | `flowsint-desk down`, o espera |
| `puerto 127.0.0.1:5273 ocupado` | Otro programa usa el puerto | Cambia `port` en `config.toml` |
| `el almacén … no tiene repositorio restic todavía` | Almacén sin inicializar, o pCloud aún no ha sincronizado | `init`; o espera a pCloud |
| `· detectados restos de una sesión anterior … se limpian` | La sesión anterior murió sin cerrar | Nada: se limpia solo y sigue |
| `dockerd rootless terminó prematuramente` (+ su log) | El motor no arranca | Lee las líneas del log que imprime; casi siempre subuid o un binario que falta. `doctor` |
| `la imagen en caché de … NO coincide con la que se descargó` | Un `.tar` de la caché está corrupto o cambiado | `flowsint-desk images update` |
| `arranque de las bases de datos: …` (+ logs de los contenedores) | Postgres, Redis o Neo4j no arrancan | Los logs salen justo encima, saneados. Poca RAM libre es lo más habitual |
| `VERIFICACIÓN FALLIDA: …` | El snapshot no cuadra con su manifest | No se arranca la interfaz. `up --snapshot ID` con uno anterior; y `verify` |
| `el snapshot … no trae secrets.env` | Ese snapshot no es de flowsint-desk o está incompleto | Otro snapshot |
| `⚠ este equipo NO ejecuta las mismas imágenes …` | Los dos equipos tienen versiones distintas de Flowsint | `down` y `images sync` |
| `✖ ¡ATENCIÓN! el almacén NO pasa la comprobación de integridad` | El repositorio tiene daños | Ver [Copias § verify falla](03-copias-y-recuperacion.md#verify-falla-o-up-avisa-de-que-el-almacén-no-pasa-la-comprobación) |
| Ctrl+C mientras arrancaba | Lo cancelaste | Se desmonta todo y sale con código 130. No hay nada que guardar |

Si `up` falla después de arrancar el motor, **lo desmonta todo antes de salir**:
no quedan contenedores ni datos descifrados en RAM.

## Durante la sesión

| Síntoma | Causa | Arreglo |
|---------|-------|---------|
| `⚠ el snapshot (auto) FALLÓ: …` | No se pudo guardar esta vez | Lee la causa. El mensaje dice hace cuánto fue el último bueno. Lo normal: pCloud |
| `… el bloqueo del almacén ya no es nuestro` | Otro equipo tomó el almacén, o pCloud no deja leer el fichero | Deja de guardarse para no pisar al otro. Comprueba pCloud y el otro equipo |
| `⚠ latido del bloqueo falló` | pCloud inaccesible un momento | Si se repite, mira pCloud |
| `⚠ socket Docker: DENEGADO …` | Flowsint pidió al motor algo fuera de lo permitido | Si fue un enricher legítimo, anota la línea (ver [Configuración § docker_socket](05-configuracion.md#docker_socket)). Si no sabes qué fue, es justo lo que el filtro debe parar |
| Un enricher con contenedor (subfinder, httpx…) no devuelve nada | Sin red a Docker Hub, o filtro | `flowsint-desk logs celery`, y busca líneas `DENEGADO` en la terminal de `up` |
| naabu (puertos) no encuentra nada | En contenedores sin privilegios no hay escaneo SYN | Limitación conocida, con ambos motores |

## Al cerrar

| Síntoma | Causa | Arreglo |
|---------|-------|---------|
| `✖ NO se cierra: la sesión SIGUE ABIERTA` | El snapshot final falló | Arregla la causa y repite; o repite antes de 2 min para cerrar sin guardar |
| `down`: `el snapshot final ha FALLADO y la sesión sigue abierta` | Lo mismo, visto desde `down` | Igual |
| `aviso: no se pudo actualizar la segunda copia` | El disco no está o está lleno | La sesión se cierra igual. `mirror` cuando lo tengas |
| `aviso: no se pudo borrar todo el directorio de ejecución` | Quedaron ficheros que solo el motor puede borrar | Están en RAM, no en disco. El siguiente `up` los elimina; un reinicio también |

## Copias

| Síntoma | Causa | Arreglo |
|---------|-------|---------|
| `forget`: repositorio bloqueado | Bloqueo interno de restic de un proceso muerto | `unlock --restic` |
| `mirror`: `no existe …: ¿está conectado y montado el disco?` | El disco de la segunda copia no está | Conéctalo |
| `mirror`: `no puede estar dentro del almacén` | Destino dentro del propio almacén | Elige otro soporte |
| `snapshots` va lento | pCloud bajo demanda | Normal con muchos snapshots; `forget` los reduce |

## Cuando nada cuadra

```bash
flowsint-desk down
```

```bash
flowsint-desk doctor
```

```bash
flowsint-desk verify
```

```bash
flowsint-desk selftest
```

Si `selftest` pasa, el equipo está bien y el problema está en el almacén o en
pCloud. Si falla, su salida dice en qué paso.

Reiniciar el equipo siempre deja el lado local limpio: todo lo de la sesión
está en RAM.
