import { useParams } from 'react-router';

/**
 * 知识库文章详情页面
 * B6 修复：原本 /knowledge/articles/[id] 路由 404
 *
 * 发布 / 取消发布 / 归档 / 编辑统一由 `ArticleDetail` 卡片内的操作组承载。
 * 本页此前在卡片外又渲染了一组同款「发布文章 / 取消发布」按钮：与卡片内入口
 * 完全重复（同一 API 调用 + 各自重复拉取一次文章），用户视角即"页面顶部多了
 * 一个按钮"，且两处状态各自刷新、互不同步。故移除页面级按钮，只保留卡片内入口。
 */

import React from 'react';
import ArticleDetail from '@/components/knowledge/ArticleDetail';

export default function KnowledgeArticleDetailPage() {
  const { id } = useParams() as { id: string };
  // key 绑 id：文章间跳转时强制重挂载，避免沿用上一篇文章的本地状态（含反馈、版本预览）。
  return <ArticleDetail key={id} />;
}
