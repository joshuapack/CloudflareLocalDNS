# Cloudflare Local DNS Updater

A lightweight, robust Go daemon that keeps Cloudflare DNS `A` records updated with your public IP address. Designed for local servers, homelabs, and Raspberry Pi deployments.

---

## Features

- **Automatic Zone Discovery**: Automatically identifies the parent Cloudflare Zone for any domain or subdomain (e.g. `home.example.com` -> `example.com`).
- **In-Memory Caching**: Fetches and retains `zone_identifier` and `dns_identifier` at startup, avoiding repeated API lookups.
- **Change Detection**: Queries `https://ipinfo.io` to detect public IP changes. Only sends an update to Cloudflare when the IP address actually changes.
- **Log Management**:
  - Timestamped logging to both file and console.
  - Logs successful startup zone and DNS discovery.
  - Logs every IP transition (`old_ip -> new_ip`).
  - Logs any network or Cloudflare API errors.
  - Suppresses repetitive logs when IP is unchanged to prevent disk bloat.
- **Cross-Platform Compilation**: Preconfigured Makefile targets for macOS (Apple Silicon/ARM64) and Linux (ARM64 for Raspberry Pi).
- **Native systemd Integration**: Makefile commands to install, manage, and monitor the daemon as a systemd service on Debian/Raspberry Pi OS.

---

## Configuration

Create a `.env` file in the project root (see `.env.example`):

```dotenv
AUTH_KEY='your-cloudflare-api-token'
DOMAIN='home.example.com'
LOG='cloudflareUpdateLog.txt'
UPDATE_TIME=3600
```

### Configuration Options

| Variable | Required | Default | Description |
| :--- | :---: | :---: | :--- |
| `AUTH_KEY` | **Yes** | — | Cloudflare API Token with `Zone:Read` and `DNS:Edit` permissions |
| `DOMAIN` | **Yes** | — | Target domain or subdomain to update (e.g., `home.example.com`) |
| `LOG` | No | `cloudflareUpdateLog.txt` | File path to append timestamped logs |
| `UPDATE_TIME` | No | `3600` | IP verification interval in seconds (e.g., `3600` = 1 hour) |

> **Note on updating `.env`:** Configuration is loaded once on application startup. If you change `.env`, restart the process or service for changes to take effect.

---

## Quick Start (Local Run)

### 1. View Help

```bash
make
# or
make help
```

### 2. Run Directly

```bash
make run
# or
go run main.go
```

### 3. Build Local Binary

```bash
make build
./cloudflarelocaldns
```

---

## Cross-Compiling

You can cross-compile binaries on macOS or any Go host:

| Target | Command | Output Binary |
| :--- | :--- | :--- |
| **macOS (ARM64)** | `make build-mac` | `cloudflarelocaldns-mac` |
| **Linux (ARM64)** | `make build-linux-arm` | `cloudflarelocaldns-linux-arm` |
| **Build All** | `make build-all` | Builds both binaries |
| **Clean** | `make clean` | Removes all built binaries |

---

## Raspberry Pi / Debian Service Setup

The repository includes complete systemd service integration.

### Installation Steps

1. **Build the Linux binary** (from your Mac or build machine):

   ```bash
   make build-linux-arm
   ```

2. **Copy the repository to your Raspberry Pi**:

   ```bash
   rsync -avz --exclude='.git' ./ pi@raspberrypi:~/cloudflarelocaldns/
   ```

3. **Install as a systemd service** (on the Raspberry Pi):

   ```bash
   cd ~/cloudflarelocaldns
   sudo make install-service
   ```

   **What `make install-service` does automatically:**
   - Verifies systemd compatibility.
   - Checks if the service is already installed to prevent accidental overwrites.
   - Configures the service to run the binary directly from your project directory.
   - Generates `/etc/systemd/system/cloudflarelocaldns.service` with:
     - `WorkingDirectory` set to the project repository directory.
     - `EnvironmentFile` directly pointing to the project's `.env` file.
     - Automatic crash recovery (`Restart=always`).
   - Reloads systemd, enables the service on boot, and starts it immediately.

---

## Service Management Commands

Run these commands directly on the Raspberry Pi:

| Action | Command |
| :--- | :--- |
| **Check Status** | `make service-status` |
| **View Live Logs** | `make service-logs` *(or `tail -f cloudflareUpdateLog.txt`)* |
| **Restart Service** | `make service-restart` |
| **Stop Service** | `make service-stop` |
| **Start Service** | `make service-start` |
| **Uninstall Service** | `sudo make uninstall-service` |

---

## Updating the Daemon

Because the service runs directly from your working directory, updating is straightforward:

```bash
# 1. Pull latest code
git pull

# 2. Rebuild binary in the directory
make build

# 3. Restart the service
make service-restart
```

---

## Modifying Configuration After Installation

When installed as a service, the service directly references the `.env` in your project directory.

To update settings:

1. Edit your `.env` file in the project directory:

   ```bash
   nano .env
   ```

2. Restart the service (which reloads systemd units and applies the new config):

   ```bash
   make service-restart
   ```

---

## License

MIT
