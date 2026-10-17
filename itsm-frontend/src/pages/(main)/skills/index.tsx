/**
 * 资源中心页（P2）：ITSM Skills 分发
 *
 * 设计原则（2026-10-17 用户决策）：
 * - **技能内容本位**：页面只介绍每个 skill 的功能与兼容格式（SKILL.md 标准目录结构），
 *   不指定安装位置——每个 code agent（Claude Code / Codex / aicli / 其他）有自己的技能
 *   根目录与发现机制，由 agent 自行决定放置位置。
 * - 不内置安装器教学；下载的是纯内容（zip 内含 SKILL.md + references/ + scripts/ + _lib/）。
 *
 * 数据源：GET /api/v1/skills-dist/registry（dist/skills/manifest.json 直通）。
 */

import { useEffect, useMemo, useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { httpClient } from '@/lib/api/http-client';
import { useMessage } from '@/hooks/useMessage';
import { Boxes, Copy, Download, FileCheck2, RefreshCw } from 'lucide-react';

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

const REGISTRY_BASE = '/api/v1/skills-dist';

function fmtSize(bytes?: number): string {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

const SkillsResourcesPage = () => {
  const [manifest, setManifest] = useState<SkillsManifest | null>(null);
  const [loading, setLoading] = useState(true);
  const messageApi = useMessage();

  const load = async () => {
    setLoading(true);
    try {
      const data = await httpClient.get<SkillsManifest>(`${REGISTRY_BASE}/registry`);
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

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(`${what}已复制`);
    } catch {
      messageApi.error('复制失败，请手动选择文本');
    }
  };

  const shaShort = (sha?: string) => (sha ? `${sha.slice(0, 12)}…` : '-');

  if (loading) {
    return <div className="p-6 text-sm text-muted-foreground">加载中…</div>;
  }

  if (!manifest || manifest.configured === false) {
    return (
      <div className="mx-auto max-w-4xl space-y-4 p-6">
        <PageHeader />
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Boxes className="h-5 w-5" /> 分发面未配置
            </CardTitle>
            <CardDescription>
              服务端尚未挂载 Skills 分发目录（环境变量 <code>SKILLS_DIST_DIR</code>）。
              原因：{manifest?.reason ?? '未知'}
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-6xl space-y-6 p-6">
      <PageHeader />

      {/* 格式说明：内容本位 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <FileCheck2 className="h-4 w-4" /> 什么是这些 Skill
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm leading-6 text-muted-foreground">
          <p>
            每个 Skill 是一个自包含目录，遵循 <strong>Agent Skill 标准结构</strong>（SKILL.md + references/ + scripts/），
            <strong>Claude Code、Codex、aicli 等主流 code agent 均可识别</strong>。安装方式由你的 agent 决定：
            将 zip 解压到该 agent 的技能目录（或按其官方文档导入）即可被自动发现。
          </p>
          <div className="grid gap-2 text-xs sm:grid-cols-2">
            <div className="rounded-md border bg-muted/40 p-3">
              <div className="mb-1 font-medium text-foreground">包内结构</div>
              <pre className="whitespace-pre-wrap font-mono leading-5">{`<skill-name>/
├── SKILL.md        # 触发说明 + 使用手册
├── references/     # API 契约参考
├── scripts/        # 零依赖 CLI（Python 3）
└── _lib/           # 共享底座`}</pre>
            </div>
            <div className="rounded-md border bg-muted/40 p-3">
              <div className="mb-1 font-medium text-foreground">使用前提</div>
              <ul className="list-inside list-disc space-y-1">
                <li>目标 ITSM 环境可达（demo/生产）</li>
                <li>Python 3.8+（CLI 仅标准库）</li>
                <li>具备相应业务权限码的账号</li>
              </ul>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 技能卡片 */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-base font-semibold">可用技能（{skills.length}）</h2>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => { window.location.href = `${REGISTRY_BASE}/bundle/download`; }}>
              <Download className="mr-1 h-4 w-4" /> 下载全量包（{fmtSize(manifest.bundle?.sizeBytes)}）
            </Button>
            <Button variant="outline" size="sm" onClick={() => void load()}>
              <RefreshCw className="mr-1 h-4 w-4" /> 刷新
            </Button>
          </div>
        </div>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {skills.map((s) => (
            <Card key={s.name} className="flex flex-col">
              <CardHeader>
                <CardTitle className="text-base">{s.name.replace(/^itsm-|-operations$/g, '')}</CardTitle>
                <CardDescription className="line-clamp-2">{s.description || '（无描述）'}</CardDescription>
              </CardHeader>
              <CardContent className="mt-auto space-y-3">
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Badge variant="secondary">v{s.version}</Badge>
                  <span>{fmtSize(s.sizeBytes)}</span>
                  <code className="break-all" title={s.sha256}>sha256:{shaShort(s.sha256)}</code>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => { window.location.href = `${REGISTRY_BASE}/${s.name}/download`; }}>
                    <Download className="mr-1 h-4 w-4" /> 下载
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => copy(`curl -fsSL -o ${s.file} "${window.location.origin}${REGISTRY_BASE}/${s.name}/download"`, '下载命令')}
                  >
                    <Copy className="mr-1 h-4 w-4" /> 复制链接
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>

      {/* 清单元信息（底部弱化） */}
      <Card>
        <CardContent className="flex flex-wrap items-center gap-x-6 gap-y-1 p-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1"><FileCheck2 className="h-3.5 w-3.5" /> 构建 <code>{manifest.sourceCommit?.slice(0, 8) ?? '-'}</code></span>
          <span>打包时间 {manifest.generatedAt ?? '-'}</span>
          <span>全量包 sha256 {shaShort(manifest.bundle?.sha256)}</span>
          <a className="underline" href={`${REGISTRY_BASE}/registry`} target="_blank" rel="noreferrer">原始 manifest</a>
        </CardContent>
      </Card>
    </div>
  );
};

function PageHeader() {
  return (
    <div>
      <h1 className="flex items-center gap-2 text-xl font-semibold">
        <Boxes className="h-6 w-6" /> 资源中心 · Agent Skills
      </h1>
      <p className="mt-1 text-sm text-muted-foreground">
        ITSM 操作技能包（SKILL.md 标准格式，Claude Code / Codex / aicli 兼容）——下载后交由你的 agent 放置到其技能目录。
      </p>
    </div>
  );
}

export default SkillsResourcesPage;
