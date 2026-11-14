import fs from 'node:fs';
import path from 'node:path';

const globals = fs.readFileSync(
  path.join(process.cwd(), 'src/styles/globals.css'),
  'utf8',
);

describe('RichTextEditor 主题令牌一致性', () => {
  it('--rte-* 色板必须从设计 token 派生，不能写回固定 hex/rgba', () => {
    const declarations = [...globals.matchAll(/--rte-[a-z-]+:\s*([^;]+);/g)].map(m => ({
      value: m[1].trim(),
      declaration: m[0].trim(),
    }));

    expect(declarations.length).toBeGreaterThanOrEqual(26);

    for (const { value, declaration } of declarations) {
      expect(declaration).not.toMatch(/#[0-9a-fA-F]{3,8}\b|rgba?\(/);
      expect(value).toMatch(/var\(--(?:color|surface|background)-|color-mix\(/);
    }
  });

  it('占位符使用可读的 tertiary 文本 token', () => {
    const values = [...globals.matchAll(/--rte-placeholder:\s*([^;]+);/g)].map(m => m[1].trim());
    expect(values.length).toBeGreaterThanOrEqual(2);
    expect(values.every(value => value === 'var(--color-text-tertiary)')).toBe(true);
  });
});