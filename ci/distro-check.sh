#!/bin/sh
# Comprobación por distribución (la ejecuta el CI dentro de un contenedor de
# Arch, Debian y Fedora, como root). Verifica DE VERDAD, contra los repos y
# descargas reales de cada distro:
#   1. `doctor --fix` en un sistema pelado: restic (SHA256SUMS oficial),
#      dockerd-rootless.sh y, en Arch, los binarios rootless con firma.
#   2. que la línea de `doctor --packages` instala sin errores;
#   3. que tras instalarla los requisitos de binarios quedan en verde.
# No arranca Flowsint: un contenedor de CI no es un escritorio con Docker
# rootless. Eso lo cubre `flowsint-desk selftest` en la máquina real.
set -eu
BIN=/usr/local/bin/flowsint-desk
install -m 0755 "$1" "$BIN"

. /etc/os-release
echo "== distro: ${PRETTY_NAME:-$ID}"
case "$ID" in
  debian|ubuntu) apt-get update -qq && apt-get install -y -qq ca-certificates >/dev/null ;;
  arch) pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null ;;
  fedora) dnf install -y -q util-linux shadow-utils >/dev/null ;;  # su y useradd no vienen en la imagen mínima
esac

useradd -m probe
echo "probe:100000:65536" >> /etc/subuid
echo "probe:100000:65536" >> /etc/subgid
as_probe() { su probe -s /bin/sh -c "cd /home/probe && $BIN $*"; }

fail=0
need() { # need "<texto que debe aparecer>" "<fichero de salida>"
  if grep -qF "$1" "$2"; then echo "  OK   $1"; else echo "  FALTA $1"; fail=1; fi
}

echo "== 1. doctor --fix en sistema pelado"
as_probe doctor --fix > /tmp/fix.out 2>&1 || true
cat /tmp/fix.out
need "✔ restic instalado" /tmp/fix.out
need "✔ binario dockerd-rootless.sh" /tmp/fix.out
if [ "$ID" = "arch" ]; then
  need "✔ binario rootlesskit" /tmp/fix.out
  need "✔ binario slirp4netns" /tmp/fix.out
  need "✔ binario fuse-overlayfs" /tmp/fix.out
  need "firma verificada" /tmp/fix.out
fi

echo "== 2. paquetes del sistema que sugiere doctor"
CMD=$(as_probe doctor --packages)
echo "   $CMD"
CMD=${CMD#sudo }
case "$CMD" in
  "apt install"*)  DEBIAN_FRONTEND=noninteractive apt-get install -y ${CMD#apt install } ;;
  "pacman -S"*)    pacman -S --noconfirm --needed ${CMD#pacman -S } ;;
  "dnf install"*)  dnf install -y ${CMD#dnf install } ;;
  *) echo "comando inesperado: $CMD"; exit 1 ;;
esac

echo "== 3. doctor tras instalar"
as_probe doctor > /tmp/doc.out 2>&1 || true
cat /tmp/doc.out
for t in "✔ binario rootlesskit" "✔ binario slirp4netns" "✔ binario fuse-overlayfs" \
         "✔ binario dockerd-rootless.sh" "✔ binario newuidmap" "✔ binario newgidmap" \
         "✔ /etc/subuid tiene entrada para probe" "✔ restic instalado" \
         "✔ binario docker" "✔ docker compose v2"; do
  need "$t" /tmp/doc.out
done
if command -v dockerd >/dev/null 2>&1; then echo "  OK   dockerd presente"; else echo "  FALTA dockerd (el paquete de docker no lo trae)"; fail=1; fi

echo "== 4. la CLI se niega como root"
if "$BIN" version >/dev/null 2>&1; then echo "  FALTA rechazo a root"; fail=1; else echo "  OK   se niega con euid=0"; fi

[ "$fail" -eq 0 ] && echo "== RESULTADO: OK en ${PRETTY_NAME:-$ID}" || { echo "== RESULTADO: FALLOS en ${PRETTY_NAME:-$ID}"; exit 1; }
