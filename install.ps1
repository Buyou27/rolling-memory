# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>
#
# install.ps1 — One-line installer for Rolling-Memory on Windows (PowerShell).
# 【兼容版】适配 PowerShell 5.1 及更低版本（PSVersion < 7）
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File 兼容版.ps1 [--with-hooks] [--with-instructions]
#   irm https://github.com/rocareer/rolling-memory/raw/main/install.ps1 | iex
$ErrorActionPreference = "Stop"
# [兼容] PS5.1 的 Invoke-WebRequest 进度条会显著拖慢大文件下载，关闭进度显示
$ProgressPreference = "SilentlyContinue"
# [兼容] PS5.1 与旧版 .NET 默认可能只启用 TLS1.0/1.1，显式启用 TLS1.2 以访问 GitHub
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}
catch {
}
$DefaultRepoUrl = "https://github.com/rocareer/rolling-memory.git"
$DefaultReleaseBase = "https://github.com/rocareer/rolling-memory/releases/latest/download"
# No default mirror: GitHub is the single public source for this project
# (see the project policy on external terms). Inject a mirror explicitly with
# ROLLING_MEMORY_RELEASE_MIRROR=<base>/releases/latest/download if you run one.
$DefaultReleaseMirror = ""
if ($env:ROLLING_MEMORY_REPO_URL) {
    $RepoUrl = $env:ROLLING_MEMORY_REPO_URL
} else {
    $RepoUrl = $DefaultRepoUrl
}
# Same rule for the source fallback: no third-party default mirror.
if ($env:ROLLING_MEMORY_REPO_MIRROR) {
    $RepoMirrorUrl = $env:ROLLING_MEMORY_REPO_MIRROR
} else {
    $RepoMirrorUrl = ""
}
$ReleaseBaseExplicit = $false
if (Test-Path Env:ROLLING_MEMORY_RELEASE_BASE) {
    $ReleaseBase = $env:ROLLING_MEMORY_RELEASE_BASE
    if ($ReleaseBase -ne "") {
        $ReleaseBaseExplicit = $true
    }
} else {
    $ReleaseBase = $DefaultReleaseBase
}
if (@("off", "none", "0") -contains $ReleaseBase) {
    $ReleaseBase = ""
}
if (Test-Path Env:ROLLING_MEMORY_RELEASE_MIRROR) {
    $ReleaseMirror = $env:ROLLING_MEMORY_RELEASE_MIRROR
} else {
    $ReleaseMirror = $DefaultReleaseMirror
}
if (@("off", "none", "0") -contains $ReleaseMirror) {
    $ReleaseMirror = ""
}
if ($env:ROLLING_MEMORY_INSTALL_DIR) {
    $InstallDir = $env:ROLLING_MEMORY_INSTALL_DIR
} else {
    $InstallDir = Join-Path $env:LOCALAPPDATA "rolling-memory\bin"
}
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$TargetBin = Join-Path $InstallDir "rolling-memory.exe"
if ($env:ROLLING_MEMORY_SOURCE_REF) {
    $SourceRef = $env:ROLLING_MEMORY_SOURCE_REF
} else {
    $SourceRef = ""
}
$HarnessArgs = @()
for ($i = 0; $i -lt $args.Count; $i++) {
    if ($args[$i] -eq "--ref") {
        if ($i + 1 -ge $args.Count) {
            throw "install.ps1: --ref requires a tag"
        }
        $SourceRef = $args[$i + 1]
        $i++
    }
    elseif ($args[$i] -like "--ref=*") {
        $SourceRef = $args[$i].Substring(6)
    }
    else {
        $HarnessArgs += $args[$i]
    }
}
if ($SourceRef -and $SourceRef.StartsWith("-")) {
    throw "install.ps1: invalid --ref value: $SourceRef"
}
if ($SourceRef) {
    $ReleaseBase = ""
}
# Map the host architecture to the release asset suffix.
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "install.ps1: unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$Built = $false
$PrebuiltErr = ""
$PrebuiltVersion = ""
$PrebuiltAsset = ""
$InstallMethod = ""
$InstallRef = ""
$InstallCommit = ""
$InstallAsset = ""
if ($env:XDG_DATA_HOME) {
    $DataDir = Join-Path $env:XDG_DATA_HOME "rolling-memory"
}
elseif ($env:USERPROFILE) {
    $DataDir = Join-Path $env:USERPROFILE ".local\share\rolling-memory"
}
else {
    $DataDir = Join-Path $env:LOCALAPPDATA "rolling-memory"
}
$EvalDataTarget = Join-Path (Split-Path -Parent $InstallDir) "evaldata"
if ((-not $SourceRef) -and (Test-Path "cmd\rolling-memory\main.go") -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[rolling-memory] Building from local checkout..."
    $env:CGO_ENABLED = "0"
    go build -trimpath -o $TargetBin .\cmd\rolling-memory
    $InstallMethod = "local"
    $InstallRef = (git describe --tags --always 2>$null)
    $InstallCommit = (git rev-parse HEAD 2>$null)
    if ((Test-Path "evaldata\MANIFEST.json") -and (-not (Test-Path (Join-Path $EvalDataTarget "MANIFEST.json")))) {
        Copy-Item -Recurse -Force "evaldata" $EvalDataTarget -ErrorAction SilentlyContinue
    }
    $Built = $true
}
function Try-DownloadPrebuilt([string]$BaseUrl) {
    $trimmedBase = $BaseUrl.TrimEnd('/')
    $SumsUrl = "$trimmedBase/SHA256SUMS"
    $Sums = $null
    try {
        $resp = Invoke-WebRequest -UseBasicParsing -TimeoutSec 20 -Uri $SumsUrl
        $rawContent = $resp.Content
        if ($rawContent -is [byte[]]) {
            $Sums = [System.Text.Encoding]::UTF8.GetString($rawContent)
        }
        else {
            $Sums = [string]$rawContent
        }
    }
    catch {
        $script:PrebuiltErr = "无法连接 $SumsUrl"
        if ($ReleaseBaseExplicit) {
            throw "无法获取 $SumsUrl 的 SHA256SUMS，拒绝安装未校验的二进制。"
        }
        return $false
    }
    if (-not $Sums) {
        return $false
    }
    # Prefer native architecture asset; on ARM64 allow fallback to windows-amd64.exe
    # (Windows 11 ARM runs x64 binaries natively) if the mirror only carries amd64.
    $CandidateAssets = @("rolling-memory-windows-$Arch.exe")
    if ($Arch -eq "arm64" -and (-not $ReleaseBaseExplicit)) {
        $CandidateAssets += "rolling-memory-windows-amd64.exe"
    }
    $SelectedAsset = $null
    $Want = $null
    $lines = $Sums -split "`r?`n"
    foreach ($Cand in $CandidateAssets) {
        foreach ($Line in $lines) {
            $trimLine = $Line.Trim()
            if ([string]::IsNullOrEmpty($trimLine)) { continue }
            $Fields = $trimLine -split '\s+'
            if ($Fields.Count -ge 2) {
                $fname = $Fields[1].TrimStart('*')
                if ($fname -eq $Cand) {
                    $SelectedAsset = $Cand
                    $Want = $Fields[0].ToLower()
                    break
                }
            }
        }
        if ($Want) { break }
    }
    if (-not $Want) {
        $script:PrebuiltErr = "SHA256SUMS 中缺少 $($CandidateAssets[0]) 的校验和"
        if ($ReleaseBaseExplicit) {
            throw "$($script:PrebuiltErr)，拒绝安装未校验的二进制。"
        }
        return $false
    }
    $script:PrebuiltAsset = $SelectedAsset
    $AssetUrl = "$trimmedBase/$SelectedAsset"
    Write-Host "[rolling-memory] Downloading prebuilt binary ($SelectedAsset)..."
    $TmpBin = "$TargetBin.tmp.$PID"
    try {
        Invoke-WebRequest -UseBasicParsing -TimeoutSec 120 -Uri $AssetUrl -OutFile $TmpBin
        $fileHashObj = Get-FileHash -Algorithm SHA256 -Path $TmpBin
        $Got = $fileHashObj.Hash.ToLower()
        if ($Got -ne $Want) {
            Remove-Item -Force -ErrorAction SilentlyContinue $TmpBin
            throw "SHA256 校验失败：期望 $Want，实得 $Got。"
        }
        Move-Item -Force -Path $TmpBin -Destination $TargetBin
        $script:PrebuiltVersion = ""
        try {
            $verResp = Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Uri "$trimmedBase/VERSION"
            $verRaw = $verResp.Content
            if ($verRaw -is [byte[]]) {
                $VersionText = [System.Text.Encoding]::UTF8.GetString($verRaw)
            }
            else {
                $VersionText = [string]$verRaw
            }
            $script:PrebuiltVersion = $VersionText.Trim()
        }
        catch {
            $script:PrebuiltVersion = ""
        }
        Write-Host "[rolling-memory] SHA256 verified ($Got)"
        return $true
    }
    catch {
        Remove-Item -Force -ErrorAction SilentlyContinue $TmpBin
        $script:PrebuiltErr = $_.Exception.Message
        if ($ReleaseBaseExplicit -or ($_.Exception.Message -match "SHA256")) {
            throw
        }
        return $false
    }
}
if ((-not $Built) -and ($ReleaseBase -ne "")) {
    if (Try-DownloadPrebuilt $ReleaseBase) {
        $Built = $true
        $InstallMethod = "release"
        $InstallAsset = $PrebuiltAsset
        if ($PrebuiltVersion) {
            $InstallRef = $PrebuiltVersion
        }
        else {
            $InstallRef = "unknown"
        }
    }
    elseif ((-not $ReleaseBaseExplicit) -and ($ReleaseMirror -ne "") -and ($ReleaseMirror -ne $ReleaseBase)) {
        Write-Host "[rolling-memory] Primary release channel unavailable; trying mirror ($ReleaseMirror)..."
        if (Try-DownloadPrebuilt $ReleaseMirror) {
            $Built = $true
            $InstallMethod = "release"
            $InstallAsset = $PrebuiltAsset
            if ($PrebuiltVersion) {
                $InstallRef = $PrebuiltVersion
            }
            else {
                $InstallRef = "unknown"
            }
        }
    }
    if (-not $Built) {
        Write-Host "[rolling-memory] Prebuilt channel unavailable ($PrebuiltErr)."
    }
}
if ((-not $Built) -and (Get-Command go -ErrorAction SilentlyContinue) -and (Get-Command git -ErrorAction SilentlyContinue)) {
    $TmpRepo = Join-Path ([System.IO.Path]::GetTempPath()) ("rm-src-" + [System.Guid]::NewGuid().ToString("N"))
    try {
        Write-Host "[rolling-memory] Cloning $RepoUrl and building..."
        if ($SourceRef) {
            $CloneArgs = @("--depth", "1", "--branch", $SourceRef)
        }
        else {
            $CloneArgs = @("--depth", "1")
        }
        git clone @CloneArgs $RepoUrl $TmpRepo
        if ($LASTEXITCODE -ne 0 -and $RepoMirrorUrl -and ($RepoUrl -eq $DefaultRepoUrl)) {
            Write-Host "[rolling-memory] Primary source unavailable; retrying mirror $RepoMirrorUrl..."
            Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $TmpRepo
            git clone @CloneArgs $RepoMirrorUrl $TmpRepo
        }
        if ($LASTEXITCODE -ne 0) {
            throw "install.ps1: failed to clone $RepoUrl"
        }
        Push-Location $TmpRepo
        try {
            $env:CGO_ENABLED = "0"
            go build -trimpath -o $TargetBin .\cmd\rolling-memory
            if ($LASTEXITCODE -ne 0) {
                throw "go build failed"
            }
            $InstallMethod = "source"
            if ($SourceRef) {
                $InstallRef = $SourceRef
            }
            else {
                $InstallRef = (git describe --tags --always 2>$null)
            }
            $InstallCommit = (git rev-parse HEAD 2>$null)
            if ((Test-Path "evaldata\MANIFEST.json") -and (-not (Test-Path (Join-Path $EvalDataTarget "MANIFEST.json")))) {
                Copy-Item -Recurse -Force "evaldata" $EvalDataTarget -ErrorAction SilentlyContinue
            }
            $Built = $true
        }
        finally {
            Pop-Location
        }
    }
    finally {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $TmpRepo
    }
}
if (-not $Built) {
    throw "无法获取预编译二进制（原因：$PrebuiltErr），且本机未安装 Git / Go 编译环境。普通用户请检查网络或设置 `$env:ROLLING_MEMORY_RELEASE_BASE='https://your-mirror/releases/latest/download' 后重试；开发者可安装 Go 1.26+ 与 Git 从源码构建。"
}
# Short alias `rmem` (equivalent to the POSIX symlink pair created by install.sh).
$RmemCmd = Join-Path $InstallDir "rmem.cmd"
Set-Content -Path $RmemCmd -Encoding ASCII -Value "@echo off`r`n`"%~dp0rolling-memory.exe`" %*"
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
if ($InstallMethod) {
    $MetaMethod = $InstallMethod
}
else {
    $MetaMethod = "unknown"
}
if ($InstallRef) {
    $MetaRef = $InstallRef
}
else {
    $MetaRef = "unknown"
}
if ($InstallCommit) {
    $MetaCommit = $InstallCommit
}
else {
    $MetaCommit = ""
}
if ($InstallAsset) {
    $MetaAsset = $InstallAsset
}
else {
    $MetaAsset = ""
}
$InstallMeta = @{
    method       = $MetaMethod
    repo         = $RepoUrl
    ref          = $MetaRef
    commit       = $MetaCommit
    asset        = $MetaAsset
    installed_at = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
# [兼容] PS5.1 的 ConvertTo-Json 默认 Depth=2，显式指定 Depth 避免嵌套对象被截断
$InstallMetaJson = $InstallMeta | ConvertTo-Json -Compress -Depth 10
$metaFile = Join-Path $DataDir "install.json"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($metaFile, $InstallMetaJson, $utf8NoBom)

Write-Host "[rolling-memory] Installed binary to $TargetBin (alias: $RmemCmd)"
$pathParts = $env:PATH -split ';'
if ($pathParts -notcontains $InstallDir) {
    $env:PATH = "$InstallDir;$env:PATH"
    if ($env:ROLLING_MEMORY_NO_MODIFY_PATH -ne "1") {
        try {
            $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
            if ($UserPath) {
                $Parts = $UserPath -split ';'
            }
            else {
                $Parts = @()
            }
            if ($Parts -notcontains $InstallDir) {
                if ($UserPath) {
                    $NewUserPath = "$UserPath;$InstallDir"
                }
                else {
                    $NewUserPath = $InstallDir
                }
                [Environment]::SetEnvironmentVariable("Path", $NewUserPath, "User")
                Write-Host "[rolling-memory] Added $InstallDir to your Windows user PATH."
            }
        }
        catch {
            Write-Host "[rolling-memory] Add to PATH if needed: `$env:Path += `";$InstallDir`""
        }
    }
}
try {
    & $TargetBin init --write-config *> $null
}
catch {
}
if ($HarnessArgs.Count -eq 0) {
    $InstallArgs = @("install", "--with-hooks")
}
else {
    $InstallArgs = @("install") + $HarnessArgs
}
$PrevEAP = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$InstallOut = & $TargetBin @InstallArgs 2>&1
$InstallRc = $LASTEXITCODE
$ErrorActionPreference = $PrevEAP
if ($InstallOut) {
    foreach ($o in $InstallOut) {
        Write-Host $o
    }
}
if ($InstallRc -eq 0) {
    & $TargetBin install --check
}
elseif (($InstallOut -join "`n") -match "no harness detected") {
    Write-Host "[rolling-memory] CLI binary and local vault are installed and ready (no host harness detected yet)."
    Write-Host "[rolling-memory] Wire a harness anytime with: rolling-memory install --with-hooks [<harness-id>]"
}
else {
    exit $InstallRc
}
Write-Host "[rolling-memory] Web console: run `rolling-memory panel` to start it (backend :9006 + web frontend :8008; add --desktop-shortcut to install for a desktop launcher)."
Write-Host "[rolling-memory] Self-update: run `rolling-memory update` (or `rolling-memory update --check`) anytime to update the CLI, console and host integrations."
