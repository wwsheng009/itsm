// 仅用于 Jest 的 Babel 配置（Vite 构建链路使用 esbuild/SWC，不读本文件）。
// 迁移说明：`next/jest` 移除后，测试转译改为 babel-jest + 本配置。
// 详见 docs/plan/vite-migration-plan.md §3.4（决策 D4）。
module.exports = {
  presets: [
    ['@babel/preset-env', { targets: { node: 'current' } }],
    ['@babel/preset-react', { runtime: 'automatic' }],
    '@babel/preset-typescript',
  ],
  plugins: [
    // 源码使用 Vite 约定的 `import.meta.env.X`；CJS 测试环境下重写为 `process.env.X`。
    './tools/babel-plugin-import-meta-env.cjs',
  ],
};
