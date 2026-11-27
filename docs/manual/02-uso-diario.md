# 2 · Uso diario

## Abrir

```bash
flowsint-desk up
```

Qué hace, en orden, y qué verás:

| Mensaje | Qué ha pasado |
|---------|---------------|
| `✔ almacén desbloqueado` | La frase abre el repositorio. Si es incorrecta, se para aquí sin tocar nada |
| `✔ sin bloqueo de otro equipo · bloqueo tomado` | Nadie más tiene el almacén abierto |
| `✔ almacén íntegro` | (cada 7 días) `restic check` ha pasado |
| `✔ secretos recuperados del snapshot …` | Se han sacado del último snapshot |
| `✔ Docker rootless vivo` / `✔ Podman rootless listo` | El motor está en marcha, con todo en tmpfs |
| `✔ socket de Docker filtrado para los enrichers` | Flowsint no ve el socket real |
| `✔ imágenes listas` | Cargadas de la caché (verificando su sha256) o descargadas |
| `✔ postgres, redis y neo4j healthy` | Bases de datos arriba |
| `✔ restaurado snapshot … (manifest verificado)` | Tus datos están dentro y cuadran con lo que se guardó |
| `✔ Flowsint listo en http://127.0.0.1:5273` | Abre esa dirección en el navegador |

La terminal se queda ocupada: es la sesión. Mientras está abierta:

- guarda un snapshot **cada 10 minutos** (`save_interval_min`);
- renueva el bloqueo del almacén cada minuto;
- una vez al día aplica la retención (borra snapshots viejos).

Para abrir desde un snapshot anterior: `flowsint-desk up --snapshot ID` (ver los
ID con `flowsint-desk snapshots`).

## Guardar ya

Desde otra terminal:

```bash
flowsint-desk backup
```

## Cerrar

**Ctrl+C** en la terminal de la sesión, o desde otra:

```bash
flowsint-desk down
```

Hace un snapshot final, actualiza la segunda copia si la tienes configurada y
el disco está conectado, para los contenedores y borra todo lo que había en RAM.

### Si el snapshot final falla, no se cierra

Es deliberado. Si al cerrar no puede guardar (pCloud desmontado o sin sesión,
sin espacio, el bloqueo ya no es tuyo), **la sesión sigue abierta**:

```
✖ NO se cierra: la sesión SIGUE ABIERTA en http://127.0.0.1:5273 para no perder ese trabajo.
```

Cerrar en ese momento borraría de la RAM lo único que queda de lo trabajado
desde el último snapshot bueno. Opciones:

1. Arregla la causa (casi siempre: abrir pCloud) y repite Ctrl+C o `down`.
2. Si de verdad quieres cerrar **sin guardar**, repite Ctrl+C o `down` antes de
   2 minutos. Vale el último snapshot bueno; el mensaje te dice de hace cuánto es.

## Ver cómo va

```bash
flowsint-desk status
```

Muestra si hay sesión, quién tiene el bloqueo, cuándo se comprobó la integridad
del almacén por última vez, el estado de la segunda copia y el último snapshot.

```bash
flowsint-desk logs api
```

Logs de los contenedores de la sesión, con los secretos y la frase enmascarados
(`***`). Sin argumento, todos; con uno, los contenedores cuyo nombre lo contenga.

## Apagar el equipo

Si apagas con una sesión abierta, `up` se entera de que el apagado empieza y
lanza el snapshot final en ese momento. **Pero** el sistema solo espera 5
segundos de fábrica, que no dan. Para que quepa, una vez por equipo:

```bash
sudo mkdir -p /etc/systemd/logind.conf.d
```

```bash
printf '[Login]\nInhibitDelayMaxSec=120\n' | sudo tee /etc/systemd/logind.conf.d/flowsint-desk.conf
```

Y reinicia. `doctor` te dice cuánto espera ahora mismo.

Sin esto, al apagar cuenta el último snapshot automático (como mucho 10 minutos
atrás). Lo más seguro sigue siendo **cerrar antes de apagar**.

No hibernes (suspender a disco) con la sesión abierta: la RAM, con tus datos en
claro, se escribe en el disco.

## Trabajar con dos equipos

El almacén es el mismo; los equipos no pueden tenerlo abierto a la vez.

1. Cierra en el equipo A y **espera a que pCloud termine de sincronizar**.
2. Abre en el equipo B.

Si intentas abrir con el otro equipo todavía dentro:

```
almacén bloqueado por otro equipo: host=torre pid=… latido hace 40s
```

Si el otro equipo se quedó colgado o se apagó de golpe, su bloqueo caduca a los
15 minutos y `up` te sugerirá:

```bash
flowsint-desk unlock --force
```

Solo si estás seguro de que ese equipo no está trabajando. El bloqueo es una
protección para una persona con dos equipos, no para dos personas a la vez:
pCloud no garantiza que los dos vean el mismo fichero en el mismo instante.

Los equipos se distinguen por un identificador de máquina, no solo por el
nombre: dos equipos que se llamen igual no se pisan.

Si al restaurar ves:

```
⚠ este equipo NO ejecuta las mismas imágenes que crearon el snapshot
```

cierra y ejecuta `flowsint-desk images sync` para igualarlas.

## Si algo se quedó colgado

Sesión muerta a medias (se cerró la terminal de golpe, `kill -9`, cuelgue):

```bash
flowsint-desk down
```

Sin proceso `up` vivo, `down` desmonta lo que quedó en RAM (contenedores,
motor, datos descifrados) y suelta el bloqueo. Si no lo haces, el siguiente
`up` lo limpia solo antes de arrancar. Lo trabajado después del último snapshot
de esa sesión no es recuperable.

Más casos en [Problemas frecuentes](06-problemas.md).
