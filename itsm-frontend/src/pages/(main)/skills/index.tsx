/**
 * 资源中心页（P2）：ITSM Skills 分发与安装指导
 *
 * 数据源：GET /api/v1/skills/registry（P1 打包产物 dist/skills/manifest.json 的直通镜像）。
 * 未配置（SKILLS_DIST_DIR 未挂载）→ 降级提示，不报错。
 *
 * 页面结构：
 * - 顶部：分发说明 + manifest 元信息（sourceCommit / generatedAt / bundle 校验和）
 * - 宿主 Tab：Claude Code / Codex (aicli) / 项目级 —— 每个技能一张卡片，
 *   含版本、sha256、大小、下载链接与「复制安装命令」
 * - 安装指导：分宿主展示三步命令（下载 manifest → 跑安装器 → 验证）
 */

import { useEffect, useMemo, useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/Tabs';
import { httpClient } from '@/lib/api/http-client';
import { useMessage } from '@/hooks/useMessage';
import { Boxes, Copy, Download, FileCheck2, RefreshCw, Terminal } from 'lucide-react';

interface SkillEntry {
  name: string;
  version: string;
  description?: string;
  file: string;
  sha256: string;
  sizeBytes: number;
  updatedAt?: string;
}

interface SkillsManifest {
  configured?: boolean;
  reason?: string;
  schemaVersion?: number;
  generatedAt?: string;
  sourceCommit?: string;
  skills?: SkillEntry[];
  bundle?: { file: string; sha256: string; sizeBytes: number };
}

type HostKey = 'claude' | 'codex' | 'project';

const HOST_META: Record<HostKey, { label: string; root: string; note: string }> = {
  claude: { label: 'Claude Code', root: '~/.claude/skills', note: '安装到 Claude Code 全局技能目录' },
  codex: { label: 'Codex / aicli', root: '~/.aicli/.agents/skills', note: '安装到 aicli 外部技能目录' },
  project: { label: '项目级', root: '<repo>/.agents/skills', note: '随仓库分发给团队' },
};

function fmtSize(bytes?: number): string {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

const SkillsResourcesPage = () => {
  const [manifest, setManifest] = useState<SkillsManifest | null>(null);
  const [loading, setLoading] = useState(true);
  const [host, setHost] = useState<HostKey>('codex');
  const messageApi = useMessage();

  const load = async () => {
    setLoading(true);
    try {
      const data = await httpClient.get<SkillsManifest>('/api/v1/skills/registry');
      setManifest(data);
    } catch {
      setManifest({ configured: false, reason: '接口不可用' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const skills = manifest?.skills ?? [];

  const installCmd = useMemo(() => {
    const base = `${window.location.origin}/api/v1/skills`;
    if (host === 'project') {
      return [
        `# 1) 下载清单与安装器`,
        `curl -fsSL ${base}/registry -o manifest.json`,
        `curl -fsSL ${base}/installer -o itsm-skills-install.py`,
        `# 2) 安装全部技能到当前仓库 .agents/skills/`,
        `python itsm-skills-install.py --manifest manifest.json --all --host project`,
        `# 3) 验证（示例）`,
        `python .agents/skills/itsm-ticket-operations/scripts/itsm_ticket.py --help`,
      ].join('\n');
    }
    return [
      `# 1) 下载清单与安装器`,
      `curl -fsSL ${base}/registry -o manifest.json`,
      `curl -fsSL ${base}/installer -o itsm-skills-install.py`,
      `# 2) 安装全部技能到 ${HOST_META[host].root}`,
      `python itsm-skills-install.py --manifest manifest.json --all --host ${host}`,
      `# 3) 验证（示例）`,
      host === 'claude'
        ? `python ~/.claude/skills/itsm-ticket-operations/scripts/itsm_ticket.py --help`
        : `python ~/.aicli/.agents/skills/itsm-ticket-operations/scripts/itsm_ticket.py --help`,
    ].join('\n');
  }, [host]);

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(`${what}已复制`);
    } catch {
      messageApi.error('复制失败，请手动选择文本');
    }
  };

  if (loading) {
    return (
      <div className="p-6 text-sm text-muted-foreground">加载中…</div>
    );
  }

  if (!manifest || manifest.configured === false) {
    return (
      <div className="space-y-4 p-6">
        <h1 className="text-xl font-semibold">资源中心 · Agent Skills</h1>
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Boxes className="h-5 w-5" /> 分发面未配置</CardTitle>
            <CardDescription>
              服务端尚未挂载 Skills 分发目录（环境变量 <code>SKILLS_DIST_DIR</code> 指向 <code>dist/skills/</code>，
              由 <code>scripts/package-skills.ps1</code> 产出）。原因：{manifest?.reason ?? '未知'}
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            <Boxes className="h-6 w-6" /> 资源中心 · Agent Skills
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            面向 Claude Code / Codex 等 code-x agent 的 ITSM 操作技能包：下载、校验、一键安装。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="mr-1 h-4 w-4" /> 刷新
        </Button>
      </div>

      {/* 分发元信息 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FileCheck2 className="h-4 w-4" /> 分发清单
          </CardTitle>
          <CardDescription className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
            <span>构建版本：<code>{manifest.sourceCommit?.slice(0, 8) ?? '-'}</code></span>
            <span>打包时间：{manifest.generatedAt ?? '-'}</span>
            <span>
              全量包：{fmtSize(manifest.bundle?.sizeBytes)} ·
              sha256 <code className="break-all">{manifest.bundle?.sha256?.slice(0, 16)}…</code>
            </span>
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={() => copy(installCmd, '安装命令 ')}>
              <Copy className="mr-1 h-4 w-4" /> 复制安装命令
            </Button>
            <Button size="sm" variant="outline" onClick={() => { window.location.href = '/api/v1/skills/bundle/download'; }}>
              <span>
                <Download className="mr-1 h-4 w-4" /> 下载全量包
              </span>
            </Button>
            <Button size="sm" variant="outline" onClick={() => { window.location.href = '/api/v1/skills/installer'; }}>
              <span>
                <Terminal className="mr-1 h-4 w-4" /> 下载安装器
              </span>
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* 宿主 Tab + 安装指导 */}
      <Tabs value={host} onValueChange={(v) => setHost(v as HostKey)}>
        <TabsList>
          {(Object.keys(HOST_META) as HostKey[]).map((h) => (
            <TabsTrigger key={h} value={h}>{HOST_META[h].label}</TabsTrigger>
          ))}
        </TabsList>
        {(Object.keys(HOST_META) as HostKey[]).map((h) => (
          <TabsContent key={h} value={h} className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">安装指导 · {HOST_META[h].label}</CardTitle>
                <CardDescription>
                  安装位置 <code>{HOST_META[h].root}</code> —— {HOST_META[h].note}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs leading-5">
                  {installCmd}
                </pre>
                <div className="mt-2">
                  <Button size="sm" variant="outline" onClick={() => copy(installCmd, '安装脚本')}>
                    <Copy className="mr-1 h-4 w-4" /> 复制脚本
                  </Button>
                </div>
              </CardContent>
            </Card>
          </TabsContent>
        ))}
      </Tabs>

      {/* 技能卡片 */}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {skills.map((s) => (
          <Card key={s.name}>
            <CardHeader>
              <CardTitle className="text-base">{s.name}</CardTitle>
              <CardDescription className="line-clamp-2">{s.description || '（无描述）'}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="secondary">{s.version}</Badge>
                <span>{fmtSize(s.sizeBytes)}</span>
                <code className="break-all">sha256:{s.sha256?.slice(0, 12)}…</code>
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="outline" onClick={() => { window.location.href = `/api/v1/skills/${s.name}/download`; }}>
                  <span>
                    <Download className="mr-1 h-4 w-4" /> 下载 zip
                  </span>
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => copy(`python itsm-skills-install.py --manifest manifest.json --name ${s.name} --host ${host}`, '单技能安装命令')}
                >
                  <Copy className="mr-1 h-4 w-4" /> 复制安装命令
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
};

export default SkillsResourcesPage;
