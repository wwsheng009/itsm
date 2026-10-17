package router

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
//   - /skills-dist/registry    清单
//   - /skills-dist/installer   安装器
//   - /skills-dist/:name/download / /skills-dist/bundle/download  分发包
// 仅挂 **auth 组单份**：gin 同一 engine 上两个 group 挂同一路径会在第二次注册时 panic
//（实测 v1.12.0：`handlers are already registered`——2026-10-17 238 部署 P0 教训：
// 本地单测 RouterConfig 全 nil + 只建 public 组，未触发双挂载场景）。安装器用户为
// 运维/开发（有账号）；前端资源中心页在登录态内复用同一路径，不受影响。
func SetupSkillsRoutes(public *gin.RouterGroup, auth *gin.RouterGroup) {
	_ = public // 保留签名兼容；分发面只挂 auth（见上注）
	auth.GET("/skills-dist/registry", skillsRegistryHandler)
	auth.GET("/skills-dist/installer", skillsInstallerHandler)
	auth.GET("/skills-dist/:name/download", skillsDownloadHandler)
	auth.GET("/skills-dist/bundle/download", skillsDownloadHandler)
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

func skillsDownloadHandler(c *gin.Context) {
	m, dir, ok := skillsManifest(c)
	if !ok {
		return
	}
	name := c.Param("name")
	if name == "bundle" {
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
		common.Fail(c, common.NotFoundCode, "分发包不存在: "+fileName)
		return
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
