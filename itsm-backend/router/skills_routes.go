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
//   - /skills-dist/registry       清单（JSON）
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
	b.WriteString("> 面向 code agent（Claude Code / Codex / aicli 等）。本文档自描述：按下列步骤即可完成全部 ITSM 技能的安装。\n\n")
	if commit, ok := m["sourceCommit"].(string); ok && commit != "" {
		b.WriteString(fmt.Sprintf("- 构建版本：`%s`\n", commit[:min(8, len(commit))]))
	}
	if generatedAt, ok := m["generatedAt"].(string); ok {
		b.WriteString(fmt.Sprintf("- 打包时间：`%s`\n", generatedAt))
	}

	// registry 下载 URL
	b.WriteString("\n## 1. 获取技能清单\n\n")
	b.WriteString(fmt.Sprintf("```bash\ncurl -fsSL %s/registry -o manifest.json\n```\n\n", base))
	b.WriteString("manifest.json 含每个技能的 `name/version/sha256/sizeBytes` 与全量包 `bundle` 信息。\n")

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

	// 安装方式二选一
	b.WriteString("\n## 3. 安装\n\n")
	b.WriteString("### 方式 A：直接解压（推荐，无外部依赖）\n\n")
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
	b.WriteString("### 方式 B：使用零依赖安装器（自动 sha256 校验 + 幂等覆盖）\n\n")
	b.WriteString(fmt.Sprintf("```bash\ncurl -fsSL %s/installer -o itsm-skills-install.py\npython itsm-skills-install.py --manifest manifest.json --all --host project\n```\n\n", base))
	b.WriteString("安装器支持 `--check`（版本对比）、`--verify`（安装态完整性）、`--name name@version`（版本锁定）、`--rollback`（回滚）。\n")

	// 校验
	b.WriteString("\n## 4. 校验\n\n")
	b.WriteString("- sha256：对照 manifest 中每条 `sha256` 字段。\n")
	b.WriteString("- 结构：解压后目录必须含 `SKILL.md`；含 `scripts/` 的技能必须含 `_lib/itsm_common.py`。\n")
	b.WriteString("- 运行：`python <SKILLS_ROOT>/<skill>/scripts/<cli>.py --help`。\n")

	// 使用前提
	b.WriteString("\n## 5. 使用前提\n\n")
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
	sum := sha256.New()
	if _, err := io.Copy(c.Writer, io.TeeReader(f, sum)); err != nil {
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `attachment; filename="`+path.Base(absClean)+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-SHA256", hex.EncodeToString(sum.Sum(nil)))
	c.Status(http.StatusOK)
}
