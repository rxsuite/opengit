# Running OpenGit on a headless Windows machine

OpenGit runs as a native Windows service: it starts at boot, restarts itself if it crashes, and needs no logged-in user.
(The Windows service code is cross-compiled and checked here but has not yet been run on a real Windows 11 machine; report anything odd.)

## 1. Prerequisites
- **Git for Windows** (https://git-scm.com/download/win), installed for all users with "Git from the command line and also from 3rd-party software", so the service can find `git`. Verify in a new terminal: `git --version`.
- `opengit-windows-amd64.exe` (or `-arm64`) from `./scripts/build.sh` or a GitHub release. Rename it to `opengit.exe`.

## 2. Install the service (Administrator PowerShell)
```powershell
mkdir C:\OpenGit
copy .\opengit.exe C:\OpenGit\opengit.exe
C:\OpenGit\opengit.exe service install -addr 0.0.0.0:3000 -data C:\OpenGit\data
```
Paths must be absolute (services start in `System32`). Logs go to `C:\OpenGit\data\opengit.log`.
Manage it with `opengit service start|stop|uninstall`, or `services.msc` (service name `OpenGit`).

**Create your account first.** The first account to register becomes admin, so do this before other devices can reach it: install with `-addr 127.0.0.1:3000`, register through a browser on that machine (Remote Desktop), then reinstall with `-addr 0.0.0.0:3000 -disable-registration` (`service uninstall` then `service install`; your data is kept).

## 3. Reach it from your network
```powershell
New-NetFirewallRule -DisplayName "OpenGit" -Direction Inbound -Protocol TCP -LocalPort 3000 -Action Allow -Profile Private
```
Give the machine a fixed address with a DHCP reservation in your router, then browse to `http://<that-ip>:3000`.

## 4. Anything beyond your home LAN: do NOT port-forward plain HTTP
Passwords and tokens would cross the internet unencrypted. Pick one:
1. **Tailscale** (recommended): install it on the Windows box and your devices; use the machine's Tailscale name. No router ports opened, encrypted end to end.
2. **Reverse proxy with HTTPS**, e.g. Caddy with a domain name (automatic certificates). Keep OpenGit on `127.0.0.1:3000` and forward 443 to Caddy only.
3. **Built-in TLS**: `-tls-cert cert.pem -tls-key key.pem` (e.g. a certificate from `mkcert` for LAN names, trusted on each client).

Also keep `-disable-registration` on, use long passwords, and prefer access tokens (`ogt_...`) for git and Claude Code.

## 5. Claude Code from another computer
Point the MCP server at the box:
```bash
claude mcp add opengit --env OPENGIT_URL=http://192.168.1.50:3000 --env OPENGIT_TOKEN=ogt_xxx -- /path/to/opengit mcp
```

## 6. Backups and updates
- **Backup:** stop the service, copy `C:\OpenGit\data` (database + all repos), start the service. Schedule it with Task Scheduler. Also keep a copy off the machine.
- **Update:** `opengit service stop`, replace `opengit.exe`, `opengit service start`.
- Repos are plain bare git repositories under `data\repos\<owner>\<name>.git`, so you can always read them with normal git.
