# datamanagementd Deployment Guide (Data Management)

This document explains how to deploy `datamanagementd` on the host machine and integrate it with the main process to enable the "Data Management" feature.

## 1. Key Constraints

- The main process probes a fixed path: `/tmp/sub2api-datamanagement.sock`
- The "Data Management" feature in the admin panel is only enabled when the Unix Socket is reachable and the `Health` check succeeds
- `datamanagementd` uses SQLite to persist metadata and does not depend on the main database

## 2. Building and Running on the Host

```bash
cd /opt/sub2api-src/datamanagement
go build -o /opt/sub2api/datamanagementd ./cmd/datamanagementd

mkdir -p /var/lib/sub2api/datamanagement
chown -R sub2api:sub2api /var/lib/sub2api/datamanagement
```

Manual startup example:

```bash
/opt/sub2api/datamanagementd \
  -socket-path /tmp/sub2api-datamanagement.sock \
  -sqlite-path /var/lib/sub2api/datamanagement/datamanagementd.db \
  -version 1.0.0
```

## 3. systemd Management (Recommended)

The repository provides an example service file: `deploy/sub2api-datamanagementd.service`

```bash
sudo cp deploy/sub2api-datamanagementd.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sub2api-datamanagementd
sudo systemctl status sub2api-datamanagementd
```

View logs:

```bash
sudo journalctl -u sub2api-datamanagementd -f
```

You can also use the one-click installation script (automatically installs the binary and registers the systemd service):

```bash
# Method 1: Use a pre-built binary
sudo ./deploy/install-datamanagementd.sh --binary /path/to/datamanagementd

# Method 2: Build from source and install
sudo ./deploy/install-datamanagementd.sh --source /path/to/sub2api
```

## 4. Docker Deployment Integration

If `sub2api` runs in a Docker container, mount the host Socket to the same path inside the container:

```yaml
services:
  sub2api:
    volumes:
      - /tmp/sub2api-datamanagement.sock:/tmp/sub2api-datamanagement.sock
```

It is recommended to maintain this mount in `docker-compose.override.yml` to avoid overwriting the main compose file.

## 5. Dependency Check

`datamanagementd` depends on the following tools when performing backups:

- `pg_dump`
- `redis-cli`
- `docker` (only when `source_mode=docker_exec`)

Missing dependencies will cause the corresponding task to fail, and the error message will be shown in the task details.
