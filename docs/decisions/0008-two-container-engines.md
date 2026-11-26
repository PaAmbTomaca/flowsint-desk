# 0008 — Two container engines: rootless Docker and rootless Podman

- **Status**: accepted (2026-11-26). Podman is new; Docker remains the default
  for existing installs.
- **Supersedes**: proposal P001 (Podman as an optional backend).
- **Language note**: written in English at the owner's request; the rest of
  `docs/` is in Spanish.

## Context

flowsint-desk was built on **rootless Docker**: every session starts its own
`dockerd` (via `dockerd-rootless.sh` + rootlesskit + slirp4netns +
fuse-overlayfs) with all state on tmpfs, runs the stack with `docker compose`,
and tears everything down on exit.

The 2026-11-26 audit showed where that hurts. Several of the bugs it fixed came
from hand-managing a daemon: a pidfile, orphan processes found by scanning
`/proc`, `.stale` directories, and a helper alpine container whose only job is
to delete the data-root (its files belong to sub-UIDs the host user cannot
remove). Installation needs five packages plus a script downloaded from the
moby repository.

The owner asked whether Podman would be a better fit. This record captures the
reasoning from that discussion, because the answer is "yes, but not for the
reason people usually give".

## The questions that came up, and the answers

**"Are Docker images incompatible with Podman?"** No. Images follow a common
standard (OCI). Podman pulls the same images from the same registries and runs
them unchanged. Nothing about Flowsint's images needs converting. The only
practical difference is naming: Docker assumes `postgres:15-alpine` lives on
Docker Hub; Podman, without a terminal, may refuse to guess. We therefore always
use full names with Podman (`docker.io/library/postgres:15-alpine`).

