/**
 * Agent Skills 安装页（公开访问，无需登录）
 *
 * 用途：用户把本页 URL（或其中的一句 prompt）粘贴给自己的 code agent，
 * agent self-fetch install.md（含本机绝对 URL 的自描述步骤）即可自动完成
 * 清单获取 → sha256 校验 → 解压到自身技能目录。
 *
 * 数据源：GET /api/v1/skills-dist/registry（公开）。
 * 本页不做登录校验——分发面为静态构建产物，无租户数据。
 */

import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { httpClient } from '@/lib/api/http-client';
import { Boxes, ArrowRight, Copy, Check, Terminal, Download, ShieldCheck } from 'lucide-react';

interface SkillEntry {
  name: string;
  version: string;
  description?: string;
  sha256: string;
  sizeBytes: number;
}

interface SkillsManifest {
  configured?: boolean;
  reason?: string;
  sourceCommit?: string;
  generatedAt?: string;
  skills?: SkillEntry[];
  bundle?: { file: string; sha256: string; sizeBytes: number };
}

const REGISTRY_BASE = '/api/v1/skills-dist';

function fmtSize(bytes?: number): string {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

export default function SkillsInstallPage() {
  const [manifest, setManifest] = useState<SkillsManifest | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    httpClient
      .get<SkillsManifest>(`${REGISTRY_BASE}/registry`)
      .then(setManifest)
      .catch(() => setError('分发接口不可用'));
  }, []);

  const installDocUrl = `${window.location.origin}${REGISTRY_BASE}/install.md`;

  // 给 agent 的 prompt：一句话 + 自描述文档 URL（agent fetch 文档后按步骤自装）
  const agentPrompt = [
    '请安装 ITSM Agent Skills：',
    `1. 获取安装说明：${installDocUrl}`,
    '2. 按说明文档的步骤完成全部技能的下载、sha256 校验与安装（安装位置由你根据自身技能目录约定决定）',
    '3. 安装后运行任一技能的 --help 验证',
  ].join('\n');

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(what);
      setTimeout(() => setCopied(null), 2000);
    } catch {
      /* clipboard 不可用时用户可手动选中文本 */
    }
  };

  const skills = manifest?.skills ?? [];

  return (
    <div className="min-h-screen bg-gradient-to-b from-slate-50 to-white">
      <nav className="sticky top-0 z-50 border-b border-gray-100 bg-white/90 backdrop-blur-md">
        <div className="container mx-auto flex h-16 items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link to="/" className="flex items-center gap-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-blue-600 to-indigo-600 text-sm font-bold text-white">
              AI
            </div>
            <span className="text-lg font-bold text-gray-900">AI-Native ITSM</span>
          </Link>
          <Link
            to="/skills"
            className="flex items-center gap-1.5 text-sm font-medium text-gray-600 transition-colors hover:text-gray-900"
          >
            登录后进入资源中心 <ArrowRight className="h-4 w-4" />
          </Link>
        </div>
      </nav>

      <div className="container mx-auto max-w-4xl space-y-6 px-4 py-8 sm:px-6 lg:px-8">
        {/* 标题 + 一句话 prompt */}
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold text-gray-900">
            <Boxes className="h-7 w-7" /> Agent Skills 一键安装
          </h1>
          <p className="mt-2 text-sm leading-6 text-gray-600">
            把下面的指令复制给任意 code agent（Claude Code / Codex / aicli…），
            agent 会读取自描述安装文档，自动完成全部 ITSM 技能的下载、校验与安装。
          </p>
        </div>

        <div className="rounded-xl border border-blue-200 bg-blue-50 p-5">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2 text-sm font-semibold text-gray-900">
              <Terminal className="h-4 w-4" /> 复制给 Agent 的指令
            </div>
            <button
              type="button"
              onClick={() => void copy(agentPrompt, '指令')}
              className="flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700"
            >
              {copied === '指令' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied === '指令' ? '已复制' : '复制指令'}
            </button>
          </div>
          <pre className="overflow-x-auto whitespace-pre-wrap rounded-lg bg-slate-900 p-4 text-xs leading-6 text-slate-100">
            {agentPrompt}
          </pre>
          <div className="mt-3 text-xs text-gray-600">
            安装说明文档（自描述，含绝对 URL）：
            <a className="ml-1 font-mono underline" href={installDocUrl} target="_blank" rel="noreferrer">
              {installDocUrl}
            </a>
          </div>
        </div>

        {/* 状态区 */}
        {error && (
          <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{error}</div>
        )}
        {manifest?.configured === false && (
          <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">
            分发面未配置：{manifest.reason ?? '未知'}
          </div>
        )}

        {manifest && manifest.configured !== false && (
          <>
            {/* 可信度说明 */}
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="rounded-xl border p-4">
                <ShieldCheck className="mb-2 h-5 w-5 text-green-600" />
                <div className="text-sm font-semibold">完整性校验</div>
                <div className="mt-1 text-xs text-gray-600">
                  每个包带 sha256，构建版本 <code>{manifest.sourceCommit?.slice(0, 8) ?? '-'}</code>
                </div>
              </div>
              <div className="rounded-xl border p-4">
                <Terminal className="mb-2 h-5 w-5 text-blue-600" />
                <div className="text-sm font-semibold">零依赖 CLI</div>
                <div className="mt-1 text-xs text-gray-600">Python 3.8+ 仅标准库，bash/PowerShell 通用</div>
              </div>
              <div className="rounded-xl border p-4">
                <Boxes className="mb-2 h-5 w-5 text-indigo-600" />
                <div className="text-sm font-semibold">标准格式</div>
                <div className="mt-1 text-xs text-gray-600">SKILL.md 结构，主流 code agent 直接识别</div>
              </div>
            </div>

            {/* 技能清单 */}
            <div>
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-base font-semibold">可用技能（{skills.length}）</h2>
                <a
                  className="flex items-center gap-1.5 text-sm text-blue-600 underline"
                  href={`${REGISTRY_BASE}/bundle/download`}
                >
                  <Download className="h-4 w-4" /> 下载全量包（{fmtSize(manifest.bundle?.sizeBytes)}）
                </a>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                {skills.map(s => (
                  <div key={s.name} className="rounded-xl border p-4">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-sm font-semibold">{s.name}</span>
                      <span className="text-xs text-gray-500">{fmtSize(s.sizeBytes)}</span>
                    </div>
                    <p className="mt-1 line-clamp-2 text-xs text-gray-600">{s.description || '（无描述）'}</p>
                    <div className="mt-2 flex items-center justify-between">
                      <code className="text-xs text-gray-400">{s.sha256?.slice(0, 12)}…</code>
                      <a
                        className="flex items-center gap-1 text-xs text-blue-600 underline"
                        href={`${REGISTRY_BASE}/${s.name}/download`}
                      >
                        <Download className="h-3 w-3" /> 下载
                      </a>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="border-t pt-4 text-center text-xs text-gray-400">
              构建 {manifest.sourceCommit?.slice(0, 8) ?? '-'} · 打包 {manifest.generatedAt ?? '-'} ·
              {' '}<Link to="/login" className="underline">登录进入系统</Link>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
