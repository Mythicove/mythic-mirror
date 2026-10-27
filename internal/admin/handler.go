package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// validate 管理端请求体校验（角色/状态/枚举/长度），避免脏值入库。
var validate = validator.New()

// RegisterRoutes 注册管理后台路由。
func (s *Service) RegisterRoutes(r chi.Router) {
	// 公开：登录方式 + 公告 + 品牌图标 + 站点信息/自定义资产
	r.Get("/api/login-methods", s.handleLoginMethods)
	r.Get("/api/announcement", s.handleAnnouncement)
	r.Get("/api/brand", s.handleBrand)
	r.Get("/api/brand/logo/{variant}", s.handleBrandLogo)
	r.Get("/api/site", s.handleSite)
	r.Get("/site/custom.css", s.handleSiteCSS)
	r.Get("/site/custom.js", s.handleSiteJS)

	// 管理员
	r.Group(func(r chi.Router) {
		r.Use(s.AdminAuth())
		r.Get("/api/admin/users", s.handleListUsers)
		r.Patch("/api/admin/users/{id}", s.handleUpdateUser)
		r.Get("/api/admin/login-methods", s.handleLoginMethods)
		r.Put("/api/admin/login-methods", s.handleUpdateLoginMethods)
		r.Get("/api/admin/auth-policy", s.handleAuthPolicy)
		r.Put("/api/admin/auth-policy", s.handleUpdateAuthPolicy)
		r.Get("/api/admin/announcement", s.handleAnnouncement)
		r.Put("/api/admin/announcement", s.handleSetAnnouncement)
		r.Get("/api/admin/site", s.handleGetSiteAdmin)
		r.Put("/api/admin/site", s.handleSetSite)
		r.Get("/api/admin/brand", s.handleBrand)
		r.Put("/api/admin/brand/logo/{variant}", s.handleSetBrandLogo)
		r.Delete("/api/admin/brand/logo/{variant}", s.handleDeleteBrandLogo)
		r.Post("/api/admin/invite-codes", s.handleInviteCodes)
		r.Delete("/api/admin/skinlib/{id}", s.handleDeleteSkinLib)
		r.Delete("/api/admin/comments/{cid}", s.handleDeleteComment)
		r.Get("/api/admin/audit", s.handleListAudit)
		r.Get("/api/admin/reports", s.handleListReports)
		r.Post("/api/admin/reports/{id}/resolve", s.handleResolveReport)
		r.Get("/api/admin/skins", s.handleAdminListSkins)
		r.Post("/api/admin/skins/{id}/status", s.handleSetSkinStatus)
	})
}

// audit 记录一条管理操作审计；写失败只记日志，不影响请求结果。
func (s *Service) audit(r *http.Request, action, target, detail string) {
	e := yggdrasil.AuditEntry{
		Actor:   ActorFrom(r.Context()),
		ActorIP: yggdrasil.ClientIP(r),
		Action:  action,
		Target:  target,
		Detail:  detail,
	}
	if err := s.store.WriteAudit(r.Context(), e); err != nil {
		log.Printf("admin: write audit %s: %v", action, err)
	}
}

