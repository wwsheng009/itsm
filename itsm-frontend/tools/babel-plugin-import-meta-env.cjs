/**
 * Jest（babel-jest，CJS）专用的最小 Babel 插件：把 `import.meta` 重写为 CJS 可用的字面量对象。
 *
 * 背景：源码按 Vite 约定读取 `import.meta.env.*`（Vite 在构建/开发时静态注入）。
 * 但 Jest 走 CommonJS，`import.meta` 在 CJS 中非法。此插件只影响测试转译，
 * 不改变 Vite 构建产物（Vite 不读 babel.config.cjs）。
 *
 * 重写结果：
 *   - `import.meta` → `({ env: process.env, url: undefined })`
 *     即 `import.meta.env.VITE_X` → `process.env.VITE_X`（测试用 process.env 赋值）。
 *   - Vite 内建的构建模式开关按 `NODE_ENV` 求值，保持与迁移前
 *     `process.env.NODE_ENV === 'development' / 'production'` 完全一致的语义
 *     （测试里仍可动态改写 `process.env.NODE_ENV` 来切换开发/生产分支）：
 *       `import.meta.env.DEV`  → `(process.env.NODE_ENV === 'development')`
 *       `import.meta.env.PROD` → `(process.env.NODE_ENV === 'production')`
 *       `import.meta.env.MODE` → `(process.env.NODE_ENV || 'development')`
 */
module.exports = function importMetaToCjs() {
  const isImportMetaEnv = (node) =>
    node &&
    node.type === 'MemberExpression' &&
    !node.computed &&
    node.object &&
    node.object.type === 'MetaProperty' &&
    node.object.meta &&
    node.object.meta.name === 'import' &&
    node.object.property &&
    node.object.property.name === 'meta' &&
    node.property &&
    node.property.name === 'env';

  return {
    name: 'import-meta-to-cjs',
    visitor: {
      MemberExpression(path) {
        const { node } = path;
        if (node.computed || !node.property) return;
        if (!isImportMetaEnv(node.object)) return;

        const replacements = {
          DEV: '(process.env.NODE_ENV === "development")',
          PROD: '(process.env.NODE_ENV === "production")',
          MODE: '(process.env.NODE_ENV || "development")',
        };
        const replacement = replacements[node.property.name];
        if (!replacement) return;

        path.replaceWithSourceString(replacement);
        path.skip();
      },
      MetaProperty(path) {
        const { node } = path;
        if (node.meta && node.meta.name === 'import' && node.property && node.property.name === 'meta') {
          path.replaceWithSourceString('({ env: process.env, url: undefined })');
          path.skip();
        }
      },
    },
  };
};
