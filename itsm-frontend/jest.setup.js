import '@testing-library/jest-dom';

// Mock dayjs - required for Ant Design DatePicker
// Must mock both default and named exports
jest.mock('dayjs', () => {
  const mockDate = {
    format: () => '2024-01-01',
    isValid: () => true,
    isAfter: () => false,
    isBefore: () => false,
    isSame: () => false,
    add: () => mockDate,
    subtract: () => mockDate,
    startOf: () => mockDate,
    endOf: () => mockDate,
    diff: () => 0,
    valueOf: () => 1704067200000,
    toISOString: () => '2024-01-01T00:00:00.000Z',
    hour: () => 12,
    minute: () => 30,
    second: () => 45,
    year: () => 2024,
    month: () => 0,
    date: () => 1,
    day: () => 1,
    unix: () => 1704067200,
    toDate: () => new Date(),
    toArray: () => [2024, 0, 1, 12, 30, 45, 0],
    isLeapYear: () => false,
    clone: () => mockDate,
    set: () => mockDate,
  };

  const mockDayjs = jest.fn((date) => {
    if (!date) return mockDate;
    return mockDate;
  });

  // Mock dayjs instance methods
  mockDayjs.prototype.format = jest.fn(function(format) {
    return '2024-01-01';
  });
  mockDayjs.prototype.isValid = jest.fn(function() {
    return true;
  });
  mockDayjs.prototype.isAfter = jest.fn(function() {
    return false;
  });
  mockDayjs.prototype.isBefore = jest.fn(function() {
    return false;
  });
  mockDayjs.prototype.isSame = jest.fn(function() {
    return false;
  });
  mockDayjs.prototype.add = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.prototype.subtract = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.prototype.startOf = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.prototype.endOf = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.prototype.diff = jest.fn(function() {
    return 0;
  });
  mockDayjs.prototype.valueOf = jest.fn(function() {
    return 1704067200000;
  });
  mockDayjs.prototype.toISOString = jest.fn(function() {
    return '2024-01-01T00:00:00.000Z';
  });
  mockDayjs.prototype.hour = jest.fn(function() {
    return 12;
  });
  mockDayjs.prototype.minute = jest.fn(function() {
    return 30;
  });
  mockDayjs.prototype.second = jest.fn(function() {
    return 45;
  });
  mockDayjs.prototype.year = jest.fn(function() {
    return 2024;
  });
  mockDayjs.prototype.month = jest.fn(function() {
    return 0;
  });
  mockDayjs.prototype.date = jest.fn(function() {
    return 1;
  });
  mockDayjs.prototype.day = jest.fn(function() {
    return 1;
  });
  mockDayjs.prototype.unix = jest.fn(function() {
    return 1704067200;
  });
  mockDayjs.prototype.toDate = jest.fn(function() {
    return new Date();
  });
  mockDayjs.prototype.toArray = jest.fn(function() {
    return [2024, 0, 1, 12, 30, 45, 0];
  });
  mockDayjs.prototype.isLeapYear = jest.fn(function() {
    return false;
  });
  mockDayjs.prototype.clone = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.prototype.set = jest.fn(function() {
    return mockDate;
  });

  // Mock static methods
  mockDayjs.extend = jest.fn();
  mockDayjs.locale = jest.fn();
  mockDayjs.unix = jest.fn(function() {
    return mockDate;
  });
  mockDayjs.isDayjs = jest.fn();

  return mockDayjs;
});

if (typeof globalThis.MessageChannel === 'undefined') {
  // React's scheduler needs MessageChannel, but worker_threads.MessageChannel
  // keeps a native MESSAGEPORT referenced for the lifetime of each Jest worker.
  // A browser-shaped microtask-backed channel preserves async scheduling in
  // jsdom without leaving a native handle or coupling React to fake timers.
  globalThis.MessageChannel = class TestMessageChannel {
    constructor() {
      this.port1 = { onmessage: null };
      this.port2 = {
        postMessage: (data) => {
          queueMicrotask(() => this.port1.onmessage?.({ data }));
        },
      };
    }
  };
}

