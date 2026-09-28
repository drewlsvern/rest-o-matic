<#
.SYNOPSIS
Download and install a rest-o-matic release on Windows.

.DESCRIPTION
Installs the newest rest-o-matic release (pre-releases included), or the one
given with -Version, after checking it against the release's checksums.txt.

From an elevated (administrator) PowerShell it installs for everyone, into
"$env:ProgramFiles\rest-o-matic", and adds that to the machine PATH.
Otherwise it installs just for you, into
"$env:LOCALAPPDATA\Programs\rest-o-matic", and adds that to your user PATH.
It never asks to elevate.

Works in Windows PowerShell 5.1 and PowerShell 7.

.PARAMETER Version
Release to install, e.g. v0.0.1-rc.3 (the leading "v" is optional).
Defaults to $env:RESTOMATIC_VERSION, else the newest release.

.PARAMETER InstallDir
Folder to install into. Defaults to $env:RESTOMATIC_INSTALL_DIR, else the
folder described above.

.PARAMETER List
List available releases, newest first, and stop.

.PARAMETER NoPath
Don't add the install folder to PATH.

.NOTES
When $env:GITHUB_TOKEN is set, it is sent with GitHub API requests (listing
releases), which raises GitHub's rate limit.

.EXAMPLE
irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1 | iex

.EXAMPLE
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.ps1))) -Version v0.0.1-rc.3
#>
param(
    [string]$Version = $env:RESTOMATIC_VERSION,
    [string]$InstallDir = $env:RESTOMATIC_INSTALL_DIR,
    [switch]$List,
    [switch]$NoPath
)

# Everything runs in a child scope, so `irm ... | iex` doesn't leave these
# preferences or variables behind in the caller's session, and a failure
# throws instead of closing the window.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is very slow with its progress bar on 5.1

    $repo = 'drewlsvern/rest-o-matic'
    $api = "https://api.github.com/repos/$repo"
    $download = "https://github.com/$repo/releases/download"

    # Windows PowerShell 5.1 may not enable TLS 1.2, which GitHub requires.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    function Get-Releases {
        try {
            # Without a token GitHub allows only 60 API requests an hour per IP.
            $headers = @{}
            if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $env:GITHUB_TOKEN" }
            @(Invoke-RestMethod -UseBasicParsing -Headers $headers -Uri "$api/releases?per_page=100")
        } catch {
            throw "could not list releases from GitHub (try -Version, or set GITHUB_TOKEN if it's a rate limit): $($_.Exception.Message)"
        }
    }

    if ($List) {
        $releases = Get-Releases
        if ($releases.Count -eq 0) { throw 'no releases found' }
        foreach ($r in $releases) {
            $kind = if ($r.prerelease) { 'pre-release' } else { 'release' }
            Write-Output "$($r.tag_name) $kind"
        }
        return
    }

    if ($env:OS -ne 'Windows_NT') {
        throw 'install.ps1 is for Windows; on Linux or macOS use install.sh'
    }

    if (-not $Version) {
        $releases = Get-Releases
        if ($releases.Count -eq 0) { throw 'no releases found' }
        $Version = $releases[0].tag_name
    }
    if (-not $Version.StartsWith('v')) { $Version = "v$Version" }

    # A 32-bit PowerShell on 64-bit Windows reports x86 here and the real
    # architecture in PROCESSOR_ARCHITEW6432.
    $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($cpu) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' {
            # No windows/arm64 build is published; Windows on ARM runs the
            # amd64 one through emulation.
            $arch = 'amd64'
            Write-Output 'Note: no ARM64 build is published for Windows; installing the amd64 build, which runs under emulation.'
        }
        default { throw "unsupported architecture $cpu; releases are built for amd64" }
    }

    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $InstallDir) {
        $InstallDir = if ($isAdmin) {
            Join-Path $env:ProgramFiles 'rest-o-matic'
        } else {
            Join-Path $env:LOCALAPPDATA 'Programs\rest-o-matic'
        }
    }

    $asset = "rest-o-matic_$($Version.Substring(1))_windows_$arch.zip"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) "rest-o-matic-$([guid]::NewGuid())"
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Output "Downloading rest-o-matic $Version (windows/$arch)..."
        $zip = Join-Path $tmp $asset
        $sums = Join-Path $tmp 'checksums.txt'
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$download/$Version/$asset" -OutFile $zip
        } catch {
            throw "could not download $asset for $Version; check the version with -List"
        }
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$download/$Version/checksums.txt" -OutFile $sums
        } catch {
            throw "could not download checksums.txt for $Version"
        }

        $line = Get-Content $sums | Where-Object { $_ -match ('\s' + [regex]::Escape($asset) + '$') } | Select-Object -First 1
        if (-not $line) { throw "$asset is missing from checksums.txt" }
        $want = ($line -split '\s+')[0]
        $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash
        if ($got -ne $want) { throw "checksum mismatch for $asset" } # -ne ignores case

        Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp 'x') -Force
        $exe = Join-Path $tmp 'x\rest-o-matic.exe'
        if (-not (Test-Path $exe)) { throw "rest-o-matic.exe is missing from $asset" }

        $target = Join-Path $InstallDir 'rest-o-matic.exe'
        try {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
            Copy-Item -Path $exe -Destination $target -Force
        } catch {
            throw "could not write $target ($($_.Exception.Message)); if rest-o-matic is running, stop it and retry, or run from an elevated PowerShell, or pass -InstallDir"
        }
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    # Collect all the output before taking the first line: cutting the
    # pipeline short (Select-Object -First 1) can make the exe exit 1.
    $versionOutput = @(& $target --version)
    if ($LASTEXITCODE -ne 0) { throw "installed $target, but running it failed with exit code $LASTEXITCODE" }
    Write-Output "Installed $($versionOutput[0]) to $target"

    if (-not $NoPath) {
        $scope = if ($isAdmin) { 'Machine' } else { 'User' }
        # Read and write the registry value directly: [Environment]'s helpers
        # would expand entries like %USERPROFILE%\bin in the stored PATH.
        $key = if ($isAdmin) {
            [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Control\Session Manager\Environment', $true)
        } else {
            [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        }
        try {
            $current = [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
            $entries = @($current -split ';' | Where-Object { $_ })
            if ($entries -notcontains $InstallDir) {
                $key.SetValue('Path', (($entries + $InstallDir) -join ';'), 'ExpandString')
                # Setting any variable through [Environment] broadcasts the
                # change, so new terminals see the updated PATH.
                [Environment]::SetEnvironmentVariable('RESTOMATIC_INSTALL_TMP', '1', $scope)
                [Environment]::SetEnvironmentVariable('RESTOMATIC_INSTALL_TMP', $null, $scope)
                Write-Output "Added $InstallDir to your $($scope.ToLower()) PATH; open a new terminal to use rest-o-matic."
            }
        } finally {
            $key.Close()
        }
        if (($env:Path -split ';') -notcontains $InstallDir) { $env:Path = "$env:Path;$InstallDir" }
    }

    if (-not (Get-Command restic -ErrorAction SilentlyContinue)) {
        Write-Output 'Note: restic was not found on PATH; rest-o-matic needs it (e.g. winget install restic.restic).'
    }
}
