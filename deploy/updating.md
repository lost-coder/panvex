# Applying updates

Panvex supports operator-triggered, in-app updates for the control-plane
("panel") and agents from the dashboard. This document covers what the
host must provide for those updates to take effect.

## Panel self-update needs a supervisor that relaunches on exit 78

When an operator applies a panel update, the control-plane downloads,
verifies (signature + checksum), and atomically replaces its own binary,
then **exits with code 78** to request a restart. A supervisor must
relaunch the process for the new binary to take over.

- **systemd (supported):** `deploy/install.sh` installs a unit with
  `restart_mode = "supervised"`, `Restart=on-failure`, and
  `RestartForceExitStatus=78`. In-app panel updates work out of the box.
- **Docker / docker-compose (NOT supported for in-app update):** replacing
  the binary inside a running container is ephemeral — after exit-78 the
  container restarts from the **old image**. To update a containerized
  panel, rebuild/pull the new image and recreate the container. The
  in-dashboard "Update panel" button does not apply under Docker.

## Agent self-update

Agents replace their binary and run `systemctl restart panvex-agent`
(installed by `deploy/install-agent.sh`). No exit-78 supervisor wiring is
required on the agent side. If `systemctl` is unavailable, the agent exits
non-zero so `Restart=on-failure` relaunches the already-replaced binary.

## Download source

The agent downloads its release archive directly from the configured
GitHub repository and resolves the per-architecture asset name itself.
Panel-proxied download (for private repositories) is not yet available.
