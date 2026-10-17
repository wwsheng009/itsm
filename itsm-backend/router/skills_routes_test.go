package router

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newSkillsTestRouter 构造带 public 组的测试路由（分发面公开挂载，见 skills_routes.go 注）。
func newSkillsTestRouter(t *testing.T, distDir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if distDir != "" {
		t.Setenv("SKILLS_DIST_DIR", distDir)
	} else {
		t.Setenv("SKILLS_DIST_DIR", "")
	}
	r := gin.New()
	pub := r.Group("/api/v1")
	SetupSkillsRoutes(pub, nil)
	return r
}

func writeFakeDist(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schemaVersion": 1,
		"sourceCommit":  "deadbeef",
		"skills": []any{
			map[string]any{"name": "itsm-cmdb-operations", "file": "itsm-cmdb-operations.zip", "sha256": "aa", "sizeBytes": 4},
			map[string]any{"name": "itsm-ticket-operations", "file": "itsm-ticket-operations.zip", "sha256": "bb", "sizeBytes": 4},
		},
		"bundle": map[string]any{"file": "itsm-skills-all.zip", "sha256": "cc", "sizeBytes": 4},
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"itsm-cmdb-operations.zip", "itsm-skills-all.zip", "itsm-skills-install.py"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("DATA"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillsRegistry_NotConfigured(t *testing.T) {
	r := newSkillsTestRouter(t, "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/registry", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200 got %d", w.Code)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Configured bool `json:"configured"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data.Configured {
		t.Fatalf("expect code=0 configured=false, got code=%d configured=%v body=%s", resp.Code, resp.Data.Configured, w.Body.String())
	}
}

func TestSkillsRegistry_Configured(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	r := newSkillsTestRouter(t, dir)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/registry", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200 got %d body=%s", w.Code, w.Body.String())
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatal("invalid json")
	}
}

func TestSkillsDownload_AllowedAndBlocked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	r := newSkillsTestRouter(t, dir)

	// 允许：manifest 登记的 zip
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/itsm-cmdb-operations/download", nil))
	if w.Code != http.StatusOK || w.Body.String() != "DATA" {
		t.Fatalf("expect 200 DATA got %d %q", w.Code, w.Body.String())
	}
	// 必须用 Result().Header 断言：ResponseRecorder.Header() 返回的是可变 map，
	// 写 body 之后再 Set 的头也能读到，正是这个盲点让「头写在 body 之后」的 bug
	// 一直没被单测发现（真实 HTTP 响应里那些头是丢的）。
	hdr := w.Result().Header
	if hdr.Get("X-SHA256") == "" {
		t.Fatal("missing X-SHA256 header")
	}
	if want := fmt.Sprintf("%x", sha256.Sum256([]byte("DATA"))); hdr.Get("X-SHA256") != want {
		t.Fatalf("X-SHA256 = %s, want %s", hdr.Get("X-SHA256"), want)
	}
	if cd := hdr.Get("Content-Disposition"); !strings.Contains(cd, "itsm-cmdb-operations.zip") {
		t.Fatalf("Content-Disposition 丢失或文件名不对: %q", cd)
	}
	if ct := hdr.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", ct)
	}
	// bundle 别名
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/bundle/download", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("bundle expect 200 got %d", w2.Code)
	}
	// 未登记 → 404（NotFoundCode 4004 映射 404）
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/not-registered/download", nil))
	if w3.Code != http.StatusNotFound {
		t.Fatalf("unregistered expect 404 got %d", w3.Code)
	}
}

