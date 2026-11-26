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
echo "== E2E OK con $ENGINE"
