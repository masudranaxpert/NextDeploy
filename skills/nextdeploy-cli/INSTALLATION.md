# NextDeploy CLI (`nd`) Installation Guide

NextDeploy distributes standalone, zero-dependency Go binaries for Linux, macOS, and Windows.

---

## 1. Quick One-Liner Install

### Linux / macOS
```bash
curl -fsSL https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.sh | sh
```

### Windows (PowerShell)
```powershell
irm https://raw.githubusercontent.com/masudranaxpert/NextDeploy/main/install-nd.ps1 | iex
```

---

## 2. Manual Binary Download

Pre-built binaries are available from the GitHub Releases page:
- `nd-linux-amd64` / `nd-linux-arm64`
- `nd-darwin-amd64` / `nd-darwin-arm64`
- `nd-windows-amd64.exe`

Move the downloaded binary to a folder in your system `$PATH` (e.g. `/usr/local/bin` on Linux/macOS or `C:\Program Files\nd` on Windows).

---

## 3. Build From Source

If you have Go installed (Go 1.22+ recommended):

```bash
git clone https://github.com/masudranaxpert/NextDeploy.git
cd NextDeploy/cmd/nd
go build -ldflags="-s -w" -o nd .
```
Move the compiled `nd` executable into your system `$PATH`.

---

## 4. Common Troubleshooting

### "Not logged in"
Run `nd login <server_url>` or ensure `ND_SERVER_URL` and `ND_TOKEN` environment variables are exported in your terminal session.

### "app_id required"
Either pass the application ID explicitly (`nd push my-app`) or run `nd link my-app` in the repository root.

### "directory not found"
Ensure the local path exists and you have read permissions to the files being synchronized.

### "401 Unauthorized"
Your API token may have expired or been deleted in the NextDeploy web panel. Generate a new token in **Settings → API Tokens**.

