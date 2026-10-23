package admin

import (
	"encoding/json"
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
	})
}

// AdminAuth 校验管理员身份：静态 token 或 role=admin 的会话。
func (s *Service) AdminAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := yggdrasil.BearerToken(r)
			if token == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if s.adminToken != "" && token == s.adminToken {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := s.store.Validate(r.Context(), token, "")
			if err == nil {
				if role, err := s.store.UserRole(r.Context(), sess.UserID); err == nil && role == "admin" {
					next.ServeHTTP(w, r)
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
	writeJSON(w, http.StatusCreated, map[string]any{"codes": codes})
}

func (s *Service) handleDeleteSkinLib(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteSkinLibItem(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if err := s.DeleteComment(r.Context(), chi.URLParam(r, "cid")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
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
