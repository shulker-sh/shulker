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
# variables and settings out of that session, and failures throw rather than exit, so an
# error doesn't close your window.
& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  $installDir = if ($env:SHULKER_INSTALL_DIR) { $env:SHULKER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\shulker' }

  function Write-Note($msg) { Write-Host "==> $msg" }

  $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'ARM64' { 'arm64' }
    'AMD64' { 'amd64' }
    default { throw "shulker install: unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
  }

  # 1. Pick the release.
  $version = $env:SHULKER_VERSION

  if (-not $version) {
    Write-Note 'resolving latest release'
    $version = (Invoke-RestMethod "https://api.github.com/repos/shulker-sh/shulker/releases/latest").tag_name
    if (-not $version) { throw "shulker install: no release found at https://github.com/shulker-sh/shulker/releases" }
  }

  $plain = $version.TrimStart('v')
  $base = "https://github.com/shulker-sh/shulker/releases/download/$version"
  $archive = "shulker_${plain}_windows_$arch.zip"

  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("shulker-install-" + [Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp | Out-Null

  try {
    # 2. Download.
    $archivePath = Join-Path $tmp $archive
    Write-Note "downloading $archive"
    try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath }
    catch { throw "shulker install: download failed: $base/$archive" }
    try { $checksums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt").Content }
    catch { throw "shulker install: could not download $base/checksums.txt" }
    if ($checksums -is [byte[]]) { $checksums = [Text.Encoding]::UTF8.GetString($checksums) }

    # 3. Checksum.
    Write-Note 'verifying SHA256 checksum'
    $want = $null

    foreach ($line in $checksums -split "`n") {
      $fields = $line.Trim() -split '\s+'
      if ($fields.Count -eq 2 -and $fields[1] -eq $archive) { $want = $fields[0] }
    }

    if (-not $want) { throw "shulker install: no checksum listed for $archive" }
    $got = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "shulker install: checksum mismatch for $archive (want $want, got $got)" }

    # 4. Build provenance. The release workflow publishes a signed attestation for each
    # archive; gh checks the signature and that it names this archive and the shulker-sh owner.
    $gh = Get-Command gh -ErrorAction SilentlyContinue

    if ($env:SHULKER_WITHOUT_ATTESTATION) {
      Write-Note 'skipping build provenance check (SHULKER_WITHOUT_ATTESTATION)'
    } elseif ($gh) {
      Write-Note 'verifying build provenance with gh'
      $verified = $false

      try {
        $bundle = Join-Path $tmp 'shulker.attestation.jsonl'
        Invoke-WebRequest -UseBasicParsing -Uri "$base/shulker.attestation.jsonl" -OutFile $bundle
        & $gh.Source attestation verify $archivePath --bundle $bundle --owner shulker-sh *> $null
        $verified = $LASTEXITCODE -eq 0
      } catch {}

      if ($verified) {
        Write-Note 'build provenance verified'
      } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
        throw 'shulker install: build provenance could not be verified and SHULKER_REQUIRE_ATTESTATION is set'
      } else {
        Write-Note 'build provenance could not be verified, continuing on the checksum'
      }
    } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
      throw 'shulker install: SHULKER_REQUIRE_ATTESTATION is set but gh is not installed'
    } else {
      Write-Note 'gh not found, skipping build provenance check'
    }

    # 5. Install.
    Write-Note "installing to $installDir"
    $extract = Join-Path $tmp 'extract'
    Expand-Archive -Path $archivePath -DestinationPath $extract -Force
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    $exe = Join-Path $installDir 'shulker.exe'
    Copy-Item -Path (Join-Path $extract 'shulker.exe') -Destination $exe -Force
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }

  # 6. PATH. Your user PATH lives in the registry at HKCU\Environment. It is edited there
  # directly because [Environment]::SetEnvironmentVariable would expand entries like
  # %USERPROFILE% into fixed paths as it saves them.
  $envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)

  try {
    $userPath = [string]$envKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $entries = @($userPath -split ';' | Where-Object { $_ })

    if ($entries -notcontains $installDir) {
      if ($env:SHULKER_NO_MODIFY_PATH) {
        Write-Note "$installDir is not on your PATH"
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
        Write-Note "added $installDir to your user PATH; open a new terminal to use shulker there"
      }
    }
  } finally {
    $envKey.Close()
  }

  Write-Host "shulker $plain installed to $exe"
  Write-Host ''
  Write-Host 'Get started: https://shulker.sh/docs/getting-started'
}
