package admin

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
)

// RegisterRoutes 注册管理后台路由。
func (s *Service) RegisterRoutes(r chi.Router) {
	// 公开：登录方式 + 公告
	r.Get("/api/login-methods", s.handleLoginMethods)
	r.Get("/api/announcement", s.handleAnnouncement)

	// 管理员
	r.Group(func(r chi.Router) {
		r.Use(s.AdminAuth())
		r.Get("/api/admin/users", s.handleListUsers)
		r.Patch("/api/admin/users/{id}", s.handleUpdateUser)
		r.Get("/api/admin/login-methods", s.handleLoginMethods)
		r.Put("/api/admin/login-methods", s.handleUpdateLoginMethods)
		r.Get("/api/admin/announcement", s.handleAnnouncement)
		r.Put("/api/admin/announcement", s.handleSetAnnouncement)
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

// actorKey 是审计 actor 的 context 键。
type actorKey struct{}

// ActorFrom 返回管理操作者标识（"token" 或 "user:<id>"）。
func ActorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorKey{}).(string); ok {
		return v
	}
	return "unknown"
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
	entries, total, err := s.store.ListAudit(r.Context(), intQuery(r, "limit", 50), intQuery(r, "offset", 0))
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
func (s *Service) AdminAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := yggdrasil.BearerToken(r)
			if token == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if s.adminToken != "" && token == s.adminToken {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, "token")))
				return
			}
			sess, err := s.store.Validate(r.Context(), token, "")
			if err == nil {
				if role, err := s.store.UserRole(r.Context(), sess.UserID); err == nil && role == "admin" {
					ctx := context.WithValue(r.Context(), actorKey{}, "user:"+sess.UserID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
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
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
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
	if err := s.SetAnnouncement(r.Context(), a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "admin.announcement.set", "", "enabled="+strconv.FormatBool(a.Enabled))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleInviteCodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
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
