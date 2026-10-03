# RLS shadow 观察流量（联调环境，只读 GET；2026-10-03）
#
# 用途：RLS_MODE=shadow 下用三类身份（超管 / 低权 MSP / 客户管理员）驱动真实读路径，
#       供 database/rls 的缺租户告警归因（详见 docs/multi-tenant/plan/rls-shadow-observation-2026-10-03.md）。
# 前置：实例以 RLS_MODE=shadow（建议 LOG_LEVEL=debug）启动。
# 账号：联调环境由 scripts/msp/setup-msp-tenants.sh 建立（默认口令可在本脚本参数覆盖）。
param(
    [string]$BaseUrl = 'http://127.0.0.1:8090/api/v1',
    [string]$AdminUser = 'admin',
    [string]$AdminPassword = 'passw0rd',
    [string]$AgentUser = 'mspagent',
    [string]$AgentPassword = 'Msp@2026Staff!',
    [string]$CustomerUser = 'custa_admin',
    [string]$CustomerPassword = 'Cust@2026User!'
)

$ErrorActionPreference = 'Continue'

function LoginSession([string]$user, [string]$password) {
    $s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    try {
        $r = Invoke-WebRequest -Uri "$BaseUrl/auth/login" -Method Post -ContentType 'application/json' `
            -Body (@{ username = $user; password = $password } | ConvertTo-Json) -WebSession $s -TimeoutSec 20 -SkipHttpErrorCheck
        if ($r.StatusCode -eq 200) { return $s }
        Write-Output "  login($user) -> $($r.StatusCode)"
    } catch {
        Write-Output "  login($user) -> ERR $($_.Exception.Message)"
    }
    return $null
}

function Hit($session, [string]$method, [string]$path) {
    if (-not $session) { Write-Output "  $method $path -> SKIP(no session)"; return }
    try {
        $r = Invoke-WebRequest -Uri "$BaseUrl$path" -Method $method -WebSession $session -TimeoutSec 30 -SkipHttpErrorCheck
        Write-Output "  $method $path -> $($r.StatusCode)"
    } catch {
        Write-Output "  $method $path -> ERR $($_.Exception.Message)"
    }
}

$admin = LoginSession $AdminUser $AdminPassword
$agent = LoginSession $AgentUser $AgentPassword
$customer = LoginSession $CustomerUser $CustomerPassword
Write-Output "sessions: admin=$([bool]$admin) agent=$([bool]$agent) customer=$([bool]$customer)"

Write-Output '== admin（平台面） =='
Hit $admin GET '/auth/me'
Hit $admin GET '/auth/menus'
Hit $admin GET '/auth/tenants'
Hit $admin GET '/admin/tenants'
Hit $admin GET '/users?page=1&pageSize=5'
Hit $admin GET '/tickets/stats'
Hit $admin GET '/tickets?page=1&pageSize=5'
Hit $admin GET '/dashboard/stats'
Hit $admin GET '/msp/status'

Write-Output '== msp agent（低权；需 DEPLOYMENT_MODE=saas_msp） =='
Hit $agent GET '/auth/me'
Hit $agent GET '/auth/menus'
Hit $agent GET '/msp/status'
Hit $agent GET '/msp/context'
Hit $agent GET '/msp/customers'
Hit $agent GET '/msp/allocations'
Hit $agent GET '/msp/workbench/tickets?page=1&pageSize=5'
Hit $agent GET '/msp/workbench/summary'
Hit $agent GET '/msp/workbench/views'
Hit $agent GET '/msp/reports/customers'
Hit $agent GET '/tickets/stats'

Write-Output '== customer admin =='
Hit $customer GET '/auth/me'
Hit $customer GET '/tickets/stats'
Hit $customer GET '/tickets?page=1&pageSize=5'
Hit $customer GET '/msp/status'
Write-Output 'TRAFFIC_DONE'
