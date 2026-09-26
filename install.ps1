# Installs shulker on Windows.
#
#   irm https://shulker.sh/install.ps1 | iex
#
# What it does, in order:
#   1. Picks the latest release of github.com/shulker-sh/shulker, or SHULKER_VERSION.
#   2. Downloads that release's archive for your CPU, and its checksums.txt.
#   3. Checks the archive's SHA256 against checksums.txt, and stops on a mismatch.
#   4. If the GitHub CLI (gh) is installed, checks the archive's build provenance: proof
#      that GitHub Actions built it in the shulker-sh organization, not someone's laptop.
#   5. Copies shulker.exe into %LOCALAPPDATA%\Programs\shulker, or SHULKER_INSTALL_DIR.
#   6. If that directory is not on your user PATH, adds it.
#
# It never needs administrator rights. Besides the install directory and your user PATH, it
# only writes to a temporary directory, which it deletes when it finishes.
#
# Options, as environment variables:
#   SHULKER_WITHOUT_ATTESTATION=1   skip step 4
#   SHULKER_REQUIRE_ATTESTATION=1   fail when step 4 can't run or fails
#   SHULKER_NO_MODIFY_PATH=1        skip step 6
#   SHULKER_INSTALL_DIR=<dir>       install somewhere else
#   SHULKER_VERSION=v0.0.1          install a specific release
#
# Set them in the same session before running it:
#
#   $env:SHULKER_NO_MODIFY_PATH = '1'; irm https://shulker.sh/install.ps1 | iex

