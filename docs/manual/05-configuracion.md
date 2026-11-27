# 5 · Configuración

Fichero: `~/.config/flowsint-desk/config.toml`, permisos `0600`.
**No contiene ningún secreto**, y `doctor` lo comprueba.

Lo crea `setup`/`init`. Puedes editarlo a mano; la CLI lo valida al cargar y se
niega a arrancar con valores peligrosos.

## Claves

| Clave | Por defecto | Qué es |
|-------|-------------|--------|
| `vault_path` | `~/pCloudDrive/flowsint-vault` | Carpeta del almacén cifrado |
| `port` | `5273` | Puerto de la interfaz en `127.0.0.1`. Entre 1024 y 65535 |
| `save_interval_min` | `10` | Minutos entre snapshots automáticos. Mínimo 1 |
| `keep_hourly` | `24` | Retención por horas |
| `keep_daily` | `30` | Retención por días |
| `keep_monthly` | `12` | Retención por meses. Las tres a 0 no se admite: borraría todo |
| `engine` | `"docker"` | Motor: `"docker"` o `"podman"`. Se cambia con `flowsint-desk engine`, no a mano |
| `docker_socket` | `"filtrado"` | Socket de Docker que ve Flowsint (ver abajo) |
| `flowsint_version` | `"latest"` | Etiqueta de las imágenes de Flowsint |
| `verify_every_days` | `7` | Cada cuántos días `up` comprueba la integridad del almacén. 0 = nunca |
| `mirror_path` | `""` | Segunda copia. La rellena `mirror --to` |

Las siguientes las escribe la CLI; no hace falta tocarlas:

| Clave | Qué es |
|-------|--------|
| `last_forget` | Última vez que se aplicó la retención |
| `last_verify` | Última comprobación de integridad correcta |
| `last_mirror` | Última actualización correcta de la segunda copia |

## `docker_socket`

Flowsint necesita un socket de Docker para los enrichers que lanzan
contenedores (subfinder, httpx, naabu, dnsx, asnmap, mapcidr).

| Valor | Qué ve Flowsint |
|-------|-----------------|
| `"filtrado"` | Un proxy de la CLI que solo deja descargar imágenes y lanzar contenedores sin montajes, sin privilegios, sin publicar puertos y sin entrar en la red de las bases de datos. Recomendado |
| `"directo"` | El socket real del motor. Un fallo en Flowsint equivaldría a acceso a todos tus ficheros. `up` lo avisa en cada arranque |
| `"no"` | Nada: esos seis enrichers no funcionan. El resto de Flowsint sí |

Si un enricher dejara de funcionar en `"filtrado"`, `up` imprime la petición
denegada (`⚠ socket Docker: DENEGADO …`). Eso es un dato para ampliar el filtro,
no un motivo para pasar a `"directo"`.

## `flowsint_version`

Con `"latest"` cada equipo usa la versión que descargó en su día; `up` avisa si
difieren del snapshot y `images sync` los iguala. Para no depender de eso, fija
una versión publicada por Flowsint:

```toml
flowsint_version = "1.2.10"
```

Después, `flowsint-desk images pull` en cada equipo.

## Ejemplo completo

```toml
vault_path = "/home/tu-usuario/pCloudDrive/flowsint-vault"
port = 5273
save_interval_min = 10
keep_hourly = 24
keep_daily = 30
keep_monthly = 12
engine = "podman"
docker_socket = "filtrado"
flowsint_version = "latest"
verify_every_days = 7
mirror_path = "/run/media/tu-usuario/DISCO/flowsint-mirror"
```

## Variables de entorno

Pensadas para pruebas y automatización; en el uso normal manda `config.toml`.

| Variable | Efecto |
|----------|--------|
| `FLOWSINT_DESK_CONFIG_DIR` | Usa otro directorio de configuración. Sirve para tener un segundo almacén sin tocar el principal |
| `FLOWSINT_DESK_ENGINE` | Fuerza el motor para este proceso |
| `FLOWSINT_DESK_NO_KEYRING` | Con cualquier valor, no lee ni escribe el llavero |

## Dónde guarda cosas la CLI

| Ruta | Qué | ¿Datos tuyos? |
|------|-----|---------------|
| `~/.config/flowsint-desk/config.toml` | Configuración | No |
| `~/.cache/flowsint-desk/images/` | Imágenes públicas en `.tar`, con su digest y su sha256 | No |
| `~/.local/bin/` | Binarios que instala `setup` (restic, script rootless…) | No |
| `/tmp/flowsint-desk-<boot_id>/` | Todo lo de la sesión, **en RAM** | Sí, en claro, solo mientras dura |
| Almacén (`vault_path`) | Repositorio restic | Sí, cifrados |
| Llavero del sistema | La frase de paso, si lo pediste | Sí |
