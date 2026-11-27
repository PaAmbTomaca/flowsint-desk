# Cambios

Lo notable de cada versión, pensado para quien usa la herramienta. El detalle
de cada cambio y su porqué están en los mensajes de commit.

## Sin publicar (rama de trabajo, 2026-11-26)

Resultado de una auditoría completa del repositorio y de lo que vino después.
Informe: [`docs/security/2026-11-26-auditoria-repo.md`](docs/security/2026-11-26-auditoria-repo.md).

### Cambia el comportamiento — léelo antes de actualizar

- **Si el snapshot final falla, la sesión ya no se cierra.** Antes avisaba y lo
  borraba todo. Ahora sigue abierta; repetir Ctrl+C o `down` antes de 2 minutos
  cierra sin guardar.
- **Flowsint ya no ve el socket real de Docker**, sino uno filtrado
  (`docker_socket = "filtrado"`). Si un enricher dejara de funcionar, `up`
  imprime la petición denegada; `docker_socket = "directo"` vuelve a lo anterior.
- **`init` sobre un almacén con datos ya no crea nada**: conecta el equipo.
- **`forget` aplica la retención al conjunto completo.** Antes cada arranque
  del PC era un grupo aparte y casi no borraba nada: la primera ejecución puede
  eliminar muchos snapshots antiguos.
- **Los contenedores arrancan con menos permisos** (sin capacidades de más, sin
  volcados de memoria).
- `config.toml` se valida al cargar: valores peligrosos impiden arrancar.
- Un arranque que falla o se cancela se desmonta entero.

### Nuevo

- **Podman rootless como segundo motor**, elegible en `setup`.
  `flowsint-desk engine` lo muestra o lo cambia.
- **Asistente `setup`** de principio a fin: motor, requisitos (con el comando de
  tu distribución: Debian, Arch, Fedora), almacén (primer o segundo equipo),
  imágenes, prueba de humo y arranque.
- **`selftest`**: ciclo completo real con un almacén desechable, incluido un
  enricher de verdad y tres intentos de abuso contra el socket.
- **`verify`**: comprobar que la copia está entera sin abrir sesión.
- **`mirror`**: segunda copia del almacén en otro soporte; `up` la actualiza al
  cerrar.
- **`images`**: ver, descargar, actualizar e igualar imágenes entre equipos por
  digest. `flowsint_version` fija la versión de Flowsint.
- **Comprobación periódica** de la integridad del almacén al arrancar.
- `unlock --restic`, `doctor --packages`, `version`, `selftest --skip-enrichers`.
- `status` muestra la integridad y el estado de la segunda copia.
- `down` limpia los restos de una sesión muerta.
- `up` lanza el snapshot final cuando el equipo empieza a apagarse.
- `doctor` avisa de swap en disco, de SELinux, de la espera de apagado y de
  pCloud sin montar.

### Corregido

- Una copia de Postgres podía quedar irrestaurable si `pg_dump` emitía un aviso.
- Un backup lento se daba por fallido aunque el snapshot existía.
- `status`, `backup` y otros comandos podían pisar la frase de la sesión viva.
- Dos equipos con el mismo nombre se pisaban el bloqueo del almacén.
- Ctrl+C durante el arranque dejaba contenedores y datos descifrados en RAM; un
  `backup` en ese momento mataba la sesión.
- El directorio de la sesión en `/tmp` no se comprobaba (dueño, enlaces).
- La frase no se enmascaraba en los logs cuando venía del llavero; en cambio se
  tapaba la palabra `flowsint`.
- `doctor --fix` no podía instalar restic (verificaba contra un fichero que no
  existe) ni, en Arch, verificar la firma de los paquetes.
- `up --snapshot` con un ID corto provocaba un fallo interno.
- Un daemon que moría al arrancar hacía esperar 90 segundos sin explicación.
- `passwd` podía dejar la frase antigua en el llavero.
- Los comandos de consulta dejaban un directorio vacío en `/tmp`.
- `init` leía la ruta del almacén con un lector que se tragaba la frase cuando
  la entrada no era un terminal.

### Seguridad

- Socket filtrado por lista de permitidos; contenedores de enricher sin
  capacidades y con topes.
- Verificación sha256 de la caché de imágenes.
- Firma de los paquetes de Arch; `dockerd-rootless.sh` de la versión instalada.
- Acciones del CI fijadas por hash.

### Pruebas

- Tests unitarios en diez paquetes.
- CI con ciclo completo **real** en Docker rootless y Podman, y comprobación de
  requisitos en Arch, Debian 13 y Fedora.

### Sin validar

- Podman 5.4 (Debian 13) y 6.x (Arch, Fedora): solo se probó 5.7.
- pCloud real, llavero, apagado del equipo, SELinux.
- El flujo de publicación de versiones.

## 0.1.0 (2026-11-20)

Primera versión completa según `ENCARGO.md`: `doctor`, `setup`, `init`, `up`,
`down`, `status`, `backup`, `snapshots`, `unlock`, `forget`, `passwd`, `logs`.
Docker rootless, datos en tmpfs, snapshots cifrados con restic en pCloud,
bloqueo entre equipos, verificación por manifest.
