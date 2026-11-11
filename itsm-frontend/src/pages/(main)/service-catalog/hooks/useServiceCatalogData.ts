
import { useState, useEffect, useCallback } from 'react';
import { ServiceCatalogApi } from '@/lib/api/service-catalog-api';
import type { ServiceItem } from '@/types/service-catalog';

export const useServiceCatalogData = () => {
  const [catalogs, setCatalogs] = useState<ServiceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchText, setSearchText] = useState('');
  const [categoryFilter, setCategoryFilter] = useState('');
  const [ciTypeFilter, setCiTypeFilter] = useState<number | undefined>(undefined);
  const [cloudServiceFilter, setCloudServiceFilter] = useState<number | undefined>(undefined);
  const [stats, setStats] = useState({
    total: 0,
    categories: [] as Array<{ name: string; count: number }>,
  });

  const loadServiceCatalogs = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await ServiceCatalogApi.getServices({
        page: 1,
        pageSize: 100,
        search: searchText,
        status: 'published' as any,
      });
      setCatalogs(data.services || []);
    } catch (error) {
      console.error('加载服务目录失败:', error);
      setError(error instanceof Error ? error.message : '服务目录加载失败，请稍后重试');
    } finally {
      setLoading(false);
    }
  }, [searchText]);

  useEffect(() => {
    loadServiceCatalogs();
  }, [loadServiceCatalogs]);

  useEffect(() => {
    // N8：顶部统计必须与下方分类标签页同一口径。
    //
    // 旧实现按「条目级」关键词模糊匹配（category.includes('支持') 等），个别分类会被
    // 两个桶重复计入，也会漏掉命名不同的分类，于是出现同屏「账号与权限」上方 2 / 下方 1、
    // 且分类合计 ≠ 服务总数。
    //
    // 新口径：先按 category 精确聚合（与标签页 `category === 选中分类` 完全一致），
    // 再把「每个分类整体」归入唯一一个统计桶 —— 这样每个分类只被计一次，
    // 标签页看到的条数与本桶贡献严格相等，三卡合计不会超过总数。
    const exactCount = new Map<string, number>();
    catalogs.forEach(catalog => {
      const category = String(catalog.category || '').trim();
      if (!category) return;
      exactCount.set(category, (exactCount.get(category) || 0) + 1);
    });

    // 顶部卡片直接展示真实分类（最多前 3 类），不再把「终端支持/网络与访问」
    // 硬塞进「账号与权限/安全服务」固定桶。每个分类的顶部计数与下方标签页
    // 使用同一个 category 精确聚合结果，避免同屏数字互相矛盾。
    const categories = Array.from(exactCount.entries())
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name, 'zh-CN'))
      .slice(0, 3);

    setStats({ total: catalogs.length, categories });
  }, [catalogs]);

  return {
    catalogs,
    loading,
    error,
    stats,
    searchText,
    setSearchText,
    categoryFilter,
    setCategoryFilter,
    ciTypeFilter,
    setCiTypeFilter,
    cloudServiceFilter,
    setCloudServiceFilter,
    loadServiceCatalogs,
  };
};
