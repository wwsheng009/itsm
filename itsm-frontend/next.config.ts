import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  reactStrictMode: true,

  // ── dev 性能备忘（2026-09-22 实测，未改运行时行为）──────────────────────────
  // 机器画像：VMware VM / Xeon E5-2686 v4（8 vCPU @2.3GHz）/ 虚拟磁盘 / Defender 实时防护开启。
  // 1) 冷编译（Next dev 按需编译，每路由只在首次请求时付一次）：
  //    - Turbopack：/login 198.1s、/tickets/create 134.7s（本次 dev server 实测，旧记录 55~91s）
  //    - webpack（npm run dev:webpack）：/login 345.4s / 3855 modules
  //    ⇒ 本机 Turbopack 冷编译约比 webpack 快 1.7 倍，dev 默认保持 --turbopack。
  //    ⇒ 首次点击菜单项要等服务端编译完，表现为「点了很久没反应」；缓解办法＝后台预热：
  //      `npm run dev:warmup`（起 server 并自动预热高频路由，需 Cookie，见脚本注释）
  //      `npm run dev:warm`（server 已在跑时，手动预热指定路由）
  // 2) Turbopack 15.5 stable 无持久化缓存：experimental.turbopackPersistentCaching 会抛
  //    CanaryOnlyError（next/dist/server/config.js），.next/cache 为空 ⇒ 重启 dev server 后全部重编译。
  //    webpack 引擎反而有 .next/cache/webpack 持久缓存（冷编译更慢、但重启后可复用），
  //    频繁重启的工作流可用 npm run dev:webpack 对比取舍。
  // 3) experimental.optimizePackageImports 在 Turbopack 下被忽略
  //    （next/dist/lib/turbopack-warning.js 明确提示 ignored），仅 webpack 模式生效；
  //    webpack 模式下 antd/@ant-design/icons/lucide-react/recharts/date-fns 等已在 Next 默认优化列表内。
  // 4) dev 下 <Link> 只在 hover 时预取（next/dist/client/link.js），所以 hover 菜单会提前触发该路由编译。
  // 5) 热请求：页面 SSR 每请求 1.2~2.6s CPU（React 19 dev 调用栈采集 + 每请求模块重初始化/GC
  //    + antd v6 cssinjs 每请求内联 30~320KB 样式）；生产构建不存在这些开销。

  // Local development fallback: when NEXT_PUBLIC_API_URL is intentionally
  // empty, keep browser requests same-origin and proxy them to the backend.
  // Production deployments may provide the same /api routing at the ingress.
  async rewrites() {
    const configuredBackendURL = process.env.ITSM_BACKEND_URL;
    if (process.env.NODE_ENV === 'production' && !configuredBackendURL) {
      return [];
    }

    // In development, proxy /api requests to the local backend (via SSH tunnel)
    // When accessed from LAN, use 127.0.0.1 since the tunnel is on the same machine
    const backendURL = configuredBackendURL || 'http://127.0.0.1:8090';
    return [
      {
        source: '/api/:path*',
        destination: `${backendURL}/api/:path*`,
      },
    ];
  },

  // 图片优化配置
  images: {
    formats: ['image/webp', 'image/avif'],
    remotePatterns: [
      {
        protocol: 'https',
        hostname: '**',
      },
      {
        protocol: 'http',
        hostname: '**',
      },
    ],
    minimumCacheTTL: 86400, // 缓存 1 天
  },
  output: 'standalone',
  outputFileTracingRoot: process.cwd(),

  // P0 修复：强制 TypeScript 与 ESLint 在构建期暴露错误，避免缺 chunk 白屏雪崩。
  // 若需临时跳过某些历史告警，请在受影响的文件顶部用 `// @ts-expect-error <reason>` 或 `// eslint-disable-next-line <rule>` 局部处理。
  typescript: {
    ignoreBuildErrors: false,
  },
  eslint: {
    ignoreDuringBuilds: false,
  },
};

export default nextConfig;
