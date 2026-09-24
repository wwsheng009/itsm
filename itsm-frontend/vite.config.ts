import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath, URL } from 'node:url';

// Vite 配置（Next.js → Vite 迁移，见 docs/plan/vite-migration-plan.md §5.1）
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const backend = env.ITSM_BACKEND_URL || 'http://127.0.0.1:8090';
  const isProd = mode === 'production';

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    base: '/',
    define: {
      __APP_VERSION__: JSON.stringify(env.VITE_APP_VERSION ?? env.npm_package_version ?? 'dev'),
      __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
      // 应用源码统一使用 `import.meta.env.DEV / PROD / MODE`（见 §5.7）。
      // 这里仍固定 `process.env.NODE_ENV`：部分第三方依赖会在代码里读取它，
      // 避免浏览器环境出现 `process is not defined`。
      'process.env.NODE_ENV': JSON.stringify(isProd ? 'production' : 'development'),
    },
    server: {
      host: '0.0.0.0',
      port: 3000,
      strictPort: true,
      proxy: {
        '/api': { target: backend, changeOrigin: false, cookieDomainRewrite: '' },
      },
    },
    preview: {
      host: '0.0.0.0',
      port: 3000,
      strictPort: true,
    },
    optimizeDeps: {
      // antd / 图表 / 编辑器等大依赖预打包，避免首次进入页面时浏览器侧二次发现
      include: [
        'react',
        'react-dom',
        'react-router',
        'antd',
        '@ant-design/icons',
        '@tanstack/react-query',
        'axios',
        'dayjs',
      ],
    },
    build: {
      outDir: 'dist',
      sourcemap: !isProd,
      chunkSizeWarningLimit: 1500,
      rollupOptions: {
        output: {
          // 图表库单独成 chunk。注意：本项目不依赖 `echarts`（@ant-design/charts 2.x 基于
          // @antv/G2），而对象形式的 manualChunks 会把不存在的包当作入口解析并直接构建失败。
          manualChunks: {
            react: ['react', 'react-dom', 'react-router'],
            antd: ['antd', '@ant-design/icons'],
            charts: ['@ant-design/charts', 'recharts'],
          },
        },
      },
    },
  };
});
