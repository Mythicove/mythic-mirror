package account

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

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
	r.Get("/api/register-requirements", s.handleRegisterRequirements)
	r.Get("/skinlib/{id}", s.handleGetSkinLib)
	r.Get("/skinlib/{id}/comments", s.handleListComments)
	r.Post("/skinlib/{id}/like", s.handleLikeSkin)

	// 需认证
	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.Auth(s.store))
		r.Post("/account/players", s.handleAddPlayer)
		r.Get("/account/players", s.handleListPlayers)
		r.Patch("/account/players/{name}", s.handleRenamePlayer)
		r.Delete("/account/players/{name}", s.handleDeletePlayer)
		r.Post("/account/players/{name}/activate", s.handleActivatePlayer)
		r.Delete("/account/players/{name}/premium", s.handleUnbindPremium)
		r.Post("/skinlib", s.handlePublishSkin)
		r.Post("/skinlib/{id}/comment", s.handleCommentSkin)
		r.Post("/reports", s.handleReport)
		r.Get("/mojang/oauth/auth", s.handleMojangOAuthAuth)
	})

	// 管理员（静态 token 或 role=admin 会话；与 /api/admin/* 同一套语义）
	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.AdminRole(s.store, s.opts.AdminToken))
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

// handleRenamePlayer 改名。改名会换 uuid 并使该玩家的游戏会话失效，故前端需提示重新登录。
func (s *Service) handleRenamePlayer(w http.ResponseWriter, r *http.Request) {
	old := chi.URLParam(r, "name")
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
	p, err := s.RenamePlayer(r.Context(), old, req.Name)
	if err != nil {
		s.writePlayerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePlayer 删除玩家（账号至少保留一个）。
func (s *Service) handleDeletePlayer(w http.ResponseWriter, r *http.Request) {
	if err := s.DeletePlayer(r.Context(), chi.URLParam(r, "name")); err != nil {
		s.writePlayerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleActivatePlayer 设为当前玩家（建会话时优先使用）。
func (s *Service) handleActivatePlayer(w http.ResponseWriter, r *http.Request) {
	if err := s.ActivatePlayer(r.Context(), chi.URLParam(r, "name")); err != nil {
		s.writePlayerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnbindPremium 解除微软正版绑定（幂等：未绑定也返回 204）。
func (s *Service) handleUnbindPremium(w http.ResponseWriter, r *http.Request) {
	if err := s.UnbindPremium(r.Context(), chi.URLParam(r, "name")); err != nil {
		s.writePlayerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writePlayerErr 把玩家管理相关错误映射为状态码；语义相同的错误只在这里映射一次。
func (s *Service) writePlayerErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, yggdrasil.ErrPremiumBound):
		writeErr(w, http.StatusConflict, s.t("account.error.premium_bound", nil))
	case errors.Is(err, yggdrasil.ErrLastPlayer):
		writeErr(w, http.StatusConflict, s.t("account.error.last_player", nil))
	case errors.Is(err, yggdrasil.ErrAlreadyExists):
		writeErr(w, http.StatusConflict, s.t("account.error.name_taken", nil))
	case errors.Is(err, ErrInvalidName):
		writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_name", nil))
	case errors.Is(err, yggdrasil.ErrNotFound):
		writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
	case errors.Is(err, yggdrasil.ErrInvalidToken):
		writeErr(w, http.StatusUnauthorized, s.t("account.error.unauthorized", nil))
	default:
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
	}
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
	redirectTo := r.URL.Query().Get("state")
	if redirectTo == "" || !isHTTPURL(redirectTo) {
		if err != nil {
			writeErr(w, http.StatusBadRequest, s.t("account.error.unknown_provider", nil))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"accessToken": token})
		return
	}
	if err != nil {
		http.Redirect(w, r, redirectTo+"#error="+url.QueryEscape("login failed"), http.StatusFound)
		return
	}
	http.Redirect(w, r, redirectTo+"#accessToken="+token, http.StatusFound)
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
	// 绑定结果回前端 profile 页展示（后端不落 HTML）
	if err := s.MojangOAuthCallback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code")); err != nil {
		http.Redirect(w, r, s.webBase()+"/profile?bind=error", http.StatusFound)
		return
	}
	http.Redirect(w, r, s.webBase()+"/profile?bind=ok", http.StatusFound)
}

// handleRegisterRequirements 公开注册前置要求：前端据此渲染邀请码/验证码控件，
// 并把密码长度与邮箱域名限制显示出来（否则用户只能靠注册失败反推规则）。
func (s *Service) handleRegisterRequirements(w http.ResponseWriter, _ *http.Request) {
	domains := s.opts.EmailDomainWhitelist
	if domains == nil {
		domains = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"registrationDisabled": s.opts.DisableRegistration,
		"inviteOnly":           s.opts.InviteOnly,
		"captcha":              s.opts.CaptchaDriver,
		"captchaSiteKey":       s.opts.CaptchaSiteKey,
		"minPasswordLength":    MinPasswordLength,
		"emailDomains":         domains, // 空数组 = 不限制域名
	})
}

// handleListPlayers 返回当前账号名下玩家（多玩家管理 UI）。
func (s *Service) handleListPlayers(w http.ResponseWriter, r *http.Request) {
	sess, ok := yggdrasil.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	players, err := s.store.PlayersByUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, players)
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
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, item)
	case errors.Is(err, yggdrasil.ErrNotFound):
		writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
	case errors.Is(err, yggdrasil.ErrQuotaExceeded):
		writeErr(w, http.StatusRequestEntityTooLarge, s.t("account.error.quota_exceeded", nil))
	default:
		writeErr(w, http.StatusBadRequest, s.t("account.error.internal", nil))
	}
}