# iex runs this in your own PowerShell session. Wrapping it in a script block keeps its
# variables and settings out of that session, and a failure is printed rather than exiting,
# so an error doesn't close your window.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    # Character codes rather than literal symbols: Windows PowerShell 5.1 reads a saved .ps1
    # without a byte order mark as ANSI, which would garble them.
    function Write-Mark($glyph, $color, $msg) {
        Write-Host "  $([char]$glyph)" -ForegroundColor $color -NoNewline
        Write-Host " $msg"
    }

    function Write-Ok($msg) { Write-Mark 0x2714 Green $msg }
    function Write-Skip($msg) { Write-Mark 0x2022 DarkGray $msg }

    try {
        $installDir = if ($env:SHULKER_INSTALL_DIR) { $env:SHULKER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\shulker' }

        $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
            'ARM64' { 'arm64' }
            'AMD64' { 'amd64' }
            default { throw "Unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
        }

        # 1. Pick the release.
        $version = $env:SHULKER_VERSION

        if (-not $version) {
            try { $version = (Invoke-RestMethod 'https://api.github.com/repos/shulker-sh/shulker/releases/latest').tag_name } catch {}
            if (-not $version) { throw "Couldn't get the latest release from https://github.com/shulker-sh/shulker/releases" }
        }

        $plain = $version.TrimStart('v')
        $base = "https://github.com/shulker-sh/shulker/releases/download/$version"
        $archive = "shulker_${plain}_windows_$arch.zip"

        Write-Host "  Installing shulker $plain for Windows ($arch)"
        Write-Host ''

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ("shulker-install-" + [Guid]::NewGuid())
        New-Item -ItemType Directory -Path $tmp | Out-Null

        try {
            # 2. Download.
            $archivePath = Join-Path $tmp $archive
            try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath }
            catch { throw "Couldn't download $base/$archive" }
            try { $checksums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt").Content }
            catch { throw "Couldn't download $base/checksums.txt" }
            if ($checksums -is [byte[]]) { $checksums = [Text.Encoding]::UTF8.GetString($checksums) }
            Write-Ok "Downloaded $archive"

            # 3. Checksum.
            $want = $null

            foreach ($line in $checksums -split "`n") {
                $fields = $line.Trim() -split '\s+'
                if ($fields.Count -eq 2 -and $fields[1] -eq $archive) { $want = $fields[0] }
            }

            if (-not $want) { throw "No checksum listed for $archive in checksums.txt" }
            $got = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLower()
            if ($got -ne $want.ToLower()) { throw "Checksum mismatch for $archive (expected $want, got $got)" }
            Write-Ok 'Checksum matches'

            # 4. Build provenance. The release workflow publishes a signed attestation for each
            # archive; gh checks the signature and that it names this archive and the shulker-sh owner.
            $gh = Get-Command gh -ErrorAction SilentlyContinue

            if ($env:SHULKER_WITHOUT_ATTESTATION) {
                Write-Skip 'Build provenance not checked (SHULKER_WITHOUT_ATTESTATION)'
            } elseif ($gh) {
                $verified = $false

                try {
                    $bundle = Join-Path $tmp 'shulker.attestation.jsonl'
                    Invoke-WebRequest -UseBasicParsing -Uri "$base/shulker.attestation.jsonl" -OutFile $bundle
                    & $gh.Source attestation verify $archivePath --bundle $bundle --owner shulker-sh *> $null
                    $verified = $LASTEXITCODE -eq 0
                } catch {}

                if ($verified) {
                    Write-Ok 'Build provenance verified'
                } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
                    throw "Build provenance couldn't be verified, and SHULKER_REQUIRE_ATTESTATION is set"
                } else {
                    Write-Skip "Build provenance couldn't be verified; relying on the checksum"
                }
            } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
                throw "SHULKER_REQUIRE_ATTESTATION is set, but the GitHub CLI (gh) isn't installed"
            } else {
                Write-Skip 'Build provenance not checked: install the GitHub CLI (gh) to check it'
            }

            # 5. Install.
            $extract = Join-Path $tmp 'extract'
            Expand-Archive -Path $archivePath -DestinationPath $extract -Force
            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
            $exe = Join-Path $installDir 'shulker.exe'
            Copy-Item -Path (Join-Path $extract 'shulker.exe') -Destination $exe -Force
            Write-Ok "Installed to $exe"
        } finally {
            Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        }

        # 6. PATH. Your user PATH lives in the registry at HKCU\Environment. It is edited there
        # directly because [Environment]::SetEnvironmentVariable would expand entries like
        # %USERPROFILE% into fixed paths as it saves them.
        $lead = 'Run'
        $envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)

        try {
            $userPath = [string]$envKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $entries = @($userPath -split ';' | Where-Object { $_ })

            if ($entries -notcontains $installDir) {
                if ($env:SHULKER_NO_MODIFY_PATH) {
                    Write-Skip "$installDir isn't on your PATH"
                    $lead = "Add $installDir to your PATH, then run"
                } else {
                    $envKey.SetValue('Path', ((@($installDir) + $entries) -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
                    # Tell running programs that the environment changed, the same way the System
                    # Properties dialog does; otherwise terminals opened from Explorer keep the old PATH
                    # until you sign out. Add-Type only declares the Windows function that sends it.
                    $HWND_BROADCAST = [IntPtr]0xffff
                    $WM_SETTINGCHANGE = 0x1A
                    $SMTO_ABORTIFHUNG = 2

                    if (-not ('Shulker.Env' -as [type])) {
                        Add-Type -Namespace Shulker -Name Env -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
                    }

                    $result = [UIntPtr]::Zero
                    [Shulker.Env]::SendMessageTimeout($HWND_BROADCAST, $WM_SETTINGCHANGE, [UIntPtr]::Zero, 'Environment', $SMTO_ABORTIFHUNG, 5000, [ref]$result) | Out-Null
                    $env:Path = "$installDir;$env:Path"
                    Write-Ok "Added $installDir to your user PATH"
                }
            }
        } finally {
            $envKey.Close()
        }

        Write-Host ''
        Write-Host "  $lead " -NoNewline
        Write-Host 'shulker --help' -ForegroundColor Cyan -NoNewline
        Write-Host ' to get started.'
        Write-Host '  Docs: https://shulker.sh/docs/getting-started'
    } catch {
        Write-Mark 0x2718 Red $_.Exception.Message
    }
}
