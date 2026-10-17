package router

import (
	"encoding/json"
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
	if w.Header().Get("X-SHA256") == "" {
		t.Fatal("missing X-SHA256 header")
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
	for _, want := range []string{"# ITSM Agent Skills 安装说明", "/api/v1/skills-dist/registry", "itsm-cmdb-operations/download", "sha256"} {
		if !strings.Contains(body, want) {
			t.Fatalf("install.md missing %q", want)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("expect text/markdown got %s", ct)
	}
}
