param(
    [Parameter(Mandatory = $true)]
    [string]$Source,

    [switch]$DesktopShortcut
)

$ErrorActionPreference = "Stop"

$sourcePath = (Resolve-Path -LiteralPath $Source).Path
$installDir = Join-Path $env:LOCALAPPDATA "Programs\GoplexCLI"
$targetPath = Join-Path $installDir "goplexcli-gui.exe"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item -LiteralPath $sourcePath -Destination $targetPath -Force

$shell = New-Object -ComObject WScript.Shell

function New-GoplexShortcut {
    param([string]$Path)

    $parent = Split-Path -Parent $Path
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    $shortcut = $shell.CreateShortcut($Path)
    $shortcut.TargetPath = $targetPath
    $shortcut.WorkingDirectory = $installDir
    $shortcut.IconLocation = "$targetPath,0"
    $shortcut.Description = "Browse and stream media from Plex"
    $shortcut.Save()
}

$startMenu = [Environment]::GetFolderPath("StartMenu")
$startMenuShortcut = Join-Path $startMenu "Programs\GoplexCLI.lnk"
New-GoplexShortcut -Path $startMenuShortcut

if ($DesktopShortcut) {
    $desktop = [Environment]::GetFolderPath("Desktop")
    New-GoplexShortcut -Path (Join-Path $desktop "GoplexCLI.lnk")
}

Write-Host "Installed GoplexCLI GUI to $targetPath"
Write-Host "Created Start Menu shortcut: $startMenuShortcut"
if ($DesktopShortcut) {
    Write-Host "Created desktop shortcut."
}