func (s *Service) handleListAudit(w http.ResponseWriter, r *http.Request) {
	entries, total, err := s.store.ListAudit(r.Context(), r.URL.Query().Get("q"), intQuery(r, "limit", 50), intQuery(r, "offset", 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": total})
}

// handleListReports 列出举报（status 过滤：pending|dismissed|acted，空=全部）。
func (s *Service) handleListReports(w http.ResponseWriter, r *http.Request) {
	reports, total, err := s.store.ListReports(r.Context(), r.URL.Query().Get("status"), intQuery(r, "limit", 50), intQuery(r, "offset", 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "total": total})
}

// handleResolveReport 标记举报处理结果；删除目标内容复用 DELETE /api/admin/skinlib|comments 端点。
func (s *Service) handleResolveReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // dismissed | acted
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "dismissed" && req.Status != "acted") {
		http.Error(w, "status must be dismissed or acted", http.StatusBadRequest)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.ResolveReport(r.Context(), id, ActorFrom(r.Context()), req.Status); err != nil {
		if err == yggdrasil.ErrNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.report.resolve", id, req.Status)
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminListSkins 按状态列出皮肤条目（status 空=全部）。
func (s *Service) handleAdminListSkins(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.store.AdminListSkins(r.Context(), r.URL.Query().Get("status"), intQuery(r, "limit", 50), intQuery(r, "offset", 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// handleSetSkinStatus 设置皮肤条目状态（listed|unlisted|under_review）。
func (s *Service) handleSetSkinStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "listed" && req.Status != "unlisted" && req.Status != "under_review") {
		http.Error(w, "status must be listed, unlisted or under_review", http.StatusBadRequest)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.SetSkinStatus(r.Context(), id, req.Status); err != nil {
		if err == yggdrasil.ErrNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.skin.status", id, req.Status)
	w.WriteHeader(http.StatusNoContent)
}

// AdminAuth 校验管理员身份：静态 token 或 role=admin 的会话；并把 actor 注入 context 供审计。
// 实现收敛到 yggdrasil.AdminRole，与其它包的后台端点共用同一套语义。
func (s *Service) AdminAuth() func(http.Handler) http.Handler {
	return yggdrasil.AdminRole(s.store, s.adminToken)
}

// ActorFrom 返回管理操作者标识（"token" 或 "user:<id>"）。
func ActorFrom(ctx context.Context) string {
	return yggdrasil.AdminActor(ctx)
}

func (s *Service) handleLoginMethods(w http.ResponseWriter, r *http.Request) {
	methods, err := s.LoginMethods(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"methods": methods})
}

func (s *Service) handleUpdateLoginMethods(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Methods []LoginMethod `json:"methods"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.UpdateLoginMethods(r.Context(), req.Methods); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.login_methods.update", "", strconv.Itoa(len(req.Methods))+" methods")
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthPolicy 返回认证策略与"还有多少历史纯密码账号"的提示计数。
func (s *Service) handleAuthPolicy(w http.ResponseWriter, r *http.Request) {
	view, err := s.GetAuthPolicyView(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleUpdateAuthPolicy 更新强制绑定开关与玩家数上限。
// 主密码开关不在此处：它归 login_methods，一个开关只有一个入口。
func (s *Service) handleUpdateAuthPolicy(w http.ResponseWriter, r *http.Request) {
	var p yggdrasil.AuthPolicy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.UpdateAuthPolicy(r.Context(), p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.auth_policy.update", "",
		"require_binding="+strconv.FormatBool(p.RequireBinding)+" max_players="+strconv.Itoa(p.MaxPlayers))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := intQuery(r, "limit", 20)
	offset := intQuery(r, "offset", 0)
	users, total, err := s.ListUsers(r.Context(), q, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "total": total})
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role   string `json:"role" validate:"required,oneof=user admin"`
		Status string `json:"status" validate:"required,oneof=active banned suspended"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validate.Struct(req); err != nil {
		http.Error(w, "role must be user|admin and status must be active|banned|suspended", http.StatusBadRequest)
		return
	}
	if err := s.UpdateUser(r.Context(), chi.URLParam(r, "id"), req.Role, req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "admin.user.update", chi.URLParam(r, "id"), "role="+req.Role+" status="+req.Status)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, err := s.Announcement(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Service) handleSetAnnouncement(w http.ResponseWriter, r *http.Request) {
	var a Announcement
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validate.Struct(a); err != nil {
		http.Error(w, "title/body too long", http.StatusBadRequest)
		return
	}
	if err := s.SetAnnouncement(r.Context(), a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.announcement.set", "", "enabled="+strconv.FormatBool(a.Enabled))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleInviteCodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count int `json:"count" validate:"min=1,max=100"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validate.Struct(req); err != nil {
		http.Error(w, "count must be between 1 and 100", http.StatusBadRequest)
		return
	}
	codes, err := s.GenerateInviteCodes(r.Context(), req.Count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "admin.invite.create", "", "count="+strconv.Itoa(len(codes)))
	writeJSON(w, http.StatusCreated, map[string]any{"codes": codes})
}

func (s *Service) handleDeleteSkinLib(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteSkinLibItem(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.audit(r, "admin.skinlib.delete", chi.URLParam(r, "id"), "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteComment(r.Context(), chi.URLParam(r, "cid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.audit(r, "admin.comment.delete", chi.URLParam(r, "cid"), "")
	w.WriteHeader(http.StatusNoContent)
}

func intQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleBrand 公开返回品牌图标配置（哪些变体被自定义 + 版本号），前端据此拼图标 URL。
func (s *Service) handleBrand(w http.ResponseWriter, r *http.Request) {
	c, err := s.Brand(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleBrandLogo 公开返回某变体的自定义图标字节；未自定义时 404（前端回落内置默认图）。
func (s *Service) handleBrandLogo(w http.ResponseWriter, r *http.Request) {
	v, err := ParseBrandVariant(chi.URLParam(r, "variant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := s.BrandLogo(r.Context(), v)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// 版本号由调用方以 ?v= 携带；同版本内容不变，可长期缓存。
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleSetBrandLogo 管理员上传某变体品牌图标（请求体为原始 PNG 字节）。
func (s *Service) handleSetBrandLogo(w http.ResponseWriter, r *http.Request) {
	v, err := ParseBrandVariant(chi.URLParam(r, "variant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxBrandLogoBytes+1))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.SetBrandLogo(r.Context(), v, data); err != nil {
		switch {
		case errors.Is(err, ErrInvalidBrandLogo):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrBrandStorage):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.audit(r, "admin.brand.logo_set", string(v), "")
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteBrandLogo 管理员移除某变体自定义图标（回落默认）。
func (s *Service) handleDeleteBrandLogo(w http.ResponseWriter, r *http.Request) {
	v, err := ParseBrandVariant(chi.URLParam(r, "variant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.DeleteBrandLogo(r.Context(), v); err != nil {
		if errors.Is(err, ErrBrandStorage) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.audit(r, "admin.brand.logo_delete", string(v), "")
	w.WriteHeader(http.StatusNoContent)
}

// sitePublicView 是公开站点视图：文本字段 + 自定义资产是否存在（前端据此决定是否挂 link/script）。
type sitePublicView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Copyright   string `json:"copyright"`
	ICP         string `json:"icp"`
	CustomCSS   bool   `json:"customCss"`
	CustomJS    bool   `json:"customJs"`
}

func (s *Service) handleSite(w http.ResponseWriter, r *http.Request) {
	info, _ := s.SiteInfo(r.Context())
	assets, _ := s.SiteAssets(r.Context())
	writeJSON(w, http.StatusOK, sitePublicView{
		Name: info.Name, Description: info.Description, Copyright: info.Copyright, ICP: info.ICP,
		CustomCSS: assets.CustomCSS != "", CustomJS: assets.CustomJS != "",
	})
}

// 自定义 CSS/JS 以同源文件下发（而非内联），使 script-src/style-src 'self' 的 CSP 下仍可用。
func (s *Service) handleSiteCSS(w http.ResponseWriter, r *http.Request) {
	a, _ := s.SiteAssets(r.Context())
	if a.CustomCSS == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(a.CustomCSS))
}

func (s *Service) handleSiteJS(w http.ResponseWriter, r *http.Request) {
	a, _ := s.SiteAssets(r.Context())
	if a.CustomJS == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(a.CustomJS))
}

type siteAdminView struct {
	Info   SiteInfo   `json:"info"`
	Assets SiteAssets `json:"assets"`
}

func (s *Service) handleGetSiteAdmin(w http.ResponseWriter, r *http.Request) {
	info, _ := s.SiteInfo(r.Context())
	assets, _ := s.SiteAssets(r.Context())
	writeJSON(w, http.StatusOK, siteAdminView{Info: info, Assets: assets})
}

func (s *Service) handleSetSite(w http.ResponseWriter, r *http.Request) {
	var v siteAdminView
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validate.Struct(v.Info); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validate.Struct(v.Assets); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.SetSiteInfo(r.Context(), v.Info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.SetSiteAssets(r.Context(), v.Assets); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.site.update", "", "")
	w.WriteHeader(http.StatusNoContent)
}