// jsdom（jest-environment-jsdom 29 内置的 jsdom）不提供 Web Encoding/Streams 全局对象，
// 而 react-router v7 的 development 构建在「模块初始化时」就读取 TextEncoder：
//   const encoder = new TextEncoder();
// 一旦缺失，任何 `require('react-router')`（含 `jest.requireActual('react-router')`）
// 都会抛 `ReferenceError: TextEncoder is not defined`。这里在任何 react-router 引用之前补齐，
// 与浏览器环境对齐（Node 18+ 有原生实现，直接复用）。
if (typeof globalThis.TextEncoder === 'undefined' || typeof globalThis.TextDecoder === 'undefined') {
  const { TextDecoder: NodeTextDecoder, TextEncoder: NodeTextEncoder } = require('node:util');
  globalThis.TextEncoder = NodeTextEncoder;
  globalThis.TextDecoder = NodeTextDecoder;
}

if (
  typeof globalThis.ReadableStream === 'undefined' ||
  typeof globalThis.TransformStream === 'undefined'
) {
  const webStreams = require('node:stream/web');
  globalThis.ReadableStream = webStreams.ReadableStream;
  globalThis.WritableStream = webStreams.WritableStream;
  globalThis.TransformStream = webStreams.TransformStream;
  globalThis.ByteLengthQueuingStrategy = webStreams.ByteLengthQueuingStrategy;
  globalThis.CountQueuingStrategy = webStreams.CountQueuingStrategy;
}

// Mock react-router（迁移自 next/navigation mock，见 docs/plan/vite-migration-plan.md §5.2）
//
// 单元测试默认不挂载真实 <Router>，而 SPA 组件普遍使用
// useNavigate / useLocation / useParams / useSearchParams / Link，
// 这些 API 在没有 Router 上下文时会抛错。这里用最小实现替换，
// 其余导出（HashRouter/Routes 等）保持真实实现。
//
// 需要断言导航行为的测试，可在自身文件中
// `jest.mock('react-router', () => ({ ...jest.requireActual('react-router'), ... }))` 覆盖。
jest.mock('react-router', () => {
  const actual = jest.requireActual('react-router');
  const React = require('react');
  const anchor =
    (displayName) =>
    ({ to, children, ...rest }) =>
      React.createElement(
        'a',
        { href: typeof to === 'string' ? to : '#', ...rest },
        typeof children === 'function'
          ? children({ isActive: false, isPending: false, isTransitioning: false })
          : children
      );
  const Link = anchor('Link');
  const NavLink = anchor('NavLink');

  return {
    ...actual,
    useNavigate: () => jest.fn(),
    useLocation: () => ({ pathname: '/', search: '', hash: '', state: null, key: 'test' }),
    useParams: () => ({}),
    useSearchParams: () => [new URLSearchParams(), jest.fn()],
    useRouteError: () => undefined,
    useNavigation: () => ({ state: 'idle' }),
    Link,
    NavLink,
    Navigate: () => null,
    Outlet: () => null,
  };
});

// Mock window.matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: jest.fn().mockImplementation(query => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: jest.fn(), // deprecated
    removeListener: jest.fn(), // deprecated
    addEventListener: jest.fn(),
    removeEventListener: jest.fn(),
    dispatchEvent: jest.fn(),
  })),
});

// Mock IntersectionObserver
global.IntersectionObserver = class IntersectionObserver {
  constructor() {}
  observe() {
    return null;
  }
  disconnect() {
    return null;
  }
  unobserve() {
    return null;
  }
};

// Mock ResizeObserver
global.ResizeObserver = class ResizeObserver {
  constructor() {}
  observe() {
    return null;
  }
  disconnect() {
    return null;
  }
  unobserve() {
    return null;
  }
};

// Suppress console warnings in tests
const originalError = console.error;
beforeAll(() => {
  console.error = (...args) => {
    if (
      typeof args[0] === 'string' &&
      args[0].includes('Warning: ReactDOM.render is no longer supported')
    ) {
      return;
    }
    originalError.call(console, ...args);
  };
});

afterAll(() => {
  console.error = originalError;
});
