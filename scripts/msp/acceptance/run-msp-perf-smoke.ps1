#!/usr/bin/env pwsh
# =============================================================================
# MSP 性能冒烟基线（OB-8，FDB-B1 后续覆盖补强）
#
# 目标（本地 dev 环境，非生产压测；仅建立可重复基线）：
#   L1 登录延迟（POST /api/v1/auth/login，独立会话 ×20）
#   L2 工作台工单列表（GET /api/v1/msp/workbench/tickets?limit=50 ×30）
#   L3 跨租户单据读面（GET /api/v1/msp/workbench/changes?limit=50 ×20）
#   L4 批量护栏拒绝路径（POST /api/v1/msp/workbench/batch，101 条 → 400，无写副作用 ×5）
#
# 前置：saas_msp 实例；MSP 凭据默认读 $env:TEMP\fdb-creds.json（prov.agent1）。
# 产物：docs/multi-tenant/evidence/perf-smoke/run-summary-<ts>.md（min/p50/p95/max，毫秒）
# 退出码：0；2=前置缺失
# =============================================================================
param(
    [string]$Base = "http://127.0.0.1:8090",
    [string]$MspUser = "prov.agent1", [string]$MspPass = "",
    [int]$ValidTicketId = 5, [int]$TenantId = 4,
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\perf-smoke" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$SummaryPath = Join-Path $OutDir "run-summary-$ts.md"

$credsFile = Join-Path $env:TEMP "fdb-creds.json"
if ([string]::IsNullOrWhiteSpace($MspPass) -and (Test-Path $credsFile)) {
    $creds = Get-Content $credsFile -Raw | ConvertFrom-Json
    if ($null -ne $creds.'prov.agent1') { $MspPass = [string]$creds.'prov.agent1' }
}
if ([string]::IsNullOrWhiteSpace($MspPass)) { Write-Error "缺少 MSP 员工口令（-MspPass 或 $credsFile 的 prov.agent1 键）"; exit 2 }

function New-Session([string]$user, [string]$pass) {
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $body = @{ username = $user; password = $pass } | ConvertTo-Json
    $resp = Invoke-WebRequest -Uri "$Base/api/v1/auth/login" -Method Post -Body $body -ContentType "application/json" -WebSession $sess -SkipHttpErrorCheck -TimeoutSec 30
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    if ($null -eq $json -or $json.code -ne 0) { return $null }
    $cookie = $sess.Cookies.GetCookies($Base) | Where-Object { $_.Name -eq "access_token" } | Select-Object -First 1
    return [pscustomobject]@{ Token = $cookie.Value }
}
function Measure-Run([string]$label, [int]$iterations, [scriptblock]$action) {
    $durations = New-Object System.Collections.Generic.List[double]
    for ($i = 0; $i -lt $iterations; $i++) {
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        & $action | Out-Null
        $sw.Stop()
        $durations.Add($sw.Elapsed.TotalMilliseconds)
    }
    $sorted = $durations | Sort-Object
    $n = $sorted.Count
    $p50 = $sorted[[Math]::Max(0, [Math]::Ceiling($n * 0.50) - 1)]
    $p95 = $sorted[[Math]::Max(0, [Math]::Ceiling($n * 0.95) - 1)]
    return [pscustomobject]@{
        Label = $label; N = $n
        Min = [math]::Round($sorted[0], 0); P50 = [math]::Round($p50, 0); P95 = [math]::Round($p95, 0); Max = [math]::Round($sorted[$n - 1], 0)
    }
}

$rows = New-Object System.Collections.Generic.List[object]

# L1 登录（独立会话 ×20）
$rows.Add((Measure-Run "L1 登录（独立会话）" 20 {
    $s = New-Session $MspUser $MspPass
    if ($null -eq $s) { throw "login failed" }
}))

# 复用会话的读路径
$msp = New-Session $MspUser $MspPass
if ($null -eq $msp) { Write-Error "MSP 登录失败"; exit 2 }
$auth = @{ Authorization = "Bearer $($msp.Token)" }

$rows.Add((Measure-Run "L2 工作台工单列表（limit=50）" 30 {
    $null = Invoke-WebRequest -Uri "$Base/api/v1/msp/workbench/tickets?limit=50" -Headers $auth -SkipHttpErrorCheck -TimeoutSec 30
}))
$rows.Add((Measure-Run "L3 跨租户单据读面（changes，limit=50）" 20 {
    $null = Invoke-WebRequest -Uri "$Base/api/v1/msp/workbench/changes?limit=50" -Headers $auth -SkipHttpErrorCheck -TimeoutSec 30
}))

# L4 批量护栏拒绝路径（101 条 → 400，无写副作用）
$items101 = @(); for ($i = 0; $i -lt 101; $i++) { $items101 += @{ ticketId = $ValidTicketId; customerTenantId = $TenantId } }
$batchBody = @{ action = "reply"; items = $items101; payload = @{ content = "perf-smoke" } } | ConvertTo-Json -Depth 6
$rows.Add((Measure-Run "L4 批量超限拒绝路径（101 条 → 400）" 5 {
    $null = Invoke-WebRequest -Uri "$Base/api/v1/msp/workbench/batch" -Method Post -Headers $auth -Body $batchBody -ContentType "application/json" -SkipHttpErrorCheck -TimeoutSec 60
}))

$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("# MSP 性能冒烟基线 run-summary（OB-8）")
$lines.Add("")
$lines.Add("- 时间：$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')｜环境：$Base（本地 dev；saas_msp；经 Vite 无关的直连后端）")
$lines.Add("- 说明：**非生产压测**，仅建立可重复基线（单进程串行请求、本机回环）；生产容量评估需独立批次。")
$lines.Add("")
$lines.Add("| 目标 | N | min (ms) | p50 (ms) | p95 (ms) | max (ms) |")
$lines.Add("|---|---|---|---|---|---|")
foreach ($r in $rows) { $lines.Add("| $($r.Label) | $($r.N) | $($r.Min) | $($r.P50) | $($r.P95) | $($r.Max) |") }
Set-Content -Path $SummaryPath -Value ($lines -join "`n") -Encoding utf8

Write-Host "run-summary: $SummaryPath"
foreach ($r in $rows) { Write-Host ("{0}: n={1} p50={2}ms p95={3}ms max={4}ms" -f $r.Label, $r.N, $r.P50, $r.P95, $r.Max) }
exit 0
