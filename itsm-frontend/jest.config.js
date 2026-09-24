// 迁移说明（Next.js → Vite，见 docs/plan/vite-migration-plan.md §3.4 决策 D4）：
// 原实现用 `next/jest`（Webpack/SWC 转译 + Next 专属 mock）。`next` 依赖已移除，
// 这里改用 babel-jest + ./babel.config.cjs（职责等价：TS/TSX 转译、import.meta.env → process.env、
// CSS/静态资源 stub）。Jest 本身按 D4 保持不变，Vitest 迁移另行立项。
// 注意：`next/jest` 会自动 mock CSS/图片导入，此处用 moduleNameMapper 显式补齐。

/** @type {import('jest').Config} */
const customJestConfig = {
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],
  // 自定义环境:让 jsdom 样式级联对 nwsapi 无法解析的 `:has()` + Tailwind 任意值类
  // 选择器返回"不匹配"而非抛错(浏览器行为),详见 jest-environment-safe-nwsapi.js
  testEnvironment: '<rootDir>/jest-environment-safe-nwsapi.js',
  transform: {
    // 注意：`configFile` 不能用 `<rootDir>` 占位符（该替换只作用于部分顶层配置项，
    // babel-jest 会原样收到字符串并报 "Cannot find module '<rootDir>/babel.config.cjs'"）。
    // 相对路径由 Babel 相对于 jest 的工作目录（= 本包根目录）解析。
    '^.+\\.(t|j)sx?$': [
      'babel-jest',
      { configFile: './babel.config.cjs' },
    ],
  },
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/src/$1',
    '^lodash-es$': 'lodash',
    // 样式与静态资源：等价于 next/jest 的内置 stub
    '\\.(css|less|sass|scss)$': '<rootDir>/tools/jest-style-mock.cjs',
    '\\.(jpg|jpeg|png|gif|webp|avif|svg|ico|bmp)$': '<rootDir>/tools/jest-style-mock.cjs',
    '\\.(woff|woff2|eot|ttf|otf)$': '<rootDir>/tools/jest-style-mock.cjs',
  },
  transformIgnorePatterns: [
    'node_modules/(?!(lodash-es)/)',
  ],
  testMatch: [
    '<rootDir>/src/**/__tests__/**/*.{js,jsx,ts,tsx}',
    '<rootDir>/src/**/*.{test,spec}.{js,jsx,ts,tsx}',
  ],
  collectCoverage: true,
  collectCoverageFrom: [
    'src/lib/**/*.{ts,tsx}',
    '!src/lib/**/__tests__/**',
    '!src/lib/**/index.ts',
    '!src/lib/**/types.ts',
  ],
  coverageThreshold: {
    global: {
      // Keep the gate just below the measured baseline while the legacy UI
      // modules are incrementally covered; statements/functions/lines remain
      // at the 80% production threshold.
      branches: 64.5,
      functions: 80,
      lines: 80,
      statements: 80,
    },
  },
  coverageReporters: [
    'text',
    'text-summary',
    'html',
    'lcov',
    'json-summary',
  ],
  coverageDirectory: 'coverage',
  testTimeout: 10000,
  verbose: true,
  clearMocks: true,
  restoreMocks: true,
  resetModules: true,
  maxWorkers: '50%',
  cacheDirectory: '<rootDir>/.jest-cache',
  errorOnDeprecated: true,
  notify: false,
  notifyMode: 'failure-change',
  watchPlugins: [
    'jest-watch-typeahead/filename',
    'jest-watch-typeahead/testname',
  ],
  reporters: [
    'default',
    [
      'jest-junit',
      {
        outputDirectory: 'test-results',
        outputName: 'junit.xml',
        ancestorSeparator: ' › ',
        uniqueOutputName: 'false',
        suiteNameTemplate: '{filepath}',
        classNameTemplate: '{classname}',
        titleTemplate: '{title}',
      },
    ],
  ],
};

export default customJestConfig;
