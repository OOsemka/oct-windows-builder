# Windows Builder: install Windows updates, then seal the golden image.
#
# Started from FirstLogonCommands (answer-file CD). Installs every available
# software update through the built-in Windows Update API, reboots and
# continues from a startup task until no updates remain (at most MaxPasses),
# then runs the same sysprep as the plain answer file. Drivers are skipped so
# the virtio drivers from virtio-win stay as installed.
#
# Never returns while a reboot or sysprep is in progress: the next
# FirstLogonCommand is a guarded fallback sysprep and must not start early.

$ErrorActionPreference = 'Continue'
$MaxPasses = 8
$Dir = 'C:\Windows\Setup\Scripts'
$Local = Join-Path $Dir 'wb-update.ps1'
$Started = Join-Path $Dir 'wb-update.started'
$PassFile = Join-Path $Dir 'wb-update.pass'
$Log = 'C:\Windows\Temp\wb-update.log'
$Task = 'oct-windows-builder-update'

function Write-Log([string]$Message) {
    Add-Content -Path $Log -Value ("{0} {1}" -f (Get-Date -Format s), $Message)
}

function Invoke-Seal {
    Write-Log 'no updates left: cleaning up and running sysprep'
    schtasks.exe /Delete /TN $Task /F 2>$null | Out-Null
    Dism.exe /Online /Cleanup-Image /StartComponentCleanup /Quiet | Out-Null
    Remove-Item -Path $PassFile -Force -ErrorAction SilentlyContinue
    Start-Process -FilePath 'C:\Windows\System32\Sysprep\sysprep.exe' `
        -ArgumentList '/generalize', '/oobe', '/shutdown', '/quiet', '/mode:vm' -Wait
    Start-Sleep -Seconds 3600
}

New-Item -ItemType Directory -Force -Path $Dir | Out-Null
# The fallback sysprep in FirstLogonCommands only runs when this marker is missing.
Set-Content -Path $Started -Value 'started'
if ($PSCommandPath -and ($PSCommandPath -ne $Local)) {
    Copy-Item -Path $PSCommandPath -Destination $Local -Force
}

$pass = 1
if (Test-Path $PassFile) { $pass = [int](Get-Content $PassFile) + 1 }
Set-Content -Path $PassFile -Value $pass
Write-Log "pass $pass of $MaxPasses"

$installed = 0
if ($pass -le $MaxPasses) {
    try {
        $session = New-Object -ComObject Microsoft.Update.Session
        $result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
        $updates = New-Object -ComObject Microsoft.Update.UpdateColl
        foreach ($u in $result.Updates) {
            if ($u.InstallationBehavior.CanRequestUserInput) { continue }
            if (-not $u.EulaAccepted) { $u.AcceptEula() }
            [void]$updates.Add($u)
            Write-Log "found: $($u.Title)"
        }
        if ($updates.Count -gt 0) {
            $downloader = $session.CreateUpdateDownloader()
            $downloader.Updates = $updates
            [void]$downloader.Download()
            $installer = $session.CreateUpdateInstaller()
            $installer.Updates = $updates
            $r = $installer.Install()
            Write-Log "installed $($updates.Count) update(s): result $($r.ResultCode), reboot required $($r.RebootRequired)"
            $installed = $updates.Count
        }
    } catch {
        Write-Log "Windows Update error: $_"
    }
} else {
    Write-Log 'pass limit reached'
}

if ($installed -gt 0) {
    # Reboot and look again: cumulative updates often unlock further updates.
    schtasks.exe /Create /TN $Task /SC ONSTART /RU SYSTEM /RL HIGHEST /F `
        /TR "powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Local" | Out-Null
    Write-Log 'rebooting to continue'
    Restart-Computer -Force
    Start-Sleep -Seconds 3600
    exit
}

Invoke-Seal
