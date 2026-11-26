# flowsint-desk

> Spanish version: [README.md](README.md)

**Public repository on GitHub** · Generated with **Mistral Large 4**

Flowsint (graph OSINT) on your desktop PC, **ephemeral and encrypted**:

- Data exists **in cleartext only in RAM** (tmpfs) while the app is open.
  When you close it (or shut down the PC), nothing is left on the local disk.
- Persistence is **encrypted backups with restic** in a folder of your pCloud.
  With the passphrase, any machine with this CLI recovers everything (data and
  Flowsint's internal secrets: `AUTH_SECRET`, `MASTER_VAULT_KEY_V1`, Postgres/Neo4j
  passwords and the API-key vault).
- **Without modifying Flowsint**: official images are used as-is; everything is
  configured from the outside (compose file embedded in the binary).
- **Without sudo**: rootless Docker, with its own per-session daemon and a
  tmpfs data-root.
- Only `127.0.0.1:5273` is published. Neither the API, Neo4j, Postgres nor Redis
  leave loopback. Redis runs without persistence (`--save "" --appendonly no`).

## Why flowsint-desk exists

Flowsint is built for a **server**: its `docker-compose.prod.yml` publishes ports
on `0.0.0.0` and assumes persistent state on disk. On a desktop PC holding
sensitive OSINT data that is not acceptable: we don't want to expose anything
beyond loopback, nor leave cleartext data on the local disk. `flowsint-desk`
wraps Flowsint with those guarantees **from the outside, without touching its
code**: all state lives in tmpfs (RAM) while the session is open —including the
Postgres and Neo4j volumes and the container logs, because the rootless Docker
data-root is on tmpfs—; Redis persists nothing; and the only thing that survives
on disk is the **encrypted** restic repository in pCloud, protected solely by
the passphrase. When the session closes (or the PC shuts down) the tmpfs goes
away with the RAM: nothing cleartext is left on the local disk.

## Credits

