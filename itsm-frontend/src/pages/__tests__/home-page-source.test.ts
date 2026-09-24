import fs from 'node:fs';
import path from 'node:path';

describe('开源首页快速开始契约', () => {
  // 迁移说明（Next.js → Vite，见 docs/plan/vite-migration-plan.md）：首页自
  // `src/app/page.tsx` 迁至 `src/pages/home/index.tsx`（`src/app` 目录随 App Router 移除）。
  const source = fs.readFileSync(path.join(process.cwd(), 'src/pages/home/index.tsx'), 'utf8');

  it('使用真实仓库目录和当前开发启动命令', () => {
    expect(source).toContain('$ cd itsm');
    expect(source).toContain('$ cp .env.dev.example .env');
    expect(source).toContain('$ make dev-start-docker');
    expect(source).not.toContain('$ cd tism');
  });

  it('不把开发默认密码描述为生产部署凭据', () => {
    expect(source).toContain('本地开发默认账户');
    expect(source).toContain('生产部署使用一次性 bootstrap token');
    expect(source).toContain('不提供默认密码');
  });
});
