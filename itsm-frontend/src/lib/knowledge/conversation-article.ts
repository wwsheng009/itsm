/**
 * AI 会话 → 知识文章正文组装（会话页标题栏「保存成文章」）。
 *
 * 为什么单独成模块：新建页拿到的是**会话 ID**（不是正文），需要在目标页经
 * `GET /api/v1/ai/conversations/:id` 拉全量消息后组装成一篇 Markdown。组装逻辑与
 * React 解耦（纯函数 + 一个取数壳），既能被页面直接调用，也能脱离渲染单测。
 *
 * 落库形态：详情页按 `contentType=markdown` 渲染（remark-gfm），因此这里只产出
 * Markdown 原文、不产 HTML，标题 / 列表 / 表格语义由渲染端原样接住。
 *
 * 助手消息在库中存的是 `{"answer": "...", "sources": [...]}` JSON（见后端
 * `handlers/ai/service.go` 的持久化分支）；历史数据与降级路径可能是纯文本，一律兜底。
 */

import { getConversationMessages, type AIMessage, type RagAnswer } from '@/lib/api/ai-api';
import { deriveArticleTitle, type ArticlePrefill } from './ai-article-prefill';

/** 单轮问答：提问可缺失（会话第一条就是助手消息 / 历史数据缺用户消息）。 */
interface ConversationRound {
  question?: string;
  answer: string;
  sources: RagAnswer[];
}

/** 解析助手消息内容，取出回答正文与引用来源。 */
export function parseAssistantContent(content: string): { answer: string; sources: RagAnswer[] } {
  const raw = content ?? '';
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const record = parsed as Record<string, unknown>;
      const answer = typeof record.answer === 'string' ? record.answer : '';
      if (answer.trim()) {
        const sources = Array.isArray(record.sources)
          ? (record.sources as unknown[]).filter(
              (item): item is RagAnswer => Boolean(item) && typeof item === 'object'
            )
          : [];
        return { answer, sources };
      }
    }
  } catch {
    // 纯文本（历史数据 / 流式失败降级）：原样返回
  }
  return { answer: raw, sources: [] };
}

/** 引用来源展示文案：标题优先，缺失时退回片段 / 序号。 */
function sourceLabel(source: RagAnswer, index: number): string {
  const title = source.title?.trim();
  if (title) return title;
  const snippet = source.snippet?.trim();
  if (snippet) return snippet.length > 40 ? `${snippet.slice(0, 40)}…` : snippet;
  return `来源 ${index + 1}`;
}

/** 按 `objectType:id` 去重（保持首次出现顺序），供文末统一列出。 */
export function dedupeSources(sources: RagAnswer[]): RagAnswer[] {
  const seen = new Set<string>();
  const result: RagAnswer[] = [];
  for (const source of sources) {
    const key = `${source.objectType ?? ''}:${source.id ?? ''}`;
    if (seen.has(key)) continue;
    seen.add(key);
    result.push(source);
  }
  return result;
}

/** 「参考来源」章节（Markdown 列表；无来源时返回空串）。 */
function renderReferenceSection(sources: RagAnswer[]): string {
  const unique = dedupeSources(sources);
  if (unique.length === 0) return '';
  const lines = unique.map((source, index) => {
    const meta = [
      source.objectType,
      typeof source.score === 'number' ? `相关度 ${(source.score * 100).toFixed(0)}%` : '',
    ]
      .filter(Boolean)
      .join(' · ');
    return `- ${sourceLabel(source, index)}${meta ? `（${meta}）` : ''}`;
  });
  return ['## 参考来源', '', ...lines].join('\n');
}

/** 在 Markdown 正文结尾追加「参考来源」章节；无来源时原样返回，不产生空章节。 */
function appendReferenceSources(markdown: string, sources?: RagAnswer[]): string {
  const section = renderReferenceSection(sources ?? []);
  if (!section) return markdown;
  return `${markdown.trimEnd()}\n\n${section}\n`;
}

/**
 * 把一次会话的消息列表整理成文章草稿（标题 + Markdown 正文）。
 * - 按「提问/回答」成对组织，保留完整回答正文（不截断）；
 * - 引用来源汇总到文末「参考来源」，按 `objectType:id` 去重；
 * - 无任何可用回答时返回 null（调用方按错误态处理，不产出空文章）。
 */
export function buildConversationArticle(messages: AIMessage[]): ArticlePrefill | null {
  // 后端已按 createdAt 升序返回；同秒并列时 id 才是稳定序，这里补一次兜底排序。
  const ordered = [...(messages ?? [])].sort((a, b) => (a.id ?? 0) - (b.id ?? 0));

  const rounds: ConversationRound[] = [];
  let pendingQuestion: string | undefined;
  for (const message of ordered) {
    if (message.role === 'user') {
      const question = (message.content ?? '').trim();
      if (question) pendingQuestion = question;
      continue;
    }
    if (message.role !== 'assistant') continue;
    const { answer, sources } = parseAssistantContent(message.content ?? '');
    if (!answer.trim()) continue;
    rounds.push({ question: pendingQuestion, answer: answer.trim(), sources });
    pendingQuestion = undefined;
  }

  if (rounds.length === 0) return null;

  // 标题取首个提问（定义会话主题）；没有提问时退回首个回答，与单条入口口径一致。
  const titleSeed = rounds[0].question || rounds[0].answer;
  const title = deriveArticleTitle(titleSeed);

  const blocks: string[] = [`> 本文由 AI 助手会话整理，共 ${rounds.length} 轮问答。`];
  rounds.forEach((round, index) => {
    if (round.question) blocks.push(`## 提问 ${index + 1}`, '', round.question);
    blocks.push(`## 回答 ${index + 1}`, '', round.answer);
  });

  let content = `${blocks.join('\n\n')}\n`;
  const allSources = rounds.flatMap(round => round.sources);
  content = appendReferenceSources(content, allSources);

  return { title, content };
}

/**
 * 拉取会话并组装成文章草稿（新建页调用）。
 * 会话不存在 / 无可用回答时抛错，由调用方呈现错误态并保留空白新建。
 */
export async function fetchConversationArticle(conversationId: number): Promise<ArticlePrefill> {
  const messages = await getConversationMessages(conversationId);
  const article = buildConversationArticle(messages);
  if (!article) {
    throw new Error('该会话没有可用于生成文章的回答');
  }
  return article;
}
