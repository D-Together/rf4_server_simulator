# capture_fight_load.ps1 - capture one full fight's 14/8 telemetry as raw hex.
#
# Usage:
#   .\tools\capture_fight_load.ps1 -Seconds 180
#   .\tools\capture_fight_load.ps1 -Seconds 180 -DataDir 'C:\path\to\gameserver\workdir'
#
# gameserver reads data/trace_cmds.on and data/trace_full.on at startup, relative
# to its OWN working directory (launcher chdir's to the config.yaml directory).
# If the server was started from another directory, pass -DataDir explicitly.
# -DataDir must be the DATA SUBDIRECTORY (the folder holding trace_cmds.on),
# not the working-directory root.
#
# If both sentinels already exist and gameserver was started after them, tracing
# is already active: use -NoRestart to capture without restarting or touching
# any sentinel (no game disconnect).
#
# Then: hook a fish and fight it all the way to landing (or losing it).
# Output: logs/_fight_load_trace.txt. Also keep the reel damage log for that rod:
# its load_fields (path/kind/n/min/max/last) is the cross-check evidence.

param(
    [int]$Seconds = 180,
    [string]$Out = '',
    [string]$DataDir = '',
    [switch]$NoRestart
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (-not $Out) { $Out = Join-Path $root 'logs\_fight_load_trace.txt' }
if (-not $DataDir) {
    # Prefer the running launcher's install root over this source worktree.
    $launcher = Get-Process -Name launcher -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($launcher -and $launcher.Path) {
        $installRoot = Split-Path (Split-Path $launcher.Path -Parent) -Parent
        $candidate = Join-Path $installRoot 'data'
        if (Test-Path $candidate) { $DataDir = $candidate }
    }
    if (-not $DataDir) { $DataDir = Join-Path $root 'data' }
}

$outDir = Split-Path $Out -Parent
if (-not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }

$cmdOn = Join-Path $DataDir 'trace_cmds.on'
$fullOn = Join-Path $DataDir 'trace_full.on'
$hadCmd = $false
$hadFull = $false
if ($NoRestart) {
    Write-Host 'NoRestart: leaving existing trace sentinels untouched (no restart, no disconnect).'
} else {
    if (-not (Test-Path $DataDir)) { New-Item -ItemType Directory -Path $DataDir -Force | Out-Null }
    $hadCmd = Test-Path $cmdOn
    $hadFull = Test-Path $fullOn
    Write-Host 'Enabling command trace (full hex) ...'
    Set-Content -Path $cmdOn -Value '1' -Encoding ascii
    Set-Content -Path $fullOn -Value '1' -Encoding ascii
}

try {
    if (-not $NoRestart) {
        Write-Host 'Restarting gameserver so it picks up the sentinels ...'
        try {
            Invoke-WebRequest 'http://127.0.0.1:9500/api/restart?name=gameserver' -Method POST -UseBasicParsing -TimeoutSec 30 | Out-Null
            Start-Sleep -Seconds 6
        } catch {
            Write-Host ('Restart failed: ' + $_.Exception.Message)
            Write-Host 'Restart gameserver manually, then re-run with -NoRestart.'
            throw
        }
    }
    Write-Host ''
    Write-Host 'NOW: hook a fish and fight it all the way to landing (or losing it).'
    Write-Host ('Capturing 14/8 for ' + $Seconds + ' seconds -> ' + $Out)
    Write-Host ''
    & (Join-Path $PSScriptRoot 'trace.ps1') -Seconds $Seconds -Filter '14/8' -Out $Out
    Write-Host ''
    Write-Host 'Also save the reel damage log for this rod (load_fields: first/last/min/max).'
    Write-Host ('Next: python tools/analyze_fight_load.py ' + $Out)
}
finally {
    if (-not $NoRestart) {
        Write-Host 'Restoring trace sentinels ...'
        if (-not $hadCmd) { Remove-Item $cmdOn -Force -ErrorAction SilentlyContinue }
        if (-not $hadFull) { Remove-Item $fullOn -Force -ErrorAction SilentlyContinue }
        try {
            Invoke-WebRequest 'http://127.0.0.1:9500/api/restart?name=gameserver' -Method POST -UseBasicParsing -TimeoutSec 30 | Out-Null
            Write-Host 'Gameserver restarted to disable tracing.'
        } catch {
            Write-Host 'Could not restart gameserver; remove sentinels and restart manually.'
        }
    }
}
