# skills 打包脚本（P1 仓库即分发）
# 产出 dist/skills/：
#   - <skill>.zip      单技能包（zip 内根即技能目录，含内嵌 _lib/itsm_common.py）
#   - itsm-skills-all.zip  全量 bundle
#   - manifest.json    清单（name/version/description/sha256/sizeBytes/updatedAt/sourceCommit）
#   - itsm-skills-install.py  零依赖安装器（从 scripts/ 拷入 dist 一起分发）
# 幂等：重复运行覆盖重建。示例口令脱敏（Demo@2026! → YOUR_PASSWORD）仅作用于包内文件，不改动仓库源。
param(
    [string]$RepoRoot = (Resolve-Path "$PSScriptRoot\..\..").Path,
    [string]$OutDir = ""
)

$ErrorActionPreference = 'Stop'
$skillsRoot = Join-Path $RepoRoot '.agents\skills'
if (-not $OutDir) { $OutDir = Join-Path $RepoRoot 'dist\skills' }

if (-not (Test-Path (Join-Path $skillsRoot '_lib\itsm_common.py'))) {
    throw "共享底座缺失：$skillsRoot\_lib\itsm_common.py（布局约束见 .agents/skills/README.md §6.1）"
}

# 1) 目标技能 = 含 SKILL.md 的目录（排除 _lib 与 dist）
$skillDirs = Get-ChildItem -Directory $skillsRoot | Where-Object {
    (Test-Path (Join-Path $_.FullName 'SKILL.md')) -and ($_.Name -ne 'dist')
}
if (-not $skillDirs) { throw "未发现任何技能目录（含 SKILL.md）" }

# 2) 干净输出目录
if (Test-Path $OutDir) { Remove-Item -Recurse -Force $OutDir }
New-Item -ItemType Directory -Force $OutDir | Out-Null
$staging = Join-Path $OutDir '.staging'
New-Item -ItemType Directory -Force $staging | Out-Null

# 3) 版本锚点：源 commit + UTC 时间
$sourceCommit = ''
try { $sourceCommit = (git -C $RepoRoot rev-parse HEAD).Trim() } catch { $sourceCommit = 'unknown' }
$packagedAt = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')

function Get-SkillVersion {
    # 版本 = SKILL.md 首个 frontmatter 后加入的生成戳 + 源 commit 短码；用 git log -1 --format=%cd 取最近触及时间
    param([string]$SkillPath)
    $short = if ($sourceCommit -ne 'unknown') { $sourceCommit.Substring(0, 8) } else { 'dev' }
    $d = git -C $RepoRoot log -1 --format=%cd --date=short -- "$SkillPath" 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $d) { $d = $packagedAt.Substring(0, 10) }
    return "$d.$short"
}

function Sanitize-File {
    # 脱敏：demo 口令 → 占位符（仅作用于待打包的临时副本）
    param([string]$Path)
    $text = Get-Content -Raw -Encoding UTF8 $Path
    $text = $text -replace [regex]::Escape('Demo@2026!'), 'YOUR_PASSWORD'
    if ($text -ne (Get-Content -Raw -Encoding UTF8 $Path)) {
        Set-Content -Path $Path -Value $text -Encoding UTF8 -NoNewline
    }
}

$manifest = [ordered]@{
    schemaVersion = 1
    generatedAt   = $packagedAt
    sourceCommit  = $sourceCommit
    sharedLib     = '_lib/itsm_common.py'
    hostRoots     = [ordered]@{
        claude  = '~/.claude/skills'
        codex   = '~/.aicli/.agents/skills'
        project = '.agents/skills'
    }
    skills        = @()
}

# 4) 逐技能打包（zip 根 = 技能目录内容 + 内嵌 _lib/）
foreach ($dir in $skillDirs) {
    $name = $dir.Name
    $stage = Join-Path $staging $name
    New-Item -ItemType Directory -Force $stage | Out-Null

    # 拷贝技能目录（排除 __pycache__/.token 类垃圾）
    Copy-Item -Path (Join-Path $dir.FullName '*') -Destination $stage -Recurse -Force
    Get-ChildItem -Path $stage -Recurse -Directory -Filter '__pycache__' | Remove-Item -Recurse -Force

    # 内嵌共享底座（布局约束：skills 根/<skill>/_lib/itsm_common.py，脚本 parents[2] 兼容）
    $libTarget = Join-Path $stage '_lib'
    New-Item -ItemType Directory -Force $libTarget | Out-Null
    Copy-Item (Join-Path $skillsRoot '_lib\itsm_common.py') $libTarget -Force

    # 脱敏（SKILL.md / references / scripts 全扫）
    Get-ChildItem $stage -Recurse -File -Include *.md, *.py | ForEach-Object { Sanitize-File $_.FullName }

    # zip：压缩包根直接是技能目录内容
    $zipPath = Join-Path $OutDir "$name.zip"
    if (Test-Path $zipPath) { Remove-Item -Force $zipPath }
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zipPath -CompressionLevel Optimal

    $hash = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
    $manifest.skills += [ordered]@{
        name        = $name
        version     = Get-SkillVersion $dir.FullName
        description = ''
        file        = "$name.zip"
        sha256      = $hash
        sizeBytes   = (Get-Item $zipPath).Length
        updatedAt   = $packagedAt
    }
}

# 5) 全量 bundle（skills 根布局：各技能目录 + _lib，解压即用）
$bundleStage = Join-Path $staging 'itsm-skills-all'
New-Item -ItemType Directory -Force $bundleStage | Out-Null
foreach ($dir in $skillDirs) { Copy-Item $dir.FullName (Join-Path $bundleStage $dir.Name) -Recurse -Force }
Copy-Item (Join-Path $skillsRoot '_lib') (Join-Path $bundleStage '_lib') -Recurse -Force
Get-ChildItem $bundleStage -Recurse -Directory -Filter '__pycache__' | Remove-Item -Recurse -Force
Get-ChildItem $bundleStage -Recurse -File -Include *.md, *.py | ForEach-Object { Sanitize-File $_.FullName }
# README（治理文档）随 bundle 分发
Copy-Item (Join-Path $skillsRoot 'README.md') $bundleStage -Force
$bundleZip = Join-Path $OutDir 'itsm-skills-all.zip'
Compress-Archive -Path (Join-Path $bundleStage '*') -DestinationPath $bundleZip -Force
$manifest.bundle = [ordered]@{
    file      = 'itsm-skills-all.zip'
    sha256    = (Get-FileHash $bundleZip -Algorithm SHA256).Hash.ToLower()
    sizeBytes = (Get-Item $bundleZip).Length
}

# 6) 安装器随包分发
Copy-Item (Join-Path $PSScriptRoot 'itsm-skills-install.py') $OutDir -Force

# 7) 写 manifest（从 SKILL.md frontmatter 补 description）
foreach ($entry in $manifest.skills) {
    $skillMd = Join-Path $skillsRoot (Join-Path $entry.name 'SKILL.md')
    if (Test-Path $skillMd) {
        $head = (Get-Content $skillMd -TotalCount 8 -Encoding UTF8) -join "`n"
        $m = [regex]::Match($head, '(?m)^description:\s*(.+)$')
        if ($m.Success) { $entry.description = $m.Groups[1].Value.Trim() }
    }
}
$manifest | ConvertTo-Json -Depth 6 | Set-Content (Join-Path $OutDir 'manifest.json') -Encoding UTF8

# 8) 清理 staging
Remove-Item -Recurse -Force $staging

Write-Output "packed -> $OutDir"
Get-ChildItem $OutDir | Select-Object Name, Length | Format-Table -AutoSize