// TestSkillsManifestFlat_NoEnvelope 锁定「裸清单」契约：顶层直接是 skills 数组，
// 不能被 {code,message,data} 包一层——安装器按顶层取数组，包了就是静默装 0 个。
func TestSkillsManifestFlat_NoEnvelope(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	r := newSkillsTestRouter(t, dir)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/manifest", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("manifest expect 200 got %d body=%s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, wrapped := raw["code"]; wrapped {
		t.Fatal("裸清单不应包含 code 字段（说明被统一信封包装了）")
	}
	if _, wrapped := raw["data"]; wrapped {
		t.Fatal("裸清单不应包含 data 字段")
	}
	skills, ok := raw["skills"].([]any)
	if !ok || len(skills) != 2 {
		t.Fatalf("顶层 skills 应为 2 条，got %v", raw["skills"])
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestSkillsManifestFlat_NotConfigured(t *testing.T) {
	r := newSkillsTestRouter(t, "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/manifest", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未配置 SKILLS_DIST_DIR 时裸清单应 404，got %d", w.Code)
	}
}

// TestSkillsDownload_RealServerHeaders 走真实 HTTP 往返验证响应头。
//
// 与 Recorder 版互补：Recorder 的 HeaderMap 在写 body 之后仍可写，无法暴露
// 「先写 body 后设头」这类 bug；这里用 httptest.NewServer 起真服务，客户端读到的
// 就是线上会收到的头。
func TestSkillsDownload_RealServerHeaders(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	srv := httptest.NewServer(newSkillsTestRouter(t, dir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/skills-dist/itsm-cmdb-operations/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expect 200 got %d", resp.StatusCode)
	}
	if got, want := resp.Header.Get("X-SHA256"), fmt.Sprintf("%x", sha256.Sum256([]byte("DATA"))); got != want {
		t.Fatalf("X-SHA256 = %q, want %q", got, want)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "itsm-cmdb-operations.zip") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if resp.Header.Get("Content-Length") != "4" {
		t.Fatalf("Content-Length = %q, want 4", resp.Header.Get("Content-Length"))
	}
}

// TestSkillsDist_RealArtifacts 用仓库内真实打包产物（dist/skills）做一次端到端核对：
// 裸清单能取到、每个包的下载摘要与清单登记一致。产物不存在时跳过（CI 未跑打包步骤）。
func TestSkillsDist_RealArtifacts(t *testing.T) {
	distDir := filepath.Join("..", "..", "dist", "skills")
	if _, err := os.Stat(filepath.Join(distDir, "manifest.json")); err != nil {
		t.Skip("dist/skills/manifest.json 不存在（未执行 scripts/package-skills.ps1），跳过")
	}
	srv := httptest.NewServer(newSkillsTestRouter(t, distDir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/skills-dist/manifest")
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	defer resp.Body.Close()
	var m struct {
		Skills []struct {
			Name   string `json:"name"`
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"skills"`
		Bundle struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"bundle"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(m.Skills) == 0 {
		t.Fatal("清单里没有技能")
	}

	// 单包摘要与清单一致
	for _, s := range m.Skills {
		dl, err := http.Get(srv.URL + "/api/v1/skills-dist/" + s.Name + "/download")
		if err != nil {
			t.Fatalf("%s download failed: %v", s.Name, err)
		}
		body, _ := io.ReadAll(dl.Body)
		dl.Body.Close()
		if dl.StatusCode != http.StatusOK {
			t.Fatalf("%s download status %d", s.Name, dl.StatusCode)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != s.SHA256 {
			t.Fatalf("%s 摘要不一致：body=%s manifest=%s", s.Name, got, s.SHA256)
		}
		if hdr := dl.Header.Get("X-SHA256"); hdr != s.SHA256 {
			t.Fatalf("%s 响应头 X-SHA256=%s 与清单不一致", s.Name, hdr)
		}
	}

	// bundle 别名指向全量包
	bl, err := http.Get(srv.URL + "/api/v1/skills-dist/bundle/download")
	if err != nil {
		t.Fatalf("bundle download failed: %v", err)
	}
	bbody, _ := io.ReadAll(bl.Body)
	bl.Body.Close()
	if bl.StatusCode != http.StatusOK {
		t.Fatalf("bundle status %d", bl.StatusCode)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(bbody)); got != m.Bundle.SHA256 {
		t.Fatalf("bundle 摘要不一致：body=%s manifest=%s", got, m.Bundle.SHA256)
	}
	if cd := bl.Header.Get("Content-Disposition"); !strings.Contains(cd, m.Bundle.File) {
		t.Fatalf("bundle Content-Disposition = %q, want 含 %s", cd, m.Bundle.File)
	}
}

func TestSkillsInstaller_Served(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	r := newSkillsTestRouter(t, dir)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/installer", nil))
	if w.Code != http.StatusOK || w.Body.String() != "DATA" {
		t.Fatalf("installer expect 200 DATA got %d %q", w.Code, w.Body.String())
	}
}

func TestSkillsInstallDoc_PublicMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	writeFakeDist(t, dir)
	r := newSkillsTestRouter(t, dir)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills-dist/install.md", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("install.md expect 200 got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"# ITSM Agent Skills 安装说明",
		"/api/v1/skills-dist/registry",
		"itsm-cmdb-operations/download",
		"sha256",
		// 2026-10-17 补齐：全量包下载地址此前只能靠猜（/itsm-skills-all.zip 与
		// /download-all 都 404，唯一正确形态是 /bundle/download）
		"/api/v1/skills-dist/bundle/download",
		// 宿主不在内置映射表内时的正确姿势
		"--skills-root",
		"--check",
		"--rollback",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install.md missing %q", want)
		}
	}
	if strings.Contains(body, "itsm-skills-all.zip\n") && !strings.Contains(body, "bundle/download") {
		t.Fatal("install.md 不应把不存在的静态 zip 路径写成下载入口")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("expect text/markdown got %s", ct)
	}
}