All credit for the OSINT engine goes to **[Flowsint](https://github.com/reconurge/flowsint)**
and its team: API, Neo4j graphs, enrichers that spawn containers, encrypted key
vault... a huge job. Our decision to deploy it locally with ephemeral rootless
Docker is based on its architecture (e.g. the Docker socket that `api`/`celery`
mount for the enrichers). This CLI only adds the ephemeral, encrypted desktop
layer on the outside, using the official images as-is.

## Requirements and install

The quick path is **`flowsint-desk setup`** (same as `doctor --fix`): it
automatically installs into `~/.local/bin` everything that does not need sudo
(restic verified by sha256, rootlesskit, slirp4netns, fuse-overlayfs,
`dockerd-rootless.sh`), creates the image cache and `config.toml`, and at the end
prints **only the pending steps** that do need sudo (usually once:
`/etc/subuid` and `/etc/subgid`). The CLI never runs sudo: pending commands are
shown for you to copy.

`flowsint-desk doctor` checks **all** requirements and, if anything is missing,
prints the exact step to fix it (`→ arreglar: ...`). With
`flowsint-desk doctor --fix` it automatically fixes everything that does not
require sudo: downloads restic to `~/.local/bin` verifying its official sha256,
the rootless binaries (`rootlesskit`, `slirp4netns`, `fuse-overlayfs`,
`dockerd-rootless.sh`) from the official mirrors, and creates the image cache
and `config.toml` with 0600 permissions. Anything that does require sudo
(`/etc/subuid`/`/etc/subgid`, mounting `/tmp` as tmpfs, sysctls) is only printed.

Checks: rootless binaries and `newuidmap`/`newgidmap`; entries in
`/etc/subuid` and `/etc/subgid`; user namespaces enabled; kernel ≥ 5.11;
restic ≥ 0.17; `docker compose` v2; `/tmp` on tmpfs with ≥ 8 GiB free;
≥ 4 GB free RAM; port free; vault exists, is writable and sits on the pCloud
FUSE mount; restic repo initialized; lock status between machines; `config.toml`
with 0600 permissions and no secret-looking keys; Secret Service keyring;
`systemd-inhibit`; writable image cache with ≥ 6 GiB free in `$HOME`;
HTTPS connectivity to `ghcr.io` and `github.com`.

Minimal no-sudo install (Arch/CachyOS):

```sh
# restic (static binary)
curl -LO https://github.com/restic/restic/releases/download/v0.19.1/restic_0.19.1_linux_amd64.bz2
bzip2 -d restic_0.19.1_linux_amd64.bz2 && chmod +x restic_0.19.1_linux_amd64
mv restic_0.19.1_linux_amd64 ~/.local/bin/restic
# rootless (Arch packages extracted by hand, no root)
for p in rootlesskit-3.3.0-1-x86_64 slirp4netns-1.3.7-1-x86_64 fuse-overlayfs-1.18-1-x86_64; do
  curl -O "https://geo.mirror.pkgbuild.com/extra/os/x86_64/$p.pkg.tar.zst"
  tar --zstd -xf "$p.pkg.tar.zst" && cp usr/bin/* ~/.local/bin/ 2>/dev/null
done
curl -o ~/.local/bin/dockerd-rootless.sh https://raw.githubusercontent.com/moby/moby/master/contrib/dockerd-rootless.sh
chmod +x ~/.local/bin/dockerd-rootless.sh
# subuid (this one does need sudo, once)
echo "$(whoami):100000:65536" | sudo tee -a /etc/subuid | sudo tee -a /etc/subgid >/dev/null
```

(That block is exactly what `flowsint-desk setup` does automatically, except the
`sudo` step, which is always left to you.)

## Usage

```sh
flowsint-desk doctor                          # all requirements, with how to fix each one
flowsint-desk doctor --fix                    # auto-fix what does not need sudo
flowsint-desk init --vault ~/pCloudDrive/flowsint-vault
#   → FIRST machine: asks for the passphrase twice (min. 8; warns below 16),
#     creates the restic repo and the first snapshot, and offers the keyring.
#   → SECOND machine (the vault already has snapshots): asks once, checks the
#     passphrase and does NOT create or modify anything in the vault.
flowsint-desk up                              # unlock, restore and open the UI
#   ✔ vault unlocked · ✔ no lock from another machine · ✔ restored snapshot 3f2a…
#   ✔ Flowsint ready at http://127.0.0.1:5273  (Ctrl+C to save and close)
flowsint-desk status                          # session, lock (host/heartbeat), last snapshot
flowsint-desk backup                          # immediate snapshot of the running session
flowsint-desk snapshots                       # date, host, size, manifest counts
flowsint-desk up --snapshot 3f2a9c…           # start from an older snapshot
flowsint-desk down                            # orderly shutdown; if the session died (kill -9), cleans up its leftovers
flowsint-desk unlock --force                  # break a stale lock from another machine
flowsint-desk forget                          # retention (24 h covered, 30 d, 12 m) + prune
#   (restic keep-hourly=N = N hours covered, 1 snapshot per hour, plus the
#    oldest of each bucket; keep-daily/keep-monthly likewise per day/month)
flowsint-desk passwd                          # change the passphrase
flowsint-desk logs [--tail N] [filter]        # logs of the live session's containers, sanitized (secrets → ***)
```

> Note: the passphrase is resolved in this order: `runDir/passphrase` (tmpfs,
> left by `init` until the first `up`) → keyring → prompt. If the stored one does
> not open the vault (keyring holding another vault's passphrase, or stale after a
> `passwd` run on the other machine), you are prompted instead of failing.

`up` stays in the foreground: it saves automatically every 10 min (configurable),
renews the lock heartbeat every minute and, on Ctrl+C/SIGTERM or at system
shutdown (`systemd-inhibit --what=shutdown --mode=delay`), takes a **final
snapshot** before tearing everything down and deleting the run directory.

**If the final snapshot fails, the session is NOT closed** (pCloud unmounted,
lock lost, restic error...): closing would wipe from RAM the only copy of the
unsaved work. `up` reports what failed and how old the last good snapshot is;
fix it and repeat Ctrl+C or `down`. To close **without saving**, repeat the
request within 2 minutes. At system shutdown there is no second chance: if
there is no time or pCloud is already gone, the last automatic snapshot stands.

A startup that fails or that you cancel with Ctrl+C is torn down completely
(daemon, containers, restored secrets and dumps): nothing decrypted stays in RAM.

## What a snapshot contains (encrypted by restic, only in pCloud)

- `postgres.sql` — plain-text `pg_dump` (restic encrypts and deduplicates afterwards).
- `neo4j.cypher` — APOC export of the whole graph (plain format; restored with
  `cypher-shell -f` into an empty DB; verified in tests: identical except
  `elementId`, which nothing persists outside Neo4j). After restoring, the
  idempotent Neo4j migrations are re-applied (the export does not carry range
  indexes).
- `secrets.env` — Flowsint's internal secrets.
- `manifest.json` — CLI version, image digests, host, date and control counts
  (rows per Postgres table, nodes per label and relationships per type in
  Neo4j) + sha256 of each file. After restoring they are compared; **if they do
  not match, the UI does not start**.

## Security

- **Never as root**: the CLI refuses to start with `euid=0` (no
  `sudo flowsint-desk ...`), so the system's rootful Docker can never be touched
  from here.
- **All state in RAM**: the rootless Docker data-root is on tmpfs, so the
  Postgres/Neo4j volumes and the container logs only live in RAM while the
  session is open; Redis runs with `--save "" --appendonly no`. The only thing
  persistent on disk is the **encrypted** restic repo in pCloud (plus the
  secret-free config and the public image cache). On shutdown the tmpfs
  disappears: data remains locked only in the encrypted exports.
- `umask 077`, `RLIMIT_CORE=0`, no secrets in argv or logs. **Warnings and logs are
  non-compromising**: any external output (compose, restic, `docker logs`) is
  masked first (secrets and passphrase → `***`); if startup fails, `up` dumps the
  involved containers' logs already sanitized, and `flowsint-desk logs` shows them live.
- **Swap**: tmpfs can be paged out to swap. If your swap is on disk and
  unencrypted (typical with hibernation), session data may end up in clear on
  disk. `doctor` warns about any non-zram swap; do not hibernate with the
  session open.
- 🔎 **Full repo audit** (2026-11-26): [`docs/security/2026-11-26-auditoria-repo.md`](docs/security/2026-11-26-auditoria-repo.md) (Spanish) — 20 bugs fixed (data loss on close, `init` on an existing vault, retention that deleted nothing, lock with duplicate hostnames...) and what is still open.
- 🛡 **Severe security audit** (2026-11-20): [`docs/security/2026-11-auditoria-seguridad.md`](docs/security/2026-11-auditoria-seguridad.md) — findings with severity, evidence and mitigations (2 high: images without digest pin and rootless socket mounted in api/celery; 3 medium; 8 controls verified green).
- The passphrase
  reaches restic via `RESTIC_PASSWORD_FILE` (a 0600 file in tmpfs, deleted at
  the end).
- Before writing anything, `statfs` verifies the run directory is on tmpfs
  (`TMPFS_MAGIC`); otherwise it refuses.
- The system (rootful) Docker is never touched: everything goes through
  `DOCKER_HOST` to the session's rootless socket.
- Cross-machine lock: `flowsint-desk.lock.json` in the vault with host, pid,
  boot_id and heartbeat (15 min). pCloud is not atomic: this protects one
  person with two machines, not real concurrency. Before each snapshot the lock
  is re-read; if another machine took it, saving stops and it warns loudly.
- After a `kill -9` or power cut, the next `up` detects the leftovers of the
  same `boot_id`, kills the orphan daemons, wipes them and restores the last
  snapshot.
- The image cache (`~/.cache/flowsint-desk/images/`) does live on disk: public
  images contain no user data.

## Configuration

`~/.config/flowsint-desk/config.toml` (0600, **no secrets**):

```toml
vault_path = "/home/pollo/pCloudDrive/flowsint-vault"
port = 5273
save_interval_min = 10
keep_hourly = 24
keep_daily = 30
keep_monthly = 12
```

## Documentation

🗺️ **Visual map of the whole architecture** (components, `up` lifecycle, where each
piece of data lives, snapshots, security, commands): [`docs/mapa-arquitectura.html`](docs/mapa-arquitectura.html)
(open it in a browser).

Architecture decisions live in [`docs/`](docs/README.md), ADR style:
`docs/decisions/` holds the **current** decisions (ephemeral rootless Docker,
restic in pCloud, cleartext only in tmpfs, loopback-only exposure, single
passphrase, Neo4j snapshot method) and `docs/proposals/` the **pending
proposals** (e.g. P001: Podman as an optional backend in v2).

## Development

```sh
go build -o dist/flowsint-desk .
go vet ./...
go test ./...     # no Docker or restic needed: lock, snapshot, config, redaction logic...
```

Unit tests start nothing; the real acceptance battery (section 7 of
`ENCARGO.md`) is still manual, against rootless Docker and a test vault.
CI (`.github/workflows/ci.yml`) runs build + vet + test on every push.
