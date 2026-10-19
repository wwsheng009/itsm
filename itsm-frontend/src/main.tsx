import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider, createBrowserRouter } from 'react-router';
import { routes } from '@/routes';
import { installChunkLoadErrorHandler } from '@/lib/chunk-recovery';
import '@/styles/globals.css';

// 部署换版后旧标签页可能引用已删除的哈希 chunk：先安装整页刷新兜底再渲染。
installChunkLoadErrorHandler();

const router = createBrowserRouter(routes);

const container = document.getElementById('root');
if (!container) {
  throw new Error('Root container #root not found in index.html');
}

createRoot(container).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>
);
