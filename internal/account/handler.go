package account

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// RegisterRoutes 注册账户相关路由到 chi 路由器。
func (s *Service) RegisterRoutes(r chi.Router) {
	r.Post("/account/register", s.handleRegister)
	r.Post("/account/verify", s.handleVerify)
	r.Post("/account/forgot-password", s.handleForgot)
	r.Post("/account/reset-password", s.handleReset)
	r.Get("/oauth/{provider}/auth", s.handleOAuthAuth)
	r.Get("/oauth/{provider}/callback", s.handleOAuthCallback)
	r.Get("/mojang/oauth/callback", s.handleMojangOAuthCallback)
	r.Get("/skinlib", s.handleListSkinLib)
	r.Post("/skinlib/{id}/like", s.handleLikeSkin)

	// 需认证
	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.Auth(s.store))
		r.Post("/account/players", s.handleAddPlayer)
		r.Post("/skinlib", s.handlePublishSkin)
		r.Post("/skinlib/{id}/comment", s.handleCommentSkin)
		r.Get("/mojang/oauth/auth", s.handleMojangOAuthAuth)
	})

	// 管理员
	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.Admin(s.opts.AdminToken))
		r.Post("/admin/batch-import", s.handleBatchImport)
	})
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	resp, err := s.Register(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, yggdrasil.ErrAlreadyExists):
			writeErr(w, http.StatusConflict, s.t("account.error.conflict", nil))
		case errors.Is(err, ErrInvalidEmail):
			writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_email", nil))
		case errors.Is(err, ErrWeakPassword):
			writeErr(w, http.StatusBadRequest, s.t("account.error.weak_password", nil))
		case errors.Is(err, ErrInvalidName):
			writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_name", nil))
		case errors.Is(err, ErrNameTaken):
			writeErr(w, http.StatusBadRequest, s.t("account.error.name_taken", nil))
		case errors.Is(err, ErrRegistrationDisabled):
			writeErr(w, http.StatusForbidden, s.t("account.error.registration_disabled", nil))
		case errors.Is(err, ErrCaptchaFailed):
			writeErr(w, http.StatusBadRequest, s.t("account.error.captcha_failed", nil))
		case errors.Is(err, ErrInviteRequired), errors.Is(err, ErrInvalidInvite):
			writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_invite", nil))
		default:
			writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		}
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := s.VerifyEmail(r.Context(), req.Token); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_token", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleForgot(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	_ = s.ForgotPassword(r.Context(), req)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleReset(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := s.ResetPassword(r.Context(), req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_token", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleAddPlayer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name" validate:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	p, err := s.AddPlayer(r.Context(), req.Name)
	if err != nil {
		switch {
		case errors.Is(err, yggdrasil.ErrAlreadyExists):
			writeErr(w, http.StatusConflict, s.t("account.error.name_taken", nil))
		case errors.Is(err, ErrInvalidName):
			writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_name", nil))
		default:
			writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		}
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Service) handleOAuthAuth(w http.ResponseWriter, r *http.Request) {
	url, err := s.OAuthAuthURL(chi.URLParam(r, "provider"), r.URL.Query().Get("state"))
	if err != nil {
		writeErr(w, http.StatusNotFound, s.t("account.error.unknown_provider", nil))
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Service) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	token, err := s.OAuthLogin(r.Context(), chi.URLParam(r, "provider"), r.URL.Query().Get("code"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.unknown_provider", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"accessToken": token})
}

func (s *Service) handleMojangOAuthAuth(w http.ResponseWriter, r *http.Request) {
	url, err := s.MojangOAuthAuthURL(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, s.t("account.error.unauthorized", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Service) handleMojangOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if err := s.MojangOAuthCallback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code")); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.internal", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handlePublishSkin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerName  string `json:"playerName"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	item, err := s.PublishSkin(r.Context(), req.PlayerName, req.Description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Service) handleListSkinLib(w http.ResponseWriter, r *http.Request) {
	items, err := s.ListSkinLib(r.Context(), intQuery(r, "offset", 0), intQuery(r, "limit", 20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Service) handleLikeSkin(w http.ResponseWriter, r *http.Request) {
	if err := s.LikeSkin(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeErr(w, http.StatusNotFound, s.t("account.error.internal", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleCommentSkin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := s.CommentSkin(r.Context(), chi.URLParam(r, "id"), req.Content); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.internal", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleBatchImport(w http.ResponseWriter, r *http.Request) {
	var req BatchImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	n, err := s.BatchImport(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
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

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
