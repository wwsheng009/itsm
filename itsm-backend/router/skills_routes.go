package router

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/common"
)

// ============================================================================
// Skills 分发路由（README §6.2 Phase 2：ITSM 内建资源中心）
//
// 数据源为 P1 打包产物 dist/skills/（package-skills.ps1 产出），由部署侧通过
// SKILLS_DIST_DIR 环境变量挂载（docker volume / CI 构建步骤均可）。
// 目录缺失或未配置 → 注册探测端点返回 configured=false，页面降级为「未配置」提示；
// 其余端点直接 404，不影响主流程。
//
// 安全面（受信内网分发面，仍按最小信任处理）：
//   - 只读；download 仅允许 manifest 登记过的文件名，且清洗后必须落在 dist 目录内（防路径穿越）
//   - 文件大小以 manifest.sizeBytes 为准上限校验
// ============================================================================

// skillsDistDir 返回分发包目录；未配置返回空串。
func skillsDistDir() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("SKILLS_DIST_DIR")), `/\`)
}

// SetupSkillsRoutes 注册资源中心路由（AI Skill Registry v1 已占用 /skills 与 /skills/:code，
// 见 handlers/skill RegisterRoutes——本面是**构建产物分发面**，与运行时技能注册表是两个概念）。
// 路径规划（避让 v1 registry）：
//   - /skills-dist/registry       清单（JSON，统一 {code,message,data} 信封）
//   - /skills-dist/manifest       清单（JSON，裸清单，供安装器/CI 直接消费，无信封）
//   - /skills-dist/install.md     Agent 安装说明（Markdown，动态生成，含本机绝对 URL）
//   - /skills-dist/installer      零依赖安装器
//   - /skills-dist/:name/download / /skills-dist/bundle/download  分发包
//
// 挂载策略（gin v1.12.0 同一 engine 上两个 group 挂同一路径会在第二次注册时 panic，
// 2026-10-17 238 部署 P0 教训）：**只挂 public 组单份**——分发面是静态构建产物且需
// 对 agent 公开（agent 无登录态）；registry 不含租户数据，下载走 manifest 白名单。
func SetupSkillsRoutes(public *gin.RouterGroup, auth *gin.RouterGroup) {
	_ = auth // 保留签名兼容；分发面只挂 public（agent 公开访问，见上注）
	public.GET("/skills-dist/registry", skillsRegistryHandler)
	public.GET("/skills-dist/manifest", skillsManifestFlatHandler)
	public.GET("/skills-dist/install.md", skillsInstallDocHandler)
	public.GET("/skills-dist/installer", skillsInstallerHandler)
	public.GET("/skills-dist/:name/download", skillsDownloadHandler)
}

// skillsManifest 读取并解析 dist 目录的 manifest.json。
func skillsManifest(c *gin.Context) (map[string]any, string, bool) {
	dir := skillsDistDir()
	if dir == "" {
		common.Success(c, gin.H{"configured": false, "reason": "SKILLS_DIST_DIR not set"})
		return nil, "", false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		common.Success(c, gin.H{"configured": false, "reason": "manifest.json not found; run scripts/package-skills.ps1"})
		return nil, "", false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		common.Fail(c, common.InternalErrorCode, "manifest.json 解析失败: "+err.Error())
		return nil, "", false
	}
	return m, dir, true
}

func skillsRegistryHandler(c *gin.Context) {
	m, dir, ok := skillsManifest(c)
	if !ok {
		return
	}
	_ = dir
	common.Success(c, m)
}

// skillsManifestFlatHandler 返回**裸清单**（dist/manifest.json 原字节，不带信封）。
//
// 为什么需要第二个清单端点（2026-10-17 修复）：
// /registry 走 common.Success 包装成 {code,message,data}，而零依赖安装器按顶层
// `skills` 取数组。把 /registry 原样喂给安装器时数组取到 nil，安装器不报错、
// 正常退出、但一个技能都没装——「假成功」比报错更难排查。
//
// 这里提供与磁盘文件字节一致的清单，安装器/CI 可直接消费；/registry 保留给
// 前端资源页等需要统一信封的调用方，两者数据同源、不会分叉。
func skillsManifestFlatHandler(c *gin.Context) {
	dir := skillsDistDir()
	if dir == "" {
		common.Fail(c, common.NotFoundCode, "SKILLS_DIST_DIR not configured")
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		common.Fail(c, common.NotFoundCode, "manifest.json not found; run scripts/package-skills.ps1")
		return
	}
	if !json.Valid(raw) {
		common.Fail(c, common.InternalErrorCode, "manifest.json 解析失败")
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

func skillsInstallerHandler(c *gin.Context) {
	dir := skillsDistDir()
	if dir == "" {
		common.Fail(c, common.NotFoundCode, "SKILLS_DIST_DIR not configured")
		return
	}
	serveSkillsFile(c, filepath.Join(dir, "itsm-skills-install.py"), "text/x-python; charset=utf-8")
}

// skillsInstallDocHandler 动态生成 Agent 安装说明（Markdown）。
// 目标读者是 code agent 本身：给它一个本 URL，它 self-fetch 本文档即可按步骤完成
// 清单获取 → 校验 → 解压到自身技能目录。文档内嵌本机绝对 URL，无需人工改写。
func skillsInstallDocHandler(c *gin.Context) {
	m, _, ok := skillsManifest(c)
	if !ok {
		return
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + c.Request.Host + "/api/v1/skills-dist"

	var b strings.Builder
	b.WriteString("# ITSM Agent Skills 安装说明\n\n")
	b.WriteString("> 面向 code agent（Claude Code / Codex / aicli / WorkBuddy 等）。本文档自描述：按下列步骤即可完成全部 ITSM 技能的安装。\n\n")
	if commit, ok := m["sourceCommit"].(string); ok && commit != "" {
		b.WriteString(fmt.Sprintf("- 构建版本：`%s`\n", commit[:min(8, len(commit))]))
	}
	if generatedAt, ok := m["generatedAt"].(string); ok {
		b.WriteString(fmt.Sprintf("- 打包时间：`%s`\n", generatedAt))
	}
	if schema, ok := m["schemaVersion"].(float64); ok {
		b.WriteString(fmt.Sprintf("- 清单协议：`schemaVersion=%d`\n", int(schema)))
	}
	b.WriteString(fmt.Sprintf("- 分发根：`%s`\n", base))

	// 端点一览：把 bundle 的下载地址显式写出来（此前只能靠猜，/itsm-skills-all.zip 与
	// /download-all 都是 404，唯一正确形态是 /bundle/download）。
	b.WriteString("\n## 0. 端点一览\n\n")
	b.WriteString("| 端点 | 用途 |\n|---|---|\n")
	b.WriteString(fmt.Sprintf("| `%s/manifest` | 技能清单（裸 JSON，无信封）——**脚本/CI 用这个** |\n", base))
	b.WriteString(fmt.Sprintf("| `%s/registry` | 同一份清单的统一信封版 `{code,message,data}`，供资源页等调用方 |\n", base))
	b.WriteString(fmt.Sprintf("| `%s/install.md` | 本文档 |\n", base))
	b.WriteString(fmt.Sprintf("| `%s/installer` | 零依赖安装器（Python 3.8+，仅标准库） |\n", base))
	b.WriteString(fmt.Sprintf("| `%s/<skill>/download` | 单技能 zip |\n", base))
	b.WriteString(fmt.Sprintf("| `%s/bundle/download` | 全量 zip（全部技能打成一个包） |\n", base))

	// 清单
	b.WriteString("\n## 1. 获取技能清单\n\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -fsSL %s/manifest -o manifest.json\n", base))
	b.WriteString("```\n\n")
	b.WriteString("manifest.json 含每个技能的 `name/version/sha256/sizeBytes` 与全量包 `bundle` 信息。\n\n")
	b.WriteString("> **不要用 `/registry` 喂安装器**：它的顶层是 `code/message/data`，技能数组在 `data` 下，直取顶层 `skills` 会拿到空值——安装器不报错、正常退出、但一个都没装。\n")

	// 技能列表
	if items, ok := m["skills"].([]any); ok && len(items) > 0 {
		b.WriteString("\n## 2. 可用技能\n\n")
		b.WriteString("| 技能 | 版本 | 大小 | 下载 URL |\n|---|---|---|---|\n")
		for _, item := range items {
			e, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := e["name"].(string)
			version, _ := e["version"].(string)
			size, _ := e["sizeBytes"].(float64)
			b.WriteString(fmt.Sprintf("| %s | %s | %.1f KB | %s/%s/download |\n",
				name, version, size/1024, base, name))
		}
	}

	// 安装方式三选一
	b.WriteString("\n## 3. 安装\n\n")
	b.WriteString("### 方式 A：直接解压（无外部依赖）\n\n")
	b.WriteString("每个 zip 即为完整技能目录（SKILL.md + references/ + scripts/ + _lib/）。\n\n")
	b.WriteString("```bash\n")
	b.WriteString("# 逐个下载并解压到你的 agent 技能目录 <SKILLS_ROOT>（由 agent 自身约定，例如 Claude Code 为 ~/.claude/skills）\n")
	if items, ok := m["skills"].([]any); ok {
		for _, item := range items {
			e, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := e["name"].(string); ok {
				b.WriteString(fmt.Sprintf("curl -fsSL %s/%s/download -o %s.zip && mkdir -p <SKILLS_ROOT>/%s && unzip -o %s.zip -d <SKILLS_ROOT>/%s\n",
					base, name, name, name, name, name))
			}
		}
	}
	b.WriteString("```\n\n")

	b.WriteString("### 方式 B：零依赖安装器（sha256 校验 + 幂等覆盖 + 内建自检）\n\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -fsSL %s/installer -o itsm-skills-install.py\n", base))
	b.WriteString(fmt.Sprintf("python itsm-skills-install.py --base %s --all --skills-root <SKILLS_ROOT>\n", base))
	b.WriteString("```\n\n")
	b.WriteString("`--skills-root` 接受任意绝对路径，优先于 `--host`。内置的宿主映射仅供参考：\n\n")
	if roots, ok := m["hostRoots"].(map[string]any); ok && len(roots) > 0 {
		b.WriteString("| `--host` | 默认技能根 |\n|---|---|\n")
		for _, key := range []string{"claude", "codex", "project"} {
			if v, ok := roots[key].(string); ok && v != "" {
				b.WriteString(fmt.Sprintf("| `%s` | `%s` |\n", key, v))
			}
		}
	} else {
		b.WriteString("| `--host` | 默认技能根 |\n|---|---|\n")
		b.WriteString("| `claude` | `~/.claude/skills` |\n")
		b.WriteString("| `codex` | `~/.aicli/.agents/skills` |\n")
		b.WriteString("| `project` | `<项目根>/.agents/skills` |\n")
	}
	b.WriteString("\n宿主不在上表内（如 WorkBuddy 的 `~/.workbuddy-ai/skills`）时**不要**用 `--host` 凑路径，直接 `--skills-root` 指定，避免技能装到了宿主不扫描的目录。\n\n")
	b.WriteString("安装器支持 `--check`（版本对比）、`--verify`（安装态完整性）、`--name name@version`（版本锁定）、`--rollback`（回滚）。安装结束会自动跑一遍自检（SKILL.md / `_lib` / 每个 CLI 的 `--help`），任何一项不过就以非零码退出——「装完」与「验收通过」是同一件事。\n\n")

	b.WriteString("### 方式 C：全量包（一次拿全部）\n\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -fsSL %s/bundle/download -o itsm-skills-all.zip\n", base))
	b.WriteString("unzip -o itsm-skills-all.zip -d <SKILLS_ROOT>\n")
	b.WriteString("```\n\n")
	b.WriteString("> 全量包与单包的 `_lib` 布局不同：单包把 `_lib/` 内嵌在技能目录里，全量包把 `_lib/` 提到归档根（8 个技能共享一份）。技能脚本已对两种布局做双路径兜底，但自定义脚本若只写死单一路径，换形态就会导入失败。\n")

	// 校验
	b.WriteString("\n## 4. 校验\n\n")
	b.WriteString("- sha256：对照 manifest 中每条 `sha256` 字段；下载响应的 `X-SHA256` 头是服务端重算值，可与 manifest 交叉验证（防代理截断/脏缓存）。\n")
	b.WriteString("- 结构：解压后目录必须含 `SKILL.md`；含 `scripts/` 的技能必须含 `_lib/itsm_common.py`（全量包布局下 `_lib` 在技能根上一级）。\n")
	b.WriteString("- 数量：`ls -d <SKILLS_ROOT>/itsm-* | wc -l` 应等于清单里的技能数。\n")
	b.WriteString("- 运行：`python <SKILLS_ROOT>/<skill>/scripts/<cli>.py --help`。`--help` 不联网、不需令牌，却能一次验证解释器可用、依赖齐全、参数定义正确、共享库导入成功——是安装后最划算的验收手段。\n")

	// 升级与回滚
	b.WriteString("\n## 5. 升级与回滚\n\n")
	b.WriteString("```bash\n")
	b.WriteString("python itsm-skills-install.py --base <BASE> --all --skills-root <SKILLS_ROOT> --check    # 版本对比（dry-run）\n")
	b.WriteString("python itsm-skills-install.py --skills-root <SKILLS_ROOT> --verify                       # 安装态完整性\n")
	b.WriteString("python itsm-skills-install.py --skills-root <SKILLS_ROOT> --rollback <skill-name>        # 回滚到上一版\n")
	b.WriteString("```\n\n")
	b.WriteString("版本变化时安装器会先把旧版打包到 `<SKILLS_ROOT>/.skills-backup/`；`--verify` 比对的是安装时记录的内容摘要，差异即表示本地被改动过。\n")

	// 使用前提
	b.WriteString("\n## 6. 使用前提\n\n")
	b.WriteString("- 目标 ITSM 环境可达（示例 base：`http://172.18.3.238:8089/api/v1`）。\n")
	b.WriteString("- Python 3.8+（CLI 仅标准库）。\n")
	b.WriteString("- 具备相应业务权限码的账号（PAT 或登录 JWT，CLI 内 `login` 子命令可换取）。\n")

	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	c.String(http.StatusOK, b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func skillsDownloadHandler(c *gin.Context) {
	m, dir, ok := skillsManifest(c)
	if !ok {
		return
	}
	// 静态别名 /skills-dist/bundle/download 的路径参数优先级：gin 中 ":name" 静态段
	// "bundle" 同级共存时，"/bundle/download" 命中 ":name/download"（name="bundle"）。
	// 统一在 handler 内解析别名，路由只保留 ":name/download" 一条。
	name := c.Param("name")
	if name == "" || name == "bundle" {
		name = "itsm-skills-all"
	}
	allowed := map[string]bool{}
	if bundle, ok := m["bundle"].(map[string]any); ok {
		if f, ok := bundle["file"].(string); ok {
			allowed[f] = true
		}
	}
	if items, ok := m["skills"].([]any); ok {
		for _, item := range items {
			if e, ok := item.(map[string]any); ok {
				if f, ok := e["file"].(string); ok {
					allowed[f] = true
				}
			}
		}
	}
	fileName := name + ".zip"
	if !allowed[fileName] {
		// bundle 经 gin 参数匹配时 ":name" 会吞掉 "bundle/download" 的斜杠前缀段之外的
		// 静态别名路由顺序差异：直接按文件名再兜底一次 itsm-skills-all.zip
		if fileName == "bundle.zip" && allowed["itsm-skills-all.zip"] {
			fileName = "itsm-skills-all.zip"
		} else {
			common.Fail(c, common.NotFoundCode, "分发包不存在: "+fileName)
			return
		}
	}
	serveSkillsFile(c, filepath.Join(dir, fileName), "application/zip")
}

// serveSkillsFile 校验文件在 dist 目录内且大小合规后下发（Content-Disposition attachment）。
//
// 顺序约束（2026-10-17 修复）：HTTP 响应头只能在写 body **之前**设置——一旦
// io.Copy 把首字节刷给客户端，状态行与头部就已经定稿，之后再调 c.Header 只会写进
// 已废弃的 header map。此前实现先 Copy 再设头，导致真实响应里 Content-Disposition /
// X-SHA256 全部丢失（Content-Type 靠 Go 的内容嗅探碰巧还是 application/zip），
// 而 httptest 的 Header() 返回的是可变 map，单测因此一直是绿的。
// 现改为：整文件先算摘要 → Seek 回起点 → 设头 → 再流式写出。
func serveSkillsFile(c *gin.Context, fullPath, contentType string) {
	dir := skillsDistDir()
	clean := path.Clean(strings.ReplaceAll(fullPath, `\`, `/`))
	absClean := filepath.Clean(clean)
	if !strings.HasPrefix(absClean, filepath.Clean(dir)+string(os.PathSeparator)) {
		common.Fail(c, common.NotFoundCode, "非法路径")
		return
	}
	f, err := os.Open(absClean)
	if err != nil {
		common.Fail(c, common.NotFoundCode, "文件不存在")
		return
	}
	defer f.Close()
	// 上限 100MB（当前产物 <1MB，防御性上限）
	const maxSkillsAssetBytes = 100 << 20
	fi, err := f.Stat()
	if err != nil || fi.IsDir() || fi.Size() > maxSkillsAssetBytes {
		common.Fail(c, common.NotFoundCode, "文件不可用")
		return
	}

	// 摘要必须在写 body 前算完：X-SHA256 是安装器做「下载完整性」校验的依据，
	// 与 manifest.sha256 互为交叉验证（防代理截断/缓存脏数据）。
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		common.Fail(c, common.NotFoundCode, "文件读取失败")
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		common.Fail(c, common.NotFoundCode, "文件读取失败")
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `attachment; filename="`+path.Base(absClean)+`"`)
	c.Header("Content-Length", strconv.FormatInt(fi.Size(), 10))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-SHA256", hex.EncodeToString(sum.Sum(nil)))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, f); err != nil {
		return
	}
}
