# 3 · Copias y recuperación

## Qué es el almacén

Una carpeta (normalmente en pCloud) con un repositorio **restic**: cifrado,
deduplicado y versionado. Dentro no hay nada legible sin la frase de paso. Lo
único en claro es `flowsint-desk.lock.json`, el bloqueo entre equipos, que solo
dice qué equipo lo tiene y desde cuándo.

Cada copia es un **snapshot**: una foto completa del estado en ese momento.

## Qué lleva cada snapshot

| Fichero | Contenido |
|---------|-----------|
| `postgres.sql` | Usuarios, investigaciones, sketches, permisos, claves de API cifradas, chats |
| `neo4j.cypher` | El grafo completo |
| `secrets.env` | Los secretos internos de Flowsint (`AUTH_SECRET`, clave maestra del vault, contraseñas de las BD) |
| `manifest.json` | Versión de la CLI, equipo, fecha, digest de las imágenes, sha256 de los tres ficheros y recuentos de control (filas por tabla, nodos por etiqueta, relaciones por tipo) |

Como los secretos viajan dentro, **con la frase de paso basta** para recuperar
todo en cualquier equipo.

## Cuándo se guarda

- Automáticamente cada `save_interval_min` minutos (10).
- Al cerrar la sesión (snapshot `final`).
- Cuando lo pides: `flowsint-desk backup`.

```bash
flowsint-desk snapshots
```

lista fecha, equipo, tamaño y, para cada uno, tipo y recuentos.

## Al restaurar se comprueba

Cada `up` restaura el último snapshot y, antes de arrancar la interfaz:

1. comprueba el sha256 de cada fichero contra el manifest;
2. carga los volcados en bases de datos vacías;
3. vuelve a contar filas, nodos y relaciones y los compara con el manifest.

Si algo no cuadra, **no arranca la interfaz**, lo desmonta todo y te dice qué
falló. Prueba entonces un snapshot anterior: `flowsint-desk up --snapshot ID`.

## Retención

Una vez al día, y cuando ejecutes `flowsint-desk forget`, se borran snapshots
viejos conservando, sobre el conjunto completo (da igual desde qué equipo):

| Clave | Por defecto | Conserva |
|-------|-------------|----------|
| `keep_hourly` | 24 | uno por hora de las últimas 24 horas con snapshots |
| `keep_daily` | 30 | uno por día de los últimos 30 días con snapshots |
| `keep_monthly` | 12 | uno por mes de los últimos 12 meses con snapshots |

## Comprobar que la copia sirve

```bash
flowsint-desk verify
```

Sin abrir sesión ni levantar nada: ejecuta `restic check` sobre el almacén,
restaura el último snapshot a la RAM, comprueba que trae sus ficheros y que su
sha256 coincide, y lo borra. Con un ID comprueba ese snapshot.

`up` hace la comprobación del repositorio sola cada `verify_every_days` (7).
Si falla **no impide arrancar** —necesitas tus datos— pero lo avisa en grande.
`status` dice cuándo se comprobó por última vez.

`verify` no carga los volcados en las bases de datos. La prueba completa la
hace cada `up` real, y `selftest` con datos de mentira.

## Segunda copia

Todo en un único almacén es un único punto de fallo: si esa cuenta se pierde, se
corrompe o se borra por error, no hay más.

```bash
flowsint-desk mirror --to /run/media/tu-usuario/DISCO/flowsint-mirror
```

Crea un segundo repositorio restic, con la **misma frase**, y copia todos los
snapshots. Es incremental. La ruta se recuerda; desde entonces:

- `flowsint-desk mirror` la actualiza a mano;
- `up` la actualiza sola al cerrar, **si el disco está conectado** (si no, no
  pasa nada);
- `status` dice cuándo se actualizó por última vez.

El destino no puede estar dentro del almacén. Lo razonable es otro soporte: un
disco externo, otro servicio de nube montado, un NAS.

## Cambiar la frase de paso

```bash
flowsint-desk passwd
```

Añade la clave nueva y elimina la antigua. Si la tenías en el llavero, o se
actualiza o se borra (nunca queda la vieja). En tu **otro equipo** el llavero
tendrá la frase antigua: `up` lo detecta y te la pide por terminal.

La segunda copia es otro repositorio: sigue abriéndose con la frase que tenía
cuando se creó hasta que le cambies la clave con restic.

## Qué hacer si…

### Se me ha roto o he perdido un equipo

Nada que recuperar de ese equipo: no guardaba datos. En otro equipo,
`flowsint-desk setup` (o `init --vault RUTA`) y la frase.

### He borrado o estropeado algo en Flowsint

```bash
flowsint-desk snapshots
```

```bash
flowsint-desk down
```

```bash
flowsint-desk up --snapshot ID
```

Arranca con ese estado antiguo. Al cerrar, ese estado pasa a ser el último; los
snapshots posteriores siguen existiendo hasta que la retención los quite.

### La sesión murió sin cerrar

Se pierde lo trabajado desde su último snapshot (como mucho 10 minutos).
`flowsint-desk down` limpia; `up` restaura ese último snapshot.

### `forget` u otro comando dice que el repositorio está bloqueado

Un `kill -9` en mitad de un backup deja un bloqueo interno de restic:

```bash
flowsint-desk unlock --restic
```

Solo quita bloqueos de procesos que ya no existen.

### `verify` falla, o `up` avisa de que el almacén no pasa la comprobación

1. No sigas trabajando como si nada: los snapshots nuevos se apilan sobre un
   repositorio dañado.
2. Si tienes segunda copia, **no la actualices** (`mirror`): es la buena.
3. Mira si un snapshot anterior restaura: `flowsint-desk up --snapshot ID`.
4. Para volver a la segunda copia: `flowsint-desk init --vault <ruta de la copia>`.
   La copia pasa a ser tu almacén.

### He perdido el acceso a pCloud

Con segunda copia: `flowsint-desk init --vault <ruta de la copia>`.
Sin ella, y sin sesión abierta en ningún equipo, no hay datos.

### He olvidado la frase de paso

No hay recuperación. El cifrado es de restic y no existe puerta trasera. Si aún
la tienes en el llavero de algún equipo, `up` funciona ahí: entra y cámbiala
con `passwd`.
