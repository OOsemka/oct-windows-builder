# Windows Builder: install Windows updates, then seal the golden image.
#
# Started from FirstLogonCommands (answer-file CD). Installs every available
# software update through the built-in Windows Update API. Reboots only when
# Windows says an update needs it, then continues from a startup task (at most
# MaxPasses boots). Updates that need no reboot (for example Defender
# definitions) are installed in the same boot without starting another pass.
# Then runs the same sysprep as the plain answer file. Drivers are skipped so
# the virtio drivers from virtio-win stay as installed.
#
# Progress goes to C:\Windows\Temp\wb-update.log and to the serial console
# (COM1) as "wb-update: <message>" lines, which the builder shows as the
# build status.
#
# Never returns while a reboot or sysprep is in progress: the next
# FirstLogonCommand is a guarded fallback sysprep and must not start early.

$ErrorActionPreference = 'Continue'
$MaxPasses = 8
$MaxRoundsPerBoot = 5
$Dir = 'C:\Windows\Setup\Scripts'
$Local = Join-Path $Dir 'wb-update.ps1'
$Started = Join-Path $Dir 'wb-update.started'
$PassFile = Join-Path $Dir 'wb-update.pass'
$Log = 'C:\Windows\Temp\wb-update.log'
$Task = 'oct-windows-builder-update'

function Write-Status([string]$Message) {
    Add-Content -Path $Log -Value ("{0} {1}" -f (Get-Date -Format s), $Message)
    try {
        $port = New-Object System.IO.Ports.SerialPort 'COM1', 115200, 'None', 8, 'One'
        $port.Open()
        $port.WriteLine("wb-update: $Message")
        $port.Close()
    } catch {
        # No serial console: the log file still has the progress.
    }
}

function Test-DefinitionUpdate($Update) {
    foreach ($c in $Update.Categories) {
        if ($c.Name -eq 'Definition Updates') { return $true }
    }
    return $false
}

function Invoke-Seal {
    Write-Status 'no updates left; cleaning up the component store'
    schtasks.exe /Delete /TN $Task /F 2>$null | Out-Null
    Dism.exe /Online /Cleanup-Image /StartComponentCleanup /Quiet | Out-Null
    Remove-Item -Path $PassFile -Force -ErrorAction SilentlyContinue
    Write-Status 'running sysprep (the VM shuts down when it is done)'
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

$rebootNeeded = $false
if ($pass -le $MaxPasses) {
    try {
        $session = New-Object -ComObject Microsoft.Update.Session
        for ($round = 1; $round -le $MaxRoundsPerBoot; $round++) {
            Write-Status "pass $pass/${MaxPasses}: checking for updates"
            $result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
            $updates = New-Object -ComObject Microsoft.Update.UpdateColl
            $substantive = 0
            foreach ($u in $result.Updates) {
                if ($u.InstallationBehavior.CanRequestUserInput) { continue }
                if (-not $u.EulaAccepted) { $u.AcceptEula() }
                [void]$updates.Add($u)
                if (-not (Test-DefinitionUpdate $u)) { $substantive++ }
                Add-Content -Path $Log -Value ("{0}   found: {1}" -f (Get-Date -Format s), $u.Title)
            }
            if ($updates.Count -eq 0) { break }
            Write-Status "pass $pass/${MaxPasses}: downloading $($updates.Count) update(s)"
            $downloader = $session.CreateUpdateDownloader()
            $downloader.Updates = $updates
            [void]$downloader.Download()
            Write-Status "pass $pass/${MaxPasses}: installing $($updates.Count) update(s)"
            $installer = $session.CreateUpdateInstaller()
            $installer.Updates = $updates
            $r = $installer.Install()
            Write-Status "pass $pass/${MaxPasses}: installed $($updates.Count) update(s), result $($r.ResultCode), reboot required $($r.RebootRequired)"
            if ($r.RebootRequired) { $rebootNeeded = $true; break }
            # Only definition updates: nothing more to chase in this boot.
            if ($substantive -eq 0) { break }
        }
    } catch {
        Write-Status "Windows Update error: $_"
    }
} else {
    Write-Status 'pass limit reached'
}

if ($rebootNeeded) {
    schtasks.exe /Create /TN $Task /SC ONSTART /RU SYSTEM /RL HIGHEST /F `
        /TR "powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Local" | Out-Null
    Write-Status "pass $pass/${MaxPasses}: rebooting to finish installing updates"
    Restart-Computer -Force
    Start-Sleep -Seconds 3600
    exit
}

Invoke-Seal
