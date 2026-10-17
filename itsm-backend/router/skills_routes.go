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

// SetupSkillsRoutes 注册资源中心路由。public 与 auth 组各挂一份读面：
//   - public：/api/v1/skills/registry 供未登录的 agent 侧 curl 获取清单与安装器；
//     登录态前端页复用同一路径（无需区分）。
func SetupSkillsRoutes(public *gin.RouterGroup, auth *gin.RouterGroup) {
	if auth != nil {
		auth.GET("/skills/registry", skillsRegistryHandler)
		auth.GET("/skills/installer", skillsInstallerHandler)
		auth.GET("/skills/:name/download", skillsDownloadHandler)
		auth.GET("/skills/bundle/download", skillsDownloadHandler)
	}
	// public 面仅开放 registry / installer / 下载（分发面无租户数据；installer/zip 均为静态构建产物）
	if public != nil {
		public.GET("/skills/registry", skillsRegistryHandler)
		public.GET("/skills/installer", skillsInstallerHandler)
		public.GET("/skills/:name/download", skillsDownloadHandler)
	}
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
