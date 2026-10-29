
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
    cloudServices: 0,
    accountServices: 0,
    securityServices: 0,
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

    // 分类 → 统计桶（每个分类最多命中一个桶，顺序即优先级）
    const bucketOf = (category: string): 'cloud' | 'account' | 'security' | null => {
      const name = category.toLowerCase();
      if (/云|cloud/.test(name)) return 'cloud';
      if (/账号|权限|支持|identity|account|support/.test(name)) return 'account';
      if (/安全|网络|security|network/.test(name)) return 'security';
      return null;
    };

    let cloudServices = 0;
    let accountServices = 0;
    let securityServices = 0;

    exactCount.forEach((count, category) => {
      switch (bucketOf(category)) {
        case 'cloud':
          cloudServices += count;
          break;
        case 'account':
          accountServices += count;
          break;
        case 'security':
          securityServices += count;
          break;
        default:
          // 未归入三卡的分类只计入 total，不硬塞进任何桶
          break;
      }
    });

    setStats({
      total: catalogs.length,
      cloudServices,
      accountServices,
      securityServices,
    });
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
