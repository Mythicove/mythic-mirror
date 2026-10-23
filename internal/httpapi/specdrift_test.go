package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/account"
	"github.com/Mythicove/mythic-mirror/internal/admin"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/maillog"
	"github.com/Mythicove/mythic-mirror/internal/oidc"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
)

// specFile 定位 mirror OpenAPI 契约。多仓库布局下默认在 ../api/specs，
// CI 用 MYTHICOVE_API_SPECS 指定 checkout 路径；找不到则跳过（非失败）。
func specFile(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if env := os.Getenv("MYTHICOVE_API_SPECS"); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "../../../api/specs", "../../../../api/specs")
	for _, dir := range candidates {
		p := filepath.Join(dir, "mirror.openapi.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("mirror.openapi.yaml not found; set MYTHICOVE_API_SPECS to enable drift gate")
	return ""
}

// parseSpecPaths 用轻量行解析提取 spec 里的 "METHOD /path" 集合。
// 仅依赖本仓库契约的稳定缩进（path 2 空格、method 4 空格），无需 YAML 依赖。
func parseSpecPaths(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	methods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true,
		"delete": true, "head": true, "options": true,
	}
	ops := map[string]bool{}
	sc := bufio.NewScanner(f)
	inPaths := false
	curPath := ""
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimRight(line, " \t")
		if trim == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		content := strings.TrimSpace(line)
		if indent == 0 {
			inPaths = trim == "paths:"
			curPath = ""
			continue
		}
		if !inPaths {
			continue
		}
		if indent == 2 && strings.HasPrefix(content, "/") && strings.HasSuffix(content, ":") {
			curPath = strings.TrimSuffix(content, ":")
			continue
		}
		if indent == 4 && curPath != "" {
			key := strings.TrimSuffix(content, ":")
			if methods[key] {
				ops[strings.ToUpper(key)+" "+curPath] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatalf("parsed 0 operations from %s (parser or spec format broken)", path)
	}
	return ops
}

func routerOps(t *testing.T) map[string]bool {
	t.Helper()
	dir := t.TempDir()
	kp, err := keys.LoadOrGenerate(filepath.Join(dir, "private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestorage.New(filepath.Join(dir, "textures"), "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	store := yggdrasil.NewMemoryStore()
	svc := yggdrasil.NewService(store, kp, files, yggdrasil.ServiceOptions{})
	acct := account.NewService(store, maillog.Mailer{}, "http://localhost")
	adminSvc := admin.NewService(store, "", nil)
	oidcSvc := oidc.NewProvider(store, kp, "http://localhost")
	if err := oidcSvc.ReloadClients(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(svc, acct, adminSvc, oidcSvc, Options{})
	r, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("NewRouter did not return *chi.Mux, got %T", h)
	}

	ops := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		ops[method+" "+route] = true
		return nil
	})
	return ops
}

// TestSpecMatchesRoutes 确保 OpenAPI 契约里记录的每个操作都真实注册在路由上，
// 防止 spec 漂移（如 /avatar 与 /avatars 之类的幽灵端点）。
func TestSpecMatchesRoutes(t *testing.T) {
	spec := parseSpecPaths(t, specFile(t))
	routes := routerOps(t)
	var missing []string
	for op := range spec {
		if !routes[op] {
			missing = append(missing, op)
		}
	}
	if len(missing) > 0 {
		t.Errorf("spec documents operations not present in router:\n  %s", strings.Join(missing, "\n  "))
	}
}
