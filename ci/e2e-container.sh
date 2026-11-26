#!/bin/sh
# Ciclo completo con el Podman DE LA DISTRO (Debian 13, Arch), dentro de un
# contenedor privilegiado del CI. Se ejecuta como root; la CLI, como usuario
# normal. Podman anidado y sin systemd: un fallo puede ser del entorno.
set -eu
BIN=/usr/local/bin/flowsint-desk
install -m 0755 "$1" "$BIN"
. /etc/os-release
echo "== distro: ${PRETTY_NAME:-$ID}"
case "$ID" in
  debian|ubuntu) apt-get update -qq && apt-get install -y -qq ca-certificates procps util-linux >/dev/null ;;
  arch) pacman -Sy --noconfirm --needed archlinux-keyring procps-ng util-linux >/dev/null ;;
esac
useradd -m probe
echo "probe:100000:65536" >> /etc/subuid
echo "probe:100000:65536" >> /etc/subgid
mount -t tmpfs -o size=12g,mode=1777 tmpfs /tmp
as_probe() { su probe -s /bin/sh -c "cd /home/probe && FLOWSINT_DESK_ENGINE=podman $*"; }

CMD=$(as_probe "$BIN doctor --packages"); echo "   $CMD"; CMD=${CMD#sudo }
case "$CMD" in
  "apt install"*) DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ${CMD#apt install } >/dev/null ;;
  "pacman -S"*)   pacman -S --noconfirm --needed ${CMD#pacman -S } >/dev/null ;;
  *) echo "comando inesperado: $CMD"; exit 1 ;;
esac
podman --version

FRASE="frase-de-prueba-del-ci-1234"
set +e
as_probe "printf '%s\n%s\n' '$FRASE' '$FRASE' | $BIN setup --yes --vault /home/probe/vault --engine podman"
RC=$?
set -e
echo "== setup terminó con código $RC"
[ "$RC" -eq 0 ] || { as_probe "$BIN doctor" || true; exit 1; }
if ls -d /tmp/flowsint-desk-* 2>/dev/null; then echo "FALLO: quedan restos en /tmp"; exit 1; fi
echo "== E2E OK con el Podman de ${PRETTY_NAME:-$ID}"