**"Then what is 'emulated'?"** Not the images — the *remote control*. Docker is
a server that takes HTTP requests on a socket ("create this container", "give me
its logs"). `docker compose` and the Python Docker SDK are clients that speak
that protocol. Podman works differently inside, so it ships a translator,
`podman system service`, that opens a socket *pretending* to be Docker. A
translator is never the original: it lags behind Docker's protocol version, it
gets the basics right and the subtleties sometimes wrong, and it differs between
Podman 5.4 (Debian 13) and Podman 6.x (Arch/CachyOS, Fedora 45).

**"We write the CLI. Why not just talk to Podman directly?"** Exactly — and that
is the design. Everything the CLI does (start containers, dump and restore the
databases, clean up) uses native `podman` commands. No translation is involved.
Flowsint itself does not care which engine runs it; upstream's own `nginx.conf`
is commented as "compatible with Docker and Podman".

**"So where does emulation remain?"** In exactly one place we do not control:
Flowsint's **enrichers**. An enricher is an action that takes a node of the
graph and discovers related nodes (domain → subdomains, IP → open ports…). About
fifty exist. Most call a web API and need no container engine. Six of them
(subfinder, dnsx, httpx, naabu, asnmap, mapcidr — ProjectDiscovery tools) are
external programs: Flowsint asks the engine to pull an image, run it for a few
seconds and return its output. Flowsint does this with the Python Docker SDK,
and the project rule is not to modify Flowsint. For those six, and only those,
Podman has to offer its Docker-speaking socket.

**"How are those temporary containers started under Podman?"** They are
*siblings*, not children:

```
Flowsint (api) ──► filtering proxy (our CLI) ──► podman system service ──► subfinder container
       ◄──────────────────── text output ◄────────────────────────────────────┘
```

Flowsint writes a request to its socket; that socket is our filtering proxy
(`internal/sockproxy`), which only lets through pulls and plain containers (no
mounts, no privileges, no shared namespaces); the Podman service translates it;
Podman creates the container next to the others, it runs, its output travels
back, and it is removed. They live in the session's tmpfs store and vanish with
it. If the service dies, running containers keep running — the service is
disposable, unlike `dockerd`.

**"Podman's rootless is better than Docker's, right?"** Cleaner, yes; safer,
barely. Both rely on the same kernel mechanism (user namespaces): a process that
escapes a container lands as the unprivileged user either way. What Podman does
better is *how it gets there*: rootless is its native mode rather than an add-on
needing rootlesskit and a wrapper script; containers do not hang off a daemon;
default capabilities are fewer. What does **not** change: whoever can talk to
the socket can do anything the user can. "Rootless" means "not root", not
"harmless" — the filtering proxy is needed with both engines.

**The honest summary:** do not switch to Podman expecting a security leap.
Switch because there are fewer parts, one package to install, and cleanup that
does not depend on a daemon staying alive. For a tool whose contract is "nothing
is left behind on exit", that last point matters.

## Decision

1. **Both engines are supported, selected per machine** by `engine` in
   `config.toml` (`docker` or `podman`). The default stays `docker`, so existing
   installs are untouched.
2. **The `setup` wizard asks once** which engine to use, showing which are
   installed and what each implies. `--engine` sets it non-interactively.
3. **With Podman the CLI orchestrates the stack itself** (`dock.StackUp`):
   six `podman run` calls mirroring `assets/compose.yml`, its own readiness
   probes, secrets via `--env-file`. It does **not** drive `docker compose`
   against the emulated API, and it disables engine healthchecks (Podman runs
   those through systemd user timers, which may not see a store in a custom
   location and would leave transient units behind).
4. **Podman is fully confined to the run directory**: its own `storage.conf`
   and `containers.conf` put graph root, run root, volumes, networks and temp
   files under `<runDir>/podman-data` on tmpfs. It never touches
   `~/.local/share/containers`.
5. **Container logs and events go to files, not journald.** Podman's default on
   systemd hosts is the journal, which would write container output to disk in
   clear — a direct breach of "data only in RAM".
6. **`podman system service` runs only as the enrichers' socket**, behind the
   same filtering proxy.
7. **Switching engines later is deliberate, not a live toggle.**
   `flowsint-desk engine <other>` requires a closed session, deletes the image
   cache and sends the user back through `setup`. The data is never touched: it
   lives encrypted in the vault and does not depend on the engine.

### On "uninstall everything and reinstall the CLI"

The owner suggested that changing engines should require removing all packages
and reinstalling the CLI. We did not implement that, because it would be a
ritual with no technical basis: one binary contains both engines, and the data
is engine-independent. The friction that *is* real — closed session, fresh image
cache, requirements of the new engine, a new smoke test — is what
`flowsint-desk engine` enforces and what the warning describes. The CLI never
uninstalls system packages; the user may remove the old engine's packages if
nothing else needs them.

## Consequences

- **Two code paths for starting the stack.** `podmanSpec()` must be kept in sync
  with `assets/compose.yml` by hand (same images, variables, volumes, network
  aliases, single loopback port). A change to one without the other is a bug.
- **Two Podman generations in the field** (5.4 on Debian 13, 6.x on Arch and
  Fedora 45). Orchestrating directly avoids most of the divergence, but the
  enrichers' path still crosses the emulated API on both.
- **`podman-docker` must never be suggested**: it replaces `/usr/bin/docker` and
  conflicts with a real Docker install, which this machine uses for other work.
- **Never call `podman system reset` or `podman system migrate`**: with a wrong
  configuration they act on the user's default store.
- Snapshots are engine-independent: a vault written under one engine restores
  under the other. Image digests are compared ignoring the `docker.io/` prefix.
- naabu (raw-socket port scanning) was already unreliable in rootless Docker;
  Podman grants no `NET_RAW` by default, so it falls back to connect scans.

## Validation status

- Unit tests and the local simulation cover the engine-neutral logic.
- The CI `e2e` job runs the full wizard (requirements, new vault, images, smoke
  test) against real containers for each engine. See the workflow run for the
  current result; this record does not claim more than CI shows.
- Not yet exercised anywhere: an enricher launching a sibling container through
  the proxy on Podman; SELinux enforcing on Fedora; a real desktop session on
  the owner's two machines. `flowsint-desk selftest` is the first thing to run
  there.

## References

- `docs/research/2026-11-26-podman-deep-research-2.md` — gains, losses,
  complexity, risks, sources (Spanish).
- `docs/research/2026-11-26-podman-reanalisis.md`, `2026-11-podman-deep-research.md`.
- `docs/security/2026-11-26-auditoria-repo.md` — the audit that motivated this.
- `internal/dock/engine.go`, `internal/dock/stack.go`, `internal/sockproxy`.
