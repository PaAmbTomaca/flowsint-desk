# 4 · Comandos

Todos se ejecutan como tu usuario. **Como root la CLI se niega a arrancar.**

`flowsint-desk <comando> --help` muestra la ayuda de cada uno.

## Preparar el equipo

### `setup`

```
flowsint-desk setup [--yes] [--engine docker|podman] [--vault RUTA]
```

Asistente de principio a fin. Pasos: motor → requisitos → almacén → imágenes →
prueba de humo → arranque. Repetible.

| Opción | Efecto |
|--------|--------|
| `--yes` | No pregunta; acepta los valores por defecto y se detiene donde haga falta una persona |
| `--engine` | Fija el motor sin preguntar. En un equipo ya configurado con otro motor, se niega y remite a `engine` |
| `--vault` | Ruta del almacén, sin detectar pCloud |

### `doctor`

```
flowsint-desk doctor [--fix] [--packages]
```

Comprueba los requisitos del motor configurado. `✔` bien, `✖` bloquea, `⚠`
aviso. Cada fallo trae su `→ arreglar`.

| Opción | Efecto |
|--------|--------|
| `--fix` | Arregla lo que no necesita sudo |
| `--packages` | Imprime una sola línea que instala todos los paquetes del sistema necesarios en esta distribución y con este motor |

### `init`

```
flowsint-desk init [--vault RUTA]
```

Configura el almacén. Almacén vacío: lo crea (frase dos veces, secretos, primer
snapshot). Almacén con snapshots: conecta el equipo sin crear nada (frase una
vez).

### `engine`

```
flowsint-desk engine [docker|podman]
```

Sin argumento muestra el motor. Con argumento lo cambia: exige que no haya
sesión, pide confirmación, borra la caché de imágenes y te manda repetir
`setup`. No toca los datos.

### `images`

```
flowsint-desk images [pull|update|sync]
```

| Forma | Efecto |
|-------|--------|
| (nada) | Tabla: digest de cada imagen en este equipo y en el último snapshot. `=` igual, `≠` distinta, `?` no anotado |
| `pull` | Descarga a la caché las que falten |
| `update` | Vuelve a descargar todas las etiquetas: actualización deliberada de Flowsint |
| `sync` | Descarga **por digest** las del último snapshot: deja este equipo igual que el que guardó |

`pull`, `update` y `sync` necesitan que no haya sesión abierta.

### `selftest`

```
flowsint-desk selftest [--skip-enrichers]
```

Ciclo completo real con un almacén desechable en la tmpfs y una frase
aleatoria. No toca tus datos, tu configuración ni tu llavero. Comprueba, en
orden:

1. se crea el almacén cifrado;
2. Flowsint arranca;
3. la API responde: registra un usuario de prueba y entra con él;
4. los contenedores no pueden volcar memoria a disco;
5. un enricher de verdad (subfinder) lanza su contenedor a través del socket;
6. el socket filtrado deniega montar una carpeta, publicar un puerto y un
   contenedor privilegiado;
7. se guarda un snapshot;
8. al cerrar no queda nada en el directorio de ejecución;
9. el segundo arranque restaura y verifica el snapshot;
10. el usuario de prueba sigue pudiendo entrar (los datos y los secretos han
    viajado por el snapshot);
11. cierre limpio.

`--skip-enrichers` omite 5 y 6 (necesitan descargar una imagen de Docker Hub).

## Trabajar

### `up`

```
flowsint-desk up [--snapshot ID]
```

Abre la sesión y se queda en primer plano. Ver [Uso diario](02-uso-diario.md).

### `down`

Pide a la sesión viva que guarde y cierre; espera hasta 5 minutos. Si el
snapshot final falla, devuelve error y la sesión sigue abierta; repetirlo antes
de 2 minutos cierra sin guardar. Sin sesión viva, limpia los restos de una que
murió.

### `backup`

Snapshot inmediato de la sesión viva.

### `status`

Sesión (corriendo, arrancando o no), bloqueo, integridad, segunda copia y
último snapshot. Sin sesión abierta puede pedir la frase para leer el almacén.

### `logs`

```
flowsint-desk logs [--tail N] [filtro]
```

Logs de los contenedores de la sesión, con secretos y frase enmascarados. 200
líneas por contenedor salvo `--tail`.

## Copias

### `snapshots`

Lista de snapshots con fecha, equipo, tamaño, tipo y recuentos.

### `verify`

```
flowsint-desk verify [ID]
```

Integridad del repositorio y de un snapshot (por defecto el último), sin abrir
sesión.

### `mirror`

```
flowsint-desk mirror [--to RUTA]
```

Segunda copia. `--to` fija y recuerda el destino.

### `forget`

Aplica la retención y libera espacio.

### `passwd`

Cambia la frase de paso.

### `unlock`

```
flowsint-desk unlock [--force] [--restic]
```

| Forma | Efecto |
|-------|--------|
| (nada) | Quita un bloqueo **caducado** (latido de hace más de 15 min) de otro equipo |
| `--force` | Lo quita aunque sea reciente, pidiendo que escribas `SI` |
| `--restic` | Quita los bloqueos internos de restic que dejó un proceso muerto |

Un bloqueo de este mismo equipo no se quita con `unlock`: lo limpia `down` o el
siguiente `up`.

## Otros

### `version`

Versión de la CLI y del compilador.

### `completion`

```
flowsint-desk completion fish
```

Genera el autocompletado para tu shell (`bash`, `zsh`, `fish`).

## Códigos de salida

| Código | Significado |
|--------|-------------|
| 0 | Bien |
| 1 | Error (el mensaje dice cuál) |
| 130 | `up` cancelado con Ctrl+C durante el arranque |
