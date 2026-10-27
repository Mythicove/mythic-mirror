package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
)

func newSiteService() *Service {
	return NewService(yggdrasil.NewMemoryStore(), "admintoken", nil)
}

func TestSiteInfoAndAssets(t *testing.T) {
	svc := newSiteService()
	r := chi.NewRouter()
	svc.RegisterRoutes(r)

	// 未设置：custom.css/js 404，/api/site 全空
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/site/custom.css", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("css before set: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/site", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"customCss":false`) {
		t.Fatalf("public site before set: %d %s", rr.Code, rr.Body.String())
	}

	// 管理端设置
	body := `{"info":{"name":"My Station","copyright":"(c) me","icp":"京ICP备1号"},"assets":{"customCss":"body{color:red}","customJs":"console.log(1)"}}`
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/site", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admintoken")
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("admin put: %d %s", rr.Code, rr.Body.String())
	}

	// 公开端点反映
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/site", nil))
	if !strings.Contains(rr.Body.String(), `"customCss":true`) || !strings.Contains(rr.Body.String(), `京ICP备1号`) {
		t.Fatalf("public site after set: %s", rr.Body.String())
	}
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/site/custom.css", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "body{color:red}" || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css after set: %d %q ct=%q", rr.Code, rr.Body.String(), rr.Header().Get("Content-Type"))
	}
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/site/custom.js", nil))
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("js after set: %d ct=%q", rr.Code, rr.Header().Get("Content-Type"))
	}

	// 管理读回
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/site", nil)
	req.Header.Set("Authorization", "Bearer admintoken")
	r.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "My Station") {
		t.Fatalf("admin get: %s", rr.Body.String())
	}
}
