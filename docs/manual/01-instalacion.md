# 1 · Instalación

## En una frase

```bash
flowsint-desk setup
```

El asistente hace todo lo que puede sin `sudo`, te dicta los comandos que sí lo
necesitan (y espera a que los ejecutes), conecta el almacén, descarga las
imágenes y prueba que todo funciona. Se puede repetir las veces que haga falta:
lo que ya está bien, se salta.

La CLI **nunca ejecuta `sudo`** ni se deja ejecutar como root.

## Antes de empezar

| Necesitas | Por qué |
|-----------|---------|
| Linux con `/tmp` montado como **tmpfs** y ≥ 8 GiB libres ahí | Todo el estado de la sesión vive en RAM |
| ≥ 4 GB de RAM libre | Postgres, Neo4j, Redis y Flowsint a la vez |
| Entradas en `/etc/subuid` y `/etc/subgid` para tu usuario | Contenedores sin privilegios (user namespaces) |
| pCloud Drive montado y con sesión | Ahí vive el almacén cifrado. Vale cualquier carpeta: `--vault RUTA` |
| ≥ 6 GiB libres en tu home | Caché de imágenes (públicas, sin datos tuyos) |
| Red la primera vez | Descargar imágenes y, si faltan, restic y el script rootless |

`flowsint-desk doctor` comprueba todo esto y para cada cosa en rojo te dice
cómo arreglarla. `doctor --fix` arregla lo que no necesita sudo.

## Paso 0 · Elegir motor de contenedores

La primera vez, `setup` te pregunta. Los dos son sin privilegios y dan las
mismas garantías.

| | Docker rootless | Podman rootless |
|---|---|---|
| Paquetes | docker, compose, rootlesskit, slirp4netns, fuse-overlayfs + un script | podman, aardvark-dns, passt |
| Daemon | uno por sesión | ninguno |
| Limpieza al cerrar | contenedor auxiliar | directa, funciona aunque todo haya muerto |
| Madurez en este proyecto | con el que se construyó | añadido después; probado en CI con Podman 5.7 |

Regla práctica: si ya usas Docker para otras cosas en ese equipo, cualquiera de
los dos vale y no se pisan. Si el equipo está limpio, Podman son menos piezas.

> **Cambiar de motor más adelante no es un ajuste en caliente.** Hay que cerrar
> la sesión y ejecutar `flowsint-desk engine <docker|podman>`, que borra la caché
> de imágenes y te manda repetir `setup`. Tus datos no se tocan: viven cifrados
> en el almacén y no dependen del motor. No hace falta reinstalar la CLI.

Para fijarlo sin que pregunte: `flowsint-desk setup --engine podman`.

## Paso 1 · Requisitos

`setup` ejecuta los chequeos de `doctor` e instala solo lo que puede:

- **restic**: binario oficial a `~/.local/bin`, verificado contra el
  `SHA256SUMS` de la release.
- **Docker rootless, en Arch/CachyOS**: `rootlesskit`, `slirp4netns` y
  `fuse-overlayfs` desde el paquete oficial, **verificando su firma** con el
  llavero de paquetes del sistema. Sin firma válida no se instala.
- **`dockerd-rootless.sh`**: el del paquete de Docker si existe; si no, el de
  moby en la etiqueta de la misma versión que tu Docker.

Para el resto te da **una línea** con los paquetes de tu distribución. También
puedes pedirla tú:

```bash
flowsint-desk doctor --packages
```

Ejemplos de lo que devuelve (comprobado instalando de verdad en contenedores de
cada distribución):

| Distribución | Motor Docker | Motor Podman |
|---|---|---|
| Debian 13 | `sudo apt install docker.io docker-compose uidmap rootlesskit slirp4netns fuse-overlayfs restic` | `sudo apt install podman aardvark-dns passt uidmap restic` |
| Arch / CachyOS | `sudo pacman -S docker docker-compose shadow rootlesskit slirp4netns fuse-overlayfs restic` | `sudo pacman -S podman aardvark-dns passt shadow restic` |
| Fedora | `sudo dnf install moby-engine docker-compose shadow-utils rootlesskit slirp4netns fuse-overlayfs restic` | `sudo dnf install podman aardvark-dns passt shadow-utils restic` |

> La línea de Podman solo se ha ejecutado de verdad en Ubuntu (el runner del
> CI). Las de Docker, en las tres distribuciones de la tabla.

**No instales `podman-docker`**: suplanta el comando `docker` y entra en
conflicto con un Docker de verdad.

Si falta la entrada de subuid/subgid:

```bash
sudo sh -c "echo '$(whoami):100000:65536' >> /etc/subuid"
```

```bash
sudo sh -c "echo '$(whoami):100000:65536' >> /etc/subgid"
```

## Paso 2 · Almacén: primer equipo o segundo

`setup` localiza pCloud Drive y propone `<montaje>/flowsint-vault`. Según lo que
encuentre ahí:

- **Almacén vacío → primer equipo.** Te pide la frase de paso dos veces, crea
  el repositorio cifrado, genera los secretos internos de Flowsint y guarda un
  primer snapshot.
- **Almacén con datos → segundo equipo.** Te pide la frase **una** vez, la
  comprueba y conecta el equipo. **No crea ni modifica nada** en el almacén.

La frase de paso es la **única llave** de todo. Mínimo 8 caracteres; avisa por
debajo de 16. Usa 5 o 6 palabras al azar. Si la pierdes, no hay recuperación.

Te ofrecerá recordarla en el llavero del sistema. Si dices que sí, no se deja
ninguna copia en RAM; si dices que no, se guarda en la tmpfs solo hasta el
primer `up`.

Si no usas pCloud Drive o no lo detecta: `flowsint-desk setup --vault /ruta`.

## Paso 3 · Imágenes

Descarga las seis imágenes a `~/.cache/flowsint-desk/images` para que el primer
arranque no tarde. En un **segundo equipo** descarga primero las mismas que usó
el equipo que guardó por última vez (por digest), para que los dos ejecuten
exactamente la misma versión de Flowsint.

## Paso 4 · Prueba de humo

Opcional, y recomendable en un equipo nuevo. `selftest` hace un ciclo completo
real con un almacén desechable y una frase aleatoria: no toca tus datos, tu
configuración ni tu llavero. Detalle en [Comandos § selftest](04-comandos.md#selftest).

## Paso 5 · Listo

```bash
flowsint-desk up
```

Sigue en [Uso diario](02-uso-diario.md).

## Sin asistente

Si prefieres ir paso a paso:

```bash
flowsint-desk doctor
```

```bash
flowsint-desk init --vault ~/pCloudDrive/flowsint-vault
```

```bash
flowsint-desk images pull
```

```bash
flowsint-desk selftest
```

## Recomendado después de instalar

- **Amplía la espera de apagado**, o el snapshot final al apagar el PC no cabe
  (ver [Uso diario § Apagar el equipo](02-uso-diario.md#apagar-el-equipo)).
- **Configura una segunda copia**: `flowsint-desk mirror --to /ruta/en/otro/disco`
  (ver [Copias](03-copias-y-recuperacion.md#segunda-copia)).
- **Revisa el aviso de swap** de `doctor` si tu swap está en disco
  (ver [Seguridad](07-seguridad.md#lo-que-puede-romper-la-garantía-de-solo-en-ram)).
