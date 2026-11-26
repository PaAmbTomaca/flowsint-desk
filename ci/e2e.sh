#!/bin/sh
# Ciclo completo REAL en un runner de CI: el asistente `setup` de principio a
# fin (requisitos, almacén nuevo, imágenes, prueba de humo) con contenedores,
# restic y Flowsint de verdad. Se ejecuta como usuario normal (la CLI se niega
# como root); usa sudo solo para preparar la máquina, igual que haría una
# persona siguiendo las instrucciones de `doctor`.
#   uso: ci/e2e.sh <binario> <motor: docker|podman>
set -eu
BIN="$1"; ENGINE="${2:-docker}"
chmod +x "$BIN"

echo "== preparar la máquina (lo que en un escritorio ya viene hecho)"
# /tmp en RAM, como exige la CLI
sudo mount -t tmpfs -o size=12g,mode=1777 tmpfs /tmp
# Ubuntu restringe los user namespaces sin privilegios con AppArmor
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0 2>/dev/null || true
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
sudo mkdir -p "$XDG_RUNTIME_DIR" && sudo chown "$(id -u):$(id -g)" "$XDG_RUNTIME_DIR"
grep -q "^$(id -un):" /etc/subuid || echo "$(id -un):100000:65536" | sudo tee -a /etc/subuid
grep -q "^$(id -un):" /etc/subgid || echo "$(id -un):100000:65536" | sudo tee -a /etc/subgid

echo "== paquetes del sistema que pide doctor para $ENGINE"
CMD=$(FLOWSINT_DESK_ENGINE="$ENGINE" "$BIN" doctor --packages)
echo "   $CMD"
sudo apt-get update -qq
# en el runner ya hay un Docker oficial (docker-ce): no se mezcla con docker.io
PKGS=$(echo "${CMD#sudo apt install }" | tr ' ' '\n' | grep -v -x -e docker.io -e docker-compose | tr '\n' ' ')
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $PKGS

echo "== asistente completo (primer equipo): requisitos, almacén, imágenes y prueba de humo"
FRASE="frase-de-prueba-del-ci-1234"
set +e
printf '%s\n%s\n' "$FRASE" "$FRASE" | "$BIN" setup --yes --vault "$HOME/vault-e2e" --engine "$ENGINE"
RC=$?
set -e
echo "== setup terminó con código $RC"
if [ "$RC" -ne 0 ]; then
  echo "== diagnóstico"
  "$BIN" doctor || true
  ls -la /tmp | head -20
  exit 1
fi

echo "== tras el asistente: nada en claro en /tmp y ningún proceso del motor vivo"
if ls -d /tmp/flowsint-desk-* 2>/dev/null; then echo "FALLO: quedan restos en /tmp"; exit 1; fi
sleep 3
VIVOS=$(ps -u "$(id -u)" -o pid=,args= | grep -E 'conmon|pasta|aardvark-dns|catatonit|podman|rootlesskit|dockerd|containerd|slirp4netns' | grep -v -e grep -e 'e2e.sh' || true)
if [ -n "$VIVOS" ]; then echo "FALLO: procesos del motor siguen vivos tras cerrar:"; echo "$VIVOS"; exit 1; fi
echo "   ningún proceso del motor vivo"
printf '%s\n' "$FRASE" | "$BIN" verify

# ───────── casos feos, con el motor real ─────────
RUN="/tmp/flowsint-desk-$(cat /proc/sys/kernel/random/boot_id)"
VAULT="$HOME/vault-e2e"
LOG="$HOME/up-e2e.log"
fallo() { echo "FALLO: $1"; [ -f "$LOG" ] && tail -40 "$LOG"; exit 1; }
vivos() { ps -u "$(id -u)" -o pid=,args= | grep -E 'conmon|pasta|aardvark-dns|catatonit|podman|rootlesskit|dockerd|containerd|slirp4netns' | grep -v -e grep -e 'e2e.sh' || true; }
nsnaps() { printf '%s\n' "$FRASE" | "$BIN" snapshots 2>/dev/null | grep -c 'tipo=' || true; }
arrancar() {
  : > "$LOG"
  printf '%s\n' "$FRASE" | "$BIN" up > "$LOG" 2>&1 &
  UP=$!
  i=0
  while [ $i -lt 240 ]; do
    grep -q 'Flowsint listo' "$LOG" && return 0
    kill -0 "$UP" 2>/dev/null || return 1
    sleep 2; i=$((i+1))
  done
  return 1
}

