# 238 demo 部署证据：里程碑/任务/项目日期编辑预填崩溃修复（2026-10-18，1.8.0-dev.f2e32b4e）

## 问题与根因

- 入口：`http://172.18.3.238:8089/projects/4` → 「里程碑」→「编辑」。
- 现象：页面进入 ErrorBoundary「页面出现错误」。
- 复现错误（Tabbit，修复前）：
  ```
  TypeError: t.isValid is not a function
      at Object.isValidate (.../antd-*.js)
      at Object.current (.../antd-*.js)
  ErrorBoundary caught an error: TypeError: t.isValid is not a function
  ```
- 根因：`ProjectMilestone.dueDate` 是 API 字符串，`milestoneForm.setFieldsValue({ dueDate: m.dueDate })` 直接传给 Ant Design `DatePicker`，组件调用 `value.isValid()` 时崩溃。
- 同类隐患：任务编辑的 `planStart/planEnd`、项目列表编辑的计划起止 RangePicker 也直接使用 API 字符串。

## 修复

- 新增 `itsm-frontend/src/lib/utils/date-value.ts`：
  - `dateValue(apiString)`：API 字符串 → Dayjs；空值/非法值 → `undefined`；
  - `isoValue(dayjs)`：Dayjs → 项目域 date-only ISO（UTC 00:00）；空值/非法值 → `undefined`。
- 项目域日期统一按 **UTC 日历日** 预填，与列表 `slice(0,10)` 口径一致；避免 `2026-11-14T16:00Z` 在 CST 下显示为 11-15。
- 接入：
  - 里程碑编辑 `dueDate`；
  - 任务编辑 `planStart/planEnd`；
  - 项目列表编辑计划起止 `range`。
- 提交时显式转 ISO，避免依赖 JSON 对 Dayjs 的隐式序列化。

## 部署

| 项 | 值 |
|---|---|
| 前端提交 | `f2e32b4e`（cherry-pick 自 `feat/msp-p0-hardening` 的 `81787af0`，部署分支 `deploy-datefix2`） |
| 首版修复 | `3eb8bcc5`（弹窗崩溃已修复；复核发现 CST 日期偏移后补 UTC 口径） |
| 镜像 | `itsm-frontend:1.8.0-dev.f2e32b4e`（sha256 `95271510…`）；backend 由 `3eb8bcc5` retag（sha256 `692825e5…`） |
| demo 版本 | `1.8.0-dev.f2e32b4e` |
| 容器 | backend/worker/frontend/init 均 healthy/exit 0；init run 45 |

## 验证证据

### 修复前（Tabbit）

- 点击「编辑」后 ErrorBoundary；`TypeError: t.isValid is not a function`；
- 页面文本：`页面出现错误 抱歉，页面遇到了一个意外错误...`。

### 修复后（Tabbit，admin）

| 检查 | 结果 |
|---|---|
| 里程碑编辑弹窗 | 标题「编辑里程碑」，正常打开 |
| 计划日期预填 | `2026-11-14`（与列表一致） |
| 保存 | 提示「保存成功」，无 ErrorBoundary |
| console error | 0 |
| pageerror | 0 |
| failed response | 0 |
| 任务编辑弹窗 | 正常打开，无 ErrorBoundary（同类日期预填修复） |
| 项目编辑弹窗 | 正常打开，RangePicker 不再崩溃 |
| 数据库复核 | `project_milestones.due_date = 2026-11-14 00:00:00+00`（date-only 归一） |

截图：`C:\Users\vince\AppData\Local\Tabbit Browser\User Data\LocalAgent\Runtime\artifacts\task-3119dfd8-3207-4e9b-9087-25ce334b318b\screenshot-shot-4a148569-70d4-4f3a-a825-69105c5790c5.png`

### 构建与单测

- `tsc --noEmit` ✅
- `date-value` 单测 **4/4** ✅（含 UTC 日历日跨时区用例）
- 改动文件 ESLint ✅
- 前端生产构建 `BUILD_FRONTEND_EXIT=0` ✅
- `route-permissions` **13/13** ✅（回归）

## 复跑方式

```bash
NEW=1.8.0-dev.f2e32b4e BUNDLE=/tmp/itsm-datefix-f2e32b4e.bundle bash /tmp/deploy238_progressfix_a.sh
NEW=1.8.0-dev.f2e32b4e BASE=1.8.0-dev.3eb8bcc5 bash /tmp/deploy238_progressfix_a2b.sh   # sudo
NEW=1.8.0-dev.f2e32b4e bash /tmp/deploy238_progressfix_w2.sh                              # sudo（轮询）
NEW=1.8.0-dev.f2e32b4e bash /tmp/deploy238_progressfix_c.sh                               # sudo
```

## 备注

- 本地 `feat/msp-p0-hardening` 分支在此期间出现独立的 `fb01af39 feat(llm): 支持完整 Endpoint 路径配置` 提交（非本次修复），部署分支 `deploy-datefix2` 未包含该提交，demo 未部署 LLM 变更。
- 项目域日期按 UTC 日历日归一为 `T00:00:00Z`；保存里程碑后 `due_date` 从 `16:00Z` 归一为 `00:00Z`，页面显示日期不变。
