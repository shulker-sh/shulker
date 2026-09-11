# shulker installer for Windows. Downloads a release from GitHub, verifies its SHA256
# checksum and (when gh is installed) its build provenance, installs shulker.exe, and adds
# the install directory to the user PATH.
#
#   irm https://shulker.sh/install.ps1 | iex
#
# Environment:
#   SHULKER_INSTALL_DIR=...            install location (default: %LOCALAPPDATA%\Programs\shulker)
#   SHULKER_VERSION=v0.0.1             install a specific tag (default: latest)
#   SHULKER_WITHOUT_ATTESTATION=1      skip the build provenance check
#   SHULKER_REQUIRE_ATTESTATION=1      fail unless build provenance is verified
#   SHULKER_NO_MODIFY_PATH=1           don't add the install directory to the user PATH

# iex runs in the caller's session: the script block keeps these settings out of it, and
# errors throw instead of exiting so the window stays open.
& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  $owner = 'shulker-sh'
  $repo = 'shulker'
  $installDir = if ($env:SHULKER_INSTALL_DIR) { $env:SHULKER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\shulker' }

  function Note($msg) { Write-Host "==> $msg" }

  $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'ARM64' { 'arm64' }
    'AMD64' { 'amd64' }
    default { throw "shulker install: unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
  }

  $version = $env:SHULKER_VERSION
  if (-not $version) {
    Note 'resolving latest release'
    $version = (Invoke-RestMethod "https://api.github.com/repos/$owner/$repo/releases/latest").tag_name
    if (-not $version) { throw "shulker install: no release found at https://github.com/$owner/$repo/releases" }
  }
  $plain = $version.TrimStart('v')
  $base = "https://github.com/$owner/$repo/releases/download/$version"
  $archive = "shulker_${plain}_windows_$arch.zip"

  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("shulker-install-" + [Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    $archivePath = Join-Path $tmp $archive
    Note "downloading $archive"
    try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath }
    catch { throw "shulker install: download failed: $base/$archive" }
    try { $checksums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt").Content }
    catch { throw "shulker install: could not download $base/checksums.txt" }
    if ($checksums -is [byte[]]) { $checksums = [Text.Encoding]::UTF8.GetString($checksums) }

    Note 'verifying SHA256 checksum'
    $want = $null
    foreach ($line in $checksums -split "`n") {
      $fields = $line.Trim() -split '\s+'
      if ($fields.Count -eq 2 -and $fields[1] -eq $archive) { $want = $fields[0] }
    }
    if (-not $want) { throw "shulker install: no checksum listed for $archive" }
    $got = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "shulker install: checksum mismatch for $archive (want $want, got $got)" }

    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($env:SHULKER_WITHOUT_ATTESTATION) {
      Note 'skipping build provenance check (SHULKER_WITHOUT_ATTESTATION)'
    } elseif ($gh) {
      Note 'verifying build provenance with gh'
      $verified = $false
      try {
        $bundle = Join-Path $tmp 'shulker.attestation.jsonl'
        Invoke-WebRequest -UseBasicParsing -Uri "$base/shulker.attestation.jsonl" -OutFile $bundle
        & $gh.Source attestation verify $archivePath --bundle $bundle --owner $owner *> $null
        $verified = $LASTEXITCODE -eq 0
      } catch {}
      if ($verified) {
        Note 'build provenance verified'
      } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
        throw 'shulker install: build provenance could not be verified and SHULKER_REQUIRE_ATTESTATION is set'
      } else {
        Note 'build provenance could not be verified, continuing on the checksum'
      }
    } elseif ($env:SHULKER_REQUIRE_ATTESTATION) {
      throw 'shulker install: SHULKER_REQUIRE_ATTESTATION is set but gh is not installed'
    } else {
      Note 'gh not found, skipping build provenance check'
    }

    Note "installing to $installDir"
    $extract = Join-Path $tmp 'extract'
    Expand-Archive -Path $archivePath -DestinationPath $extract -Force
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    $exe = Join-Path $installDir 'shulker.exe'
    Copy-Item -Path (Join-Path $extract 'shulker.exe') -Destination $exe -Force
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }

  # The raw registry value keeps %VAR% entries unexpanded; [Environment]::SetEnvironmentVariable
  # would rewrite the user PATH as REG_SZ with them frozen.
  $envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
  try {
    $userPath = [string]$envKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $installDir) {
      if ($env:SHULKER_NO_MODIFY_PATH) {
        Note "$installDir is not on your PATH"
      } else {
        $envKey.SetValue('Path', ((@($installDir) + $entries) -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
        # Without this broadcast, terminals opened from Explorer keep the old PATH until sign-out.
        if (-not ('Shulker.Env' -as [type])) {
          Add-Type -Namespace Shulker -Name Env -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
        }
        $result = [UIntPtr]::Zero
        [Shulker.Env]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result) | Out-Null
        $env:Path = "$installDir;$env:Path"
        Note "added $installDir to your user PATH; open a new terminal to use shulker there"
      }
    }
  } finally {
    $envKey.Close()
  }

  Write-Host "shulker $plain installed to $exe"
  Write-Host ''
  Write-Host 'Get started: https://shulker.sh/docs/getting-started'
}