echo "== caso 1: frase incorrecta → error claro, nada arrancado, sin bloqueo"
set +e; printf 'frase-equivocada\n' | "$BIN" up > "$LOG" 2>&1; RC=$?; set -e
[ "$RC" -ne 0 ] && grep -q INCORRECTA "$LOG" || fallo "la frase incorrecta no dio el error esperado"
[ ! -e "$VAULT/flowsint-desk.lock.json" ] || fallo "quedó el bloqueo tras una frase incorrecta"
[ -z "$(vivos)" ] || fallo "quedaron procesos tras una frase incorrecta"

echo "== caso 2: kill -9 a la sesión → down limpia lo que quedó en RAM"
arrancar || fallo "up no arrancó"
kill -9 "$UP"; wait "$UP" 2>/dev/null || true
[ -d "$RUN" ] || fallo "tras kill -9 debería quedar el directorio de ejecución (si no, la prueba no prueba nada)"
"$BIN" down </dev/null || fallo "down tras kill -9 devolvió error"
sleep 3
[ ! -e "$RUN" ] || fallo "down no borró el directorio de ejecución tras kill -9"
[ -z "$(vivos)" ] || { vivos; fallo "quedaron procesos del motor tras kill -9 + down"; }
[ ! -e "$VAULT/flowsint-desk.lock.json" ] || fallo "down no soltó el bloqueo de la sesión muerta"

echo "== caso 3: kill -9 y el SIGUIENTE up limpia solo y restaura"
arrancar || fallo "up no arrancó"
kill -9 "$UP"; wait "$UP" 2>/dev/null || true
arrancar || fallo "up no arrancó tras un kill -9"
grep -q 'restos de una sesión anterior' "$LOG" || fallo "up no detectó los restos del kill -9"
grep -q 'manifest verificado' "$LOG" || fallo "up no restauró el snapshot tras el kill -9"

echo "== caso 4: almacén inaccesible al cerrar → NO se destruye la sesión"
mv "$VAULT" "$VAULT.off"
set +e; "$BIN" down </dev/null > "$HOME/down1.log" 2>&1; RC=$?; set -e
cat "$HOME/down1.log"
[ "$RC" -ne 0 ] && grep -q FALLADO "$HOME/down1.log" || fallo "down no avisó del snapshot final fallido"
kill -0 "$UP" 2>/dev/null || fallo "la sesión se cerró aunque no pudo guardar"
[ -d "$RUN/staging" ] || fallo "se destruyó el directorio de ejecución sin haber guardado"
mv "$VAULT.off" "$VAULT"
N=$(nsnaps)
"$BIN" down </dev/null || fallo "down no cerró tras recuperar el almacén"
wait "$UP" 2>/dev/null || true
sleep 3
[ ! -e "$RUN" ] || fallo "quedó el directorio de ejecución tras el cierre"
[ -z "$(vivos)" ] || { vivos; fallo "quedaron procesos del motor tras el cierre"; }
[ "$(nsnaps)" -gt "$N" ] || fallo "al recuperar el almacén no se guardó el snapshot final"
[ ! -e "$RUN" ] || fallo "un comando de consulta (snapshots) dejó un directorio en /tmp"

echo "== caso 5: segunda copia (mirror)"
printf '%s\n' "$FRASE" | "$BIN" mirror --to "$HOME/mirror-e2e" || fallo "mirror falló"
printf '%s\n' "$FRASE" | "$BIN" mirror | grep -q 'segunda copia al día' || fallo "el segundo mirror no fue incremental"
printf '%s\n' "$FRASE" | "$BIN" status </dev/null | grep -q 'segunda copia: ' || true

echo "== caso 6: SEGUNDO equipo sobre el mismo almacén → se conecta sin crear nada"
N=$(nsnaps)
export FLOWSINT_DESK_CONFIG_DIR="$HOME/config-equipo2"
set +e; printf '%s\n' "$FRASE" | "$BIN" setup --yes --vault "$VAULT" --engine "$ENGINE" > "$HOME/setup2.log" 2>&1; RC=$?; set -e
tail -25 "$HOME/setup2.log"
[ "$RC" -eq 0 ] || fallo "setup en el segundo equipo falló"
grep -q 'SEGUNDO equipo' "$HOME/setup2.log" && grep -q 'conectado al almacén existente' "$HOME/setup2.log" || fallo "no se detectó el segundo equipo"
unset FLOWSINT_DESK_CONFIG_DIR
[ "$(nsnaps)" -eq "$N" ] || fallo "el segundo equipo creó snapshots en el almacén ($N → $(nsnaps))"

sleep 3
if ls -d /tmp/flowsint-desk-* 2>/dev/null; then fallo "quedan restos en /tmp al final"; fi
[ -z "$(vivos)" ] || { vivos; fallo "quedan procesos del motor al final"; }
echo "== E2E OK con $ENGINE (ciclo completo + 6 casos feos)"
