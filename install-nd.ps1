# NextDeploy CLI Windows Installer
$ErrorActionPreference = "Stop"

$repo = "masudranaxpert/NextDeploy"
$arch = if ([System.Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$binary = "nd-windows-$arch.exe"
$url = "https://github.com/$repo/releases/latest/download/$binary"

$installDir = Join-Path $env:LOCALAPPDATA "nd\bin"
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

$destPath = Join-Path $installDir "nd.exe"

Write-Host "-> Downloading nd CLI for Windows ($arch)..." -ForegroundColor Cyan
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -Uri $url -OutFile $destPath -UseBasicParsing

# Check if installDir is in user PATH
$userPath = [System.Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::User)
if ($userPath -notlike "*$installDir*") {
    $newPath = "$userPath;$installDir".Trim(';')
    [System.Environment]::SetEnvironmentVariable("Path", $newPath, [System.EnvironmentVariableTarget]::User)
    $env:Path = "$env:Path;$installDir"
    Write-Host "[+] Added $installDir to User PATH" -ForegroundColor Green
}

Write-Host "[+] nd CLI installed successfully to $destPath" -ForegroundColor Green
Write-Host "    Run 'nd --version' or 'nd login <server_url>' to get started." -ForegroundColor Yellow