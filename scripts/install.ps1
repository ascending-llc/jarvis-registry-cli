# Installs the latest (or a pinned) jarvis-registry release on Windows.
#
#   irm https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.ps1 | iex
#
# Configuration is environment variables only:
#   JARVIS_REGISTRY_VERSION      release tag to install (e.g. v0.6.7); defaults to the latest release.
#   JARVIS_REGISTRY_INSTALL_DIR  install directory; defaults to
#                                %LOCALAPPDATA%\Programs\jarvis-registry (non-elevated) or
#                                %ProgramFiles%\jarvis-registry (elevated).
#
# Supports Windows PowerShell 5.1 and PowerShell 7+. Only releases signed with ASCENDING's Azure
# Artifact Signing identity install; every release up to and including v0.6.6 is unsigned and is
# rejected.
#
# Everything runs inside one script block so that, under `irm | iex`, no functions, variables, or
# preference changes leak into the caller's session, and failures throw instead of calling `exit`,
# which would close the caller's interactive window. A non-interactive host (`powershell -Command`,
# `-File`) still exits non-zero on the thrown error.
& {
    Set-StrictMode -Version 3.0
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1's progress rendering slows Invoke-WebRequest by orders of magnitude.
    $ProgressPreference = 'SilentlyContinue'

    $ReleaseApi = 'https://api.github.com/repos/ascending-llc/jarvis-registry-cli/releases/latest'
    $ReleaseBase = 'https://github.com/ascending-llc/jarvis-registry-cli/releases/download'

    # Pinned Authenticode signer identity, recorded from AS-1874's sign-check.yaml run. Azure
    # Artifact Signing reissues leaf certificates daily (72-hour validity), so neither the leaf
    # thumbprint nor any chain certificate is pinned. The identity EKU is ASCENDING's durable
    # subscriber identity (not the shared Public Trust marker 1.3.6.1.4.1.311.97.1.0); the Subject
    # comes from the same identity validation and stays constant across leaf rotations.
    $ExpectedIdentityEku = '1.3.6.1.4.1.311.97.998331463.743287935.177617509.963801539'
    $ExpectedSubject = 'CN="ASCENDING, Inc.", O="ASCENDING, Inc.", L=Fairfax, S=Virginia, C=US'

    function Fail([string] $Message) {
        throw "jarvis-registry installer: $Message"
    }

    function Resolve-Arch {
        # A 32-bit (WOW64) host, such as the one Intune runs scripts in by default, reports x86 in
        # PROCESSOR_ARCHITECTURE and the OS's native architecture in PROCESSOR_ARCHITEW6432.
        $nativeArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
        switch ($nativeArch) {
            'AMD64' { return 'amd64' }
            'ARM64' { return 'arm64' }
            default { Fail "unsupported Windows architecture: $nativeArch" }
        }
    }

    function Resolve-Tag {
        if ($env:JARVIS_REGISTRY_VERSION) {
            $tag = 'v' + ($env:JARVIS_REGISTRY_VERSION -replace '^v', '')
        } else {
            try {
                $tag = [string] (Invoke-RestMethod -Uri $ReleaseApi -UseBasicParsing).tag_name
            } catch {
                Fail "could not resolve the latest GitHub release: $($_.Exception.Message)"
            }
        }

        if ($tag -cnotmatch '^v[0-9][0-9A-Za-z.+-]*$') {
            Fail "invalid release version: $tag"
        }
        return $tag
    }

    function Save-ReleaseAsset([string] $Url, [string] $OutFile) {
        try {
            # -UseBasicParsing: Windows PowerShell 5.1 otherwise throws where the IE engine is
            # unavailable; it is a no-op on PowerShell 7.
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
        } catch {
            Fail "could not download ${Url}: $($_.Exception.Message)"
        }
    }

    function Assert-Checksum([string] $ArchivePath, [string] $ChecksumsPath) {
        $archiveName = Split-Path -Leaf $ArchivePath
        $hashes = @()
        foreach ($line in Get-Content -LiteralPath $ChecksumsPath) {
            if ($line -match '^\s*([0-9A-Fa-f]{64})\s+(\S+)\s*$' -and $Matches[2] -ceq $archiveName) {
                $hashes += $Matches[1]
            }
        }
        if ($hashes.Count -ne 1) {
            Fail "checksums.txt must contain exactly one entry for $archiveName"
        }

        $expected = $hashes[0]
        $actual = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash
        # -ne on strings is case-insensitive: checksums.txt is lowercase, Get-FileHash uppercase.
        if ($actual -ne $expected) {
            Fail "SHA-256 verification failed for $archiveName (expected $expected, got $actual)"
        }
    }

    function Assert-Signature([string] $ExePath) {
        $sig = Get-AuthenticodeSignature -LiteralPath $ExePath
        $cert = $sig.SignerCertificate
        $subject = if ($null -ne $cert) { $cert.Subject } else { '<none>' }
        # StatusMessage is localized to the host's display language, so it is only ever reported,
        # never branched on.
        $details = "signer subject: $subject; status message: $($sig.StatusMessage)"

        # Anything but Valid aborts, UnknownError included: PowerShell maps every unrecognized
        # WinVerifyTrust failure there, including a revoked certificate, an untrusted root, and a
        # broken chain. Valid already means WinVerifyTrust chained the signer to a trusted root.
        if ($sig.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
            $hint = switch ($sig.Status) {
                'NotSigned' {
                    ' This installer supports signed releases only (v0.6.7 and later); install older' +
                    ' releases manually from the releases page.'
                }
                'UnknownError' {
                    ' One possible cause is blocked access to the certificate revocation list at' +
                    ' http://www.microsoft.com/pkiops/.'
                }
                default { '' }
            }
            Fail "Authenticode signature of jarvis-registry.exe is $($sig.Status), not Valid ($details).$hint"
        }

        $ekus = @(
            $cert.Extensions |
                Where-Object { $_ -is [System.Security.Cryptography.X509Certificates.X509EnhancedKeyUsageExtension] } |
                ForEach-Object { $_.EnhancedKeyUsages } |
                ForEach-Object { $_.Value }
        )
        if ($ekus -notcontains $ExpectedIdentityEku) {
            Fail "jarvis-registry.exe is not signed with ASCENDING's identity: signer lacks EKU $ExpectedIdentityEku ($details)"
        }
        if ($subject -cne $ExpectedSubject) {
            Fail "jarvis-registry.exe is not signed with ASCENDING's identity: expected signer subject $ExpectedSubject ($details)"
        }
    }

    function Add-InstallDirToPath([string] $InstallDir, [bool] $Machine) {
        # This writes the same registry value that
        # [Environment]::SetEnvironmentVariable('PATH', ..., 'User'|'Machine') would, but directly:
        # round-tripping through [Environment] expands every %VAR% entry into a fixed absolute path
        # and rewrites the value as REG_SZ, silently breaking entries such as %SystemRoot%\system32
        # or %JAVA_HOME%\bin. Reading without expansion and keeping the value's kind avoids that.
        if ($Machine) {
            $key = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey(
                'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', $true)
            $scope = 'machine'
        } else {
            $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
            $scope = 'user'
        }

        try {
            $raw = ''
            $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
            if ($key.GetValueNames() -contains 'Path') {
                $raw = [string] $key.GetValue('Path', '',
                    [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
                $existingKind = $key.GetValueKind('Path')
                if ($existingKind -eq [Microsoft.Win32.RegistryValueKind]::String) {
                    $kind = $existingKind
                }
            }

            $entries = @($raw -split ';' | Where-Object { $_ -ne '' })
            $normalizedDir = $InstallDir.TrimEnd('\')
            foreach ($entry in $entries) {
                if ([Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\') -eq $normalizedDir) {
                    Write-Host "$InstallDir is already on the $scope PATH."
                    return
                }
            }

            $key.SetValue('Path', (($entries + $InstallDir) -join ';'), $kind)
            Write-Host "Added $InstallDir to the $scope PATH."
        } finally {
            $key.Close()
        }

        # Broadcast WM_SETTINGCHANGE so Explorer, and every process it launches from now on, picks up
        # the new PATH without a sign-out. Best-effort: the registry write above already succeeded.
        try {
            if (-not ('JarvisRegistryInstaller.NativeMethods' -as [type])) {
                Add-Type -Namespace JarvisRegistryInstaller -Name NativeMethods -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(
    IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
            }
            $HWND_BROADCAST = [IntPtr] 0xffff
            $WM_SETTINGCHANGE = 0x1a
            $SMTO_ABORTIFHUNG = 0x2
            $result = [UIntPtr]::Zero
            [void] [JarvisRegistryInstaller.NativeMethods]::SendMessageTimeout(
                $HWND_BROADCAST, $WM_SETTINGCHANGE, [UIntPtr]::Zero, 'Environment',
                $SMTO_ABORTIFHUNG, 5000, [ref] $result)
        } catch {
            Write-Warning "could not broadcast the PATH change; sign out and back in if new terminals don't see it: $($_.Exception.Message)"
        }
        Write-Host 'Open a new terminal to use jarvis-registry from PATH.'
    }

    # AppLocker or WDAC script enforcement runs `irm | iex` in ConstrainedLanguage mode, which blocks
    # Add-Type and most .NET calls this script makes. Check first so the user gets an actionable
    # message instead of whichever "not permitted in this language mode" error happens to hit first.
    $languageMode = $ExecutionContext.SessionState.LanguageMode
    if ($languageMode -ne 'FullLanguage') {
        Fail ("PowerShell is running in $languageMode mode, usually because AppLocker or WDAC" +
            ' enforces script rules on this machine, and this installer needs FullLanguage mode.' +
            ' Install with winget or manually from' +
            ' https://github.com/ascending-llc/jarvis-registry-cli/releases instead.')
    }

    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        Fail 'this installer supports Windows only; see docs/setup.md for macOS and Linux'
    }

    # TLS 1.2 is off by default in Windows PowerShell 5.1 on older .NET Framework builds, and GitHub
    # requires it.
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $arch = Resolve-Arch
    $isElevated = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator)

    if ($env:JARVIS_REGISTRY_INSTALL_DIR) {
        $installDir = [IO.Path]::GetFullPath($env:JARVIS_REGISTRY_INSTALL_DIR)
    } elseif ($isElevated) {
        # In a 32-bit host, ProgramFiles is "Program Files (x86)"; ProgramW6432 is the native one.
        $programFiles = if ($env:ProgramW6432) { $env:ProgramW6432 } else { $env:ProgramFiles }
        $installDir = Join-Path $programFiles 'jarvis-registry'
    } else {
        $installDir = Join-Path $env:LOCALAPPDATA 'Programs\jarvis-registry'
    }

    $tag = Resolve-Tag
    $version = $tag.Substring(1)
    $archive = "jarvis-registry_${version}_windows_${arch}.zip"
    $releaseUrl = "$ReleaseBase/$tag"

    $workDir = Join-Path ([IO.Path]::GetTempPath()) ('jarvis-registry-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $workDir | Out-Null
    try {
        $archivePath = Join-Path $workDir $archive
        $checksumsPath = Join-Path $workDir 'checksums.txt'
        Save-ReleaseAsset "$releaseUrl/$archive" $archivePath
        Save-ReleaseAsset "$releaseUrl/checksums.txt" $checksumsPath
        Assert-Checksum $archivePath $checksumsPath

        # Verify the signature on a staged copy so nothing reaches the install directory unverified.
        $stagingDir = Join-Path $workDir 'extracted'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $stagingDir
        $stagedExe = Join-Path $stagingDir 'jarvis-registry.exe'
        if (-not (Test-Path -LiteralPath $stagedExe -PathType Leaf)) {
            Fail "release archive is missing jarvis-registry.exe"
        }
        Assert-Signature $stagedExe

        # Install the verified staged copy rather than re-expanding the archive. The archive's
        # completions\ folder comes along unregistered: PowerShell/CMD completion is out of scope.
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        Copy-Item -Path (Join-Path $stagingDir '*') -Destination $installDir -Recurse -Force

        # The staging directory sits under %TEMP%, which the invoking user's non-elevated processes
        # can write to, so the staged exe could be swapped between its check and the copy. Check it
        # again in the install directory, which only administrators can write to on an elevated
        # install, before it is put on PATH or run, and remove it if it fails.
        $installedExe = Join-Path $installDir 'jarvis-registry.exe'
        try {
            Assert-Signature $installedExe
        } catch {
            Remove-Item -LiteralPath $installedExe -Force -ErrorAction SilentlyContinue
            throw
        }
    } finally {
        Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    Add-InstallDirToPath $installDir $isElevated

    & (Join-Path $installDir 'jarvis-registry.exe') --version
}
