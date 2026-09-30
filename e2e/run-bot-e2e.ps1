<#
.SYNOPSIS
  Bot E2E 单一入口（B4-01）：api + browser 双通道，产出 run-summary。

.DESCRIPTION
  通道说明见 e2e/README.md；用例清单见 e2e/cases.md。
  - api     ：纯 Go（无需前端/浏览器），本机可跑；
  - browser ：Playwright（需真实栈：后端 :8090 + vite :3000 + 账号）；
  - all     ：先 api 后 browser。

.PARAMETER Channel
  api | browser | all（默认 all）。

.PARAMETER OutDir
  run-summary 输出目录（默认 docs/plan/evidence/bot-b4）。

.EXAMPLE
  pwsh e2e/run-bot-e2e.ps1 -Channel api
#>
[CmdletBinding()]
param(
  [ValidateSet('api', 'browser', 'all')]
  [string]$Channel = 'all',
  [string]$OutDir = 'docs/plan/evidence/bot-b4'
)

$ErrorActionPreference = 'Continue'

# ── 仓库根定位（脚本位于 <root>/e2e/）────────────────────────────
$RepoRoot = Split-Path -Parent $PSScriptRoot
$Backend = Join-Path $RepoRoot 'itsm-backend'
$Frontend = Join-Path $RepoRoot 'itsm-frontend'
$OutPath = if ([System.IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $RepoRoot $OutDir }
New-Item -ItemType Directory -Force -Path $OutPath | Out-Null

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$summaryFile = Join-Path $OutPath "run-summary-$stamp.md"

# ── 元信息 ───────────────────────────────────────────────────────
Push-Location $RepoRoot
$commit = (git rev-parse --short HEAD) 2>$null
$branch = (git rev-parse --abbrev-ref HEAD) 2>$null
$dirty = if ((git status --porcelain) 2>$null) { 'dirty' } else { 'clean' }
Pop-Location
$goVersion = (go version) 2>$null
$nodeVersion = (node -v) 2>$null

$results = New-Object System.Collections.Generic.List[object]
$failures = New-Object System.Collections.Generic.List[object]

function Invoke-Checked {
  param(
    [string]$Id,
    [string]$Target,
    [string]$Command,
    [string]$WorkDir
  )
  Write-Host "── [$Id] $Target" -ForegroundColor Cyan
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  Push-Location $WorkDir
  $out = & ([scriptblock]::Create($Command)) 2>&1 | Out-String
  $code = $LASTEXITCODE
  Pop-Location
  $sw.Stop()
  $ok = ($code -eq 0)
  $results.Add([pscustomobject]@{
    Id = $Id; Channel = 'api'; Target = $Target; Command = $Command
    Result = if ($ok) { 'ok' } else { 'FAIL' }; Exit = $code; Seconds = [math]::Round($sw.Elapsed.TotalSeconds, 1)
  })
  if (-not $ok) {
    $tail = ($out -split "`n" | Where-Object { $_ -match 'FAIL|Error|panic' } | Select-Object -First 6) -join ' / '
    $failures.Add([pscustomobject]@{ Id = $Id; Type = '失败'; Summary = ($tail -replace '\s+', ' ') })
    Write-Host "   FAIL (exit=$code)" -ForegroundColor Red
  } else {
    Write-Host "   ok ($($results[-1].Seconds)s)" -ForegroundColor Green
  }
  return $out
}

# ── api 通道 ─────────────────────────────────────────────────────
$apiLogs = @{}
if ($Channel -in @('api', 'all')) {
  $apiLogs['A-1'] = Invoke-Checked -Id 'A-1' -Target 'tests/botintegration（B0）' -Command 'go test ./tests/botintegration/ -run TestB0 -count=1 -timeout 20m' -WorkDir $Backend
  $apiLogs['A-2'] = Invoke-Checked -Id 'A-2' -Target 'tests/botintegration（B1）' -Command 'go test ./tests/botintegration/ -run TestB1 -count=1 -timeout 20m' -WorkDir $Backend
  $apiLogs['A-3'] = Invoke-Checked -Id 'A-3' -Target 'tests/botintegration（B2）' -Command 'go test ./tests/botintegration/ -run TestB2 -count=1 -timeout 20m' -WorkDir $Backend
  $apiLogs['A-4'] = Invoke-Checked -Id 'A-4' -Target 'tests/botintegration（B3）' -Command 'go test ./tests/botintegration/ -run TestB3 -count=1 -timeout 20m' -WorkDir $Backend
  $apiLogs['A-5'] = Invoke-Checked -Id 'A-5' -Target 'handlers/ai（SSE 兼容）' -Command "go test ./handlers/ai/ -run 'TestSSERegistry|TestToolEvents' -count=1 -timeout 10m" -WorkDir $Backend
  $apiLogs['A-6'] = Invoke-Checked -Id 'A-6' -Target 'service/bot' -Command 'go test ./service/bot/ -count=1 -timeout 20m' -WorkDir $Backend
}

# ── browser 通道 ─────────────────────────────────────────────────
if ($Channel -in @('browser', 'all')) {
  Write-Host '── [W] Playwright：flow-bot-workspace.spec.ts' -ForegroundColor Cyan
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  Push-Location $Frontend
  $spec = 'tests/e2e/flows/flow-bot-workspace.spec.ts'
  $out = npx playwright test $spec --project=chromium --reporter=list 2>&1 | Out-String
  $code = $LASTEXITCODE
  Pop-Location
  $sw.Stop()
  $passed = ([regex]::Matches($out, '(?m)^\s*\d+ passed')).Count
  $skipped = ([regex]::Matches($out, '(?m)^\s*\d+ skipped')).Count
  $ok = ($code -eq 0)
  $results.Add([pscustomobject]@{
    Id = 'W-1..3'; Channel = 'browser'; Target = 'flow-bot-workspace.spec.ts'
    Command = 'npx playwright test ... --project=chromium'
    Result = if ($ok) { 'passed' } else { 'failed' }; Exit = $code; Seconds = [math]::Round($sw.Elapsed.TotalSeconds, 1)
  })
  if (-not $ok) {
    $tail = ($out -split "`n" | Where-Object { $_ -match 'failed|Error|✘' } | Select-Object -First 6) -join ' / '
    $failures.Add([pscustomobject]@{ Id = 'W-1..3'; Type = '失败'; Summary = ($tail -replace '\s+', ' ') })
    Write-Host "   failed (exit=$code)" -ForegroundColor Red
  } else {
    Write-Host "   passed/skipped (exit=$code)" -ForegroundColor Green
  }
}

# ── 生成 run-summary ─────────────────────────────────────────────
$overall = if ($failures.Count -eq 0) { '通过' } else { '部分通过/失败' }
$lines = New-Object System.Collections.Generic.List[string]
$lines.Add('# Bot E2E run-summary')
$lines.Add('')
$lines.Add('| 项 | 值 |')
$lines.Add('| --- | --- |')
$lines.Add("| 运行时间 | $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss zzz') |")
$lines.Add("| 通道 | $Channel |")
$lines.Add("| 提交 | $commit（分支 $branch；工作树 $dirty） |")
$lines.Add("| 环境 | $goVersion；Node $nodeVersion；OS $([System.Environment]::OSVersion.VersionString) |")
$lines.Add("| 结论 | **$overall** |")
$lines.Add('')
$lines.Add('## 1. 结果总表')
$lines.Add('')
$lines.Add('| # | 通道 | 目标 | 命令 | 结果 | 耗时(s) |')
$lines.Add('| --- | --- | --- | --- | --- | --- |')
foreach ($r in $results) {
  $lines.Add("| $($r.Id) | $($r.Channel) | $($r.Target) | $($r.Command) | $($r.Result) | $($r.Seconds) |")
}
$lines.Add('')
$lines.Add('## 2. 失败与 skip 明细')
$lines.Add('')
if ($failures.Count -eq 0) {
  $lines.Add('（无）')
} else {
  $lines.Add('| 项 | 类型 | 摘要 |')
  $lines.Add('| --- | --- | --- |')
  foreach ($f in $failures) { $lines.Add("| $($f.Id) | $($f.Type) | $($f.Summary) |") }
}
$lines.Add('')
$lines.Add('## 3. 原始日志（尾行）')
$lines.Add('')
foreach ($k in $apiLogs.Keys) {
  $tail = (($apiLogs[$k] -split "`n") | Where-Object { $_.Trim() } | Select-Object -Last 3) -join ' / '
  $lines.Add("- **$k**：$($tail -replace '\s+', ' ')")
}
$lines.Add('')
$lines.Add('> 模板见 `e2e/run-summary.template.md`；章程见 `e2e/README.md`。')

Set-Content -Path $summaryFile -Value ($lines -join "`n") -Encoding UTF8
Write-Host ""
Write-Host "run-summary: $summaryFile" -ForegroundColor Yellow
if ($failures.Count -gt 0) { exit 1 } else { exit 0 }