// handleReport 处理内容举报（body: targetType=skin|comment, targetID, reason 可选）。
func (s *Service) handleReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType string `json:"targetType" validate:"required,oneof=skin comment"`
		TargetID   string `json:"targetID" validate:"required"`
		Reason     string `json:"reason" validate:"max=1000"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.bad_request", nil))
		return
	}
	if err := validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_report", nil))
		return
	}
	err := s.Report(r.Context(), req.TargetType, req.TargetID, req.Reason)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidReport):
		writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_report", nil))
	case errors.Is(err, yggdrasil.ErrAlreadyExists):
		writeErr(w, http.StatusConflict, s.t("account.error.duplicate_report", nil))
	case errors.Is(err, yggdrasil.ErrNotFound):
		writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
	default:
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
	}
}

func (s *Service) handleListSkinLib(w http.ResponseWriter, r *http.Request) {
	offset := intQuery(r, "offset", 0)
	limit := intQuery(r, "limit", 20)
	items, total, err := s.ListSkinLib(r.Context(), strQuery(r, "q"), strQuery(r, "sort"), offset, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, skinLibPage{Items: items, Total: total, Offset: offset, Limit: limit})
}

// skinLibPage 是皮肤库分页响应。
type skinLibPage struct {
	Items  []yggdrasil.SkinLibItem `json:"items"`
	Total  int64                   `json:"total"`
	Offset int                     `json:"offset"`
	Limit  int                     `json:"limit"`
}

func (s *Service) handleGetSkinLib(w http.ResponseWriter, r *http.Request) {
	item, err := s.GetSkinLibItem(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, yggdrasil.ErrNotFound) {
			writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
			return
		}
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Service) handleListComments(w http.ResponseWriter, r *http.Request) {
	comments, err := s.ListComments(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, yggdrasil.ErrNotFound) {
			writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
			return
		}
		writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (s *Service) handleLikeSkin(w http.ResponseWriter, r *http.Request) {
	if err := s.LikeSkin(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
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
		switch {
		case errors.Is(err, ErrInvalidComment):
			writeErr(w, http.StatusBadRequest, s.t("account.error.invalid_comment", nil))
		case errors.Is(err, yggdrasil.ErrNotFound):
			writeErr(w, http.StatusNotFound, s.t("account.error.not_found", nil))
		default:
			writeErr(w, http.StatusInternalServerError, s.t("account.error.internal", nil))
		}
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
	if err := s.store.WriteAudit(r.Context(), yggdrasil.AuditEntry{
		Actor: yggdrasil.AdminActor(r.Context()), ActorIP: yggdrasil.ClientIP(r),
		Action: "admin.batch_import", Detail: "imported=" + strconv.Itoa(n),
	}); err != nil {
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

func strQuery(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
