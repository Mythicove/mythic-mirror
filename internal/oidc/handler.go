package oidc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v3"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/openid"
	"github.com/ory/fosite/token/jwt"
)

const sessionCookie = "mythicove_oidc_session"

// ticketTTL 是登录/同意 challenge 的有效期：一次性流程凭证，超时即失效。
const ticketTTL = 10 * time.Minute

// ticket 是无状态的流程凭证（hydra 式 login/consent challenge 的轻量实现）：
// 登录与同意 UI 由前端（webURL）承载，后端只暴露 JSON API；
// 跨站无法依赖 cookie 时，用 HMAC 签名的 ticket 在重定向间传递授权请求上下文。
type ticket struct {
	Kind string `json:"k"`             // login | consent
	Q    string `json:"q"`             // 原始 authorize 查询串
	UID  string `json:"uid,omitempty"` // 已认证用户；login 挑战下发时为空
	Exp  int64  `json:"exp"`
}

// RegisterRoutes 注册 OIDC 路由与客户端管理端点。
func (p *Provider) RegisterRoutes(r chi.Router) {
	r.Get("/.well-known/openid-configuration", p.handleDiscovery)
	r.Get("/.well-known/jwks.json", p.handleJWKS)
	r.Get("/oauth2/auth", p.handleAuthorize)
	r.Post("/oauth2/auth", p.handleAuthorize)
	r.Post("/oauth2/login/accept", p.handleLoginAccept)
	r.Get("/oauth2/consent", p.handleConsentInfo)
	r.Post("/oauth2/consent/accept", p.handleConsentAccept)
	r.Post("/oauth2/consent/reject", p.handleConsentReject)
	r.Post("/oauth2/token", p.handleToken)
	r.Post("/oauth2/revoke", p.handleRevoke)
	r.Get("/oauth2/userinfo", p.handleUserinfo)
	r.Post("/oauth2/userinfo", p.handleUserinfo)

	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.AdminRole(p.store, p.adminTokenValue))
		r.Get("/api/admin/oauth-clients", p.handleListClients)
		r.Post("/api/admin/oauth-clients", p.handleSaveClient)
		r.Delete("/api/admin/oauth-clients/{id}", p.handleDeleteClient)
	})
}

// SetAdminToken 设置客户端管理端点的静态管理员令牌。
func (p *Provider) SetAdminToken(token string) {
	p.adminTokenValue = token
}

// SetWebURL 设置前端站点地址：登录/同意 UI 由前端承载，后端仅重定向与提供 JSON API。
func (p *Provider) SetWebURL(u string) {
	p.webURL = strings.TrimRight(u, "/")
}

func (p *Provider) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.baseURL,
		"authorization_endpoint":                p.baseURL + "/oauth2/auth",
		"token_endpoint":                        p.baseURL + "/oauth2/token",
		"userinfo_endpoint":                     p.baseURL + "/oauth2/userinfo",
		"jwks_uri":                              p.baseURL + "/.well-known/jwks.json",
		"revocation_endpoint":                   p.baseURL + "/oauth2/revoke",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "offline_access", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	key := jose.JSONWebKey{Key: p.rsaKey.PublicKey(), Use: "sig", Algorithm: "RS256", KeyID: "mirror-1"}
	writeJSON(w, http.StatusOK, map[string]any{"keys": []jose.JSONWebKey{key}})
}

// cleanQuery 返回剔除流程票据参数后的规范化查询串：
// ticket 回跳时 URL 会带上 ticket 自身，参与比对与再签发前必须剔除，否则死循环。
func cleanQuery(u *url.URL) string {
	v := u.Query()
	v.Del("login_ticket")
	v.Del("consent_ticket")
	v.Del("deny_ticket")
	return v.Encode()
}

// handleAuthorize 是授权端点：未登录→前端登录页；已登录未同意→前端同意页；
// 持有效 consent ticket→发放 code。全程不渲染任何 HTML。
func (p *Provider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if p.webURL == "" {
		http.Error(w, "MIRROR_WEB_URL not configured", http.StatusInternalServerError)
		return
	}
	q := cleanQuery(r.URL)
	userID, ok := p.sessionUser(r)
	if !ok {
		// n 合一部署：web 前端的 httpOnly 会话 cookie 同 host 跨端口携带，
		// 视为 IdP 登录态并落 OIDC 会话 cookie——mods 与皮肤站共享一次登录。
		if uid, mok := p.mirrorSessionUser(r); mok {
			if token, err := p.store.CreateSession(r.Context(), uid, "oidc"); err == nil {
				p.setSessionCookie(w, token)
				userID, ok = uid, true
			}
		}
	}
	if !ok {
		if tk, valid := p.verifyTicket(r.URL.Query().Get("login_ticket"), "login"); valid && tk.Q == q && tk.UID != "" {
			if token, err := p.store.CreateSession(r.Context(), tk.UID, "oidc"); err == nil {
				p.setSessionCookie(w, token)
				userID, ok = tk.UID, true
			}
		}
	}
	if !ok {
		http.Redirect(w, r, p.webURL+"/oauth/login?challenge="+p.signTicket(ticket{Kind: "login", Q: q}), http.StatusFound)
		return
	}

	ar, err := p.provider.NewAuthorizeRequest(r.Context(), r)
	if err != nil {
		p.provider.WriteAuthorizeError(r.Context(), w, ar, err)
		return
	}

	if tk, valid := p.verifyTicket(r.URL.Query().Get("deny_ticket"), "consent"); valid && tk.Q == q && tk.UID == userID {
		p.provider.WriteAuthorizeError(r.Context(), w, ar, fosite.ErrAccessDenied)
		return
	}
	// 第一方客户端（同部署内的自有服务，如 Hoard）免同意：用户已向 IdP 登录，
	// 对自己生态内的服务再弹同意页只是摩擦；第三方客户端仍走前端同意页。
	firstParty := p.clientFirstParty(r.Context(), ar.GetClient().GetID())
	if tk, valid := p.verifyTicket(r.URL.Query().Get("consent_ticket"), "consent"); firstParty || (valid && tk.Q == q && tk.UID == userID) {
		session := p.newSession(r.Context(), userID, ar.GetRequestedScopes())
		// 我们的同意页展示全部请求 scope，用户批准即全量授予。
		// fosite 不自动 grant（consent 模型）：不 grant 则 granted_scope 为空，
		// id_token/refresh 全部发不出来。
		for _, scope := range ar.GetRequestedScopes() {
			ar.GrantScope(scope)
		}
		resp, err := p.provider.NewAuthorizeResponse(r.Context(), ar, session)
		if err != nil {
			p.provider.WriteAuthorizeError(r.Context(), w, ar, err)
			return
		}
		p.provider.WriteAuthorizeResponse(r.Context(), w, ar, resp)
		return
	}

	http.Redirect(w, r, p.webURL+"/oauth/consent?challenge="+p.signTicket(ticket{Kind: "consent", Q: q, UID: userID}), http.StatusFound)
}

// handleLoginAccept 校验凭据并把流程交回授权端点（login_ticket 携带已认证用户）。
func (p *Provider) handleLoginAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge string `json:"challenge"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}
	tk, valid := p.verifyTicket(req.Challenge, "login")
	if !valid || tk.UID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_challenge"})
		return
	}
	userID, err := p.store.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials"})
		return
	}
	done := p.signTicket(ticket{Kind: "login", Q: tk.Q, UID: userID})
	writeJSON(w, http.StatusOK, map[string]any{"redirectTo": "/oauth2/auth?" + tk.Q + "&login_ticket=" + done})
}

// handleConsentInfo 返回同意页展示所需信息（客户端与请求 scope）。
func (p *Provider) handleConsentInfo(w http.ResponseWriter, r *http.Request) {
	tk, valid := p.verifyTicket(r.URL.Query().Get("challenge"), "consent")
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_challenge"})
		return
	}
	ar, err := p.authorizeRequestFromQuery(r.Context(), tk.Q)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_challenge"})
		return
	}
	clientID := ar.GetClient().GetID()
	clientName := clientID
	if clients, err := p.ListClients(r.Context()); err == nil {
		for _, c := range clients {
			if c.ID == clientID && c.Name != "" {
				clientName = c.Name
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":   clientID,
		"client_name": clientName,
		"scopes":      ar.GetRequestedScopes(),
	})
}

func (p *Provider) handleConsentAccept(w http.ResponseWriter, r *http.Request) {
	p.consentDecision(w, r, "consent_ticket")
}

func (p *Provider) handleConsentReject(w http.ResponseWriter, r *http.Request) {
	p.consentDecision(w, r, "deny_ticket")
}

func (p *Provider) consentDecision(w http.ResponseWriter, r *http.Request, param string) {
	var req struct {
		Challenge string `json:"challenge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}
	tk, valid := p.verifyTicket(req.Challenge, "consent")
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_challenge"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"redirectTo": "/oauth2/auth?" + tk.Q + "&" + param + "=" + p.signTicket(tk),
	})
}

// authorizeRequestFromQuery 用保存的查询串重建授权请求以读取客户端信息（不写响应）。
func (p *Provider) authorizeRequestFromQuery(ctx context.Context, q string) (fosite.AuthorizeRequester, error) {
	nr, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/oauth2/auth?"+q, nil)
	if err != nil {
		return nil, err
	}
	return p.provider.NewAuthorizeRequest(ctx, nr)
}

func (p *Provider) handleToken(w http.ResponseWriter, r *http.Request) {
	session := openid.NewDefaultSession()
	ar, err := p.provider.NewAccessRequest(r.Context(), r, session)
	if err != nil {
		p.provider.WriteAccessError(r.Context(), w, ar, err)
		return
	}
	if ar.GetGrantTypes().ExactOne("client_credentials") {
		for _, scope := range ar.GetRequestedScopes() {
			ar.GrantScope(scope)
		}
	}
	resp, err := p.provider.NewAccessResponse(r.Context(), ar)
	if err != nil {
		p.provider.WriteAccessError(r.Context(), w, ar, err)
		return
	}
	p.provider.WriteAccessResponse(r.Context(), w, ar, resp)
}

func (p *Provider) handleRevoke(w http.ResponseWriter, r *http.Request) {
	err := p.provider.NewRevocationRequest(r.Context(), r)
	p.provider.WriteRevocationResponse(r.Context(), w, err)
}

func (p *Provider) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	token := fosite.AccessTokenFromRequest(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	session := openid.NewDefaultSession()
	// IntrospectToken 通过 Merge 把存储里的会话写进返回的 AccessRequester，
	// 传入的 session 变量本身不会被填充——必须用返回值的会话。
	_, ar, err := p.provider.IntrospectToken(r.Context(), token, fosite.AccessToken, session)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	sess, _ := ar.GetSession().(*openid.DefaultSession)
	if sess == nil {
		sess = session
	}
	claims := map[string]any{"sub": sess.GetSubject()}
	if sess.Username != "" {
		claims["preferred_username"] = sess.Username
	}
	if sess.Claims != nil && sess.Claims.Extra != nil {
		for k, v := range sess.Claims.Extra {
			claims[k] = v
		}
	}
	writeJSON(w, http.StatusOK, claims)
}

func (p *Provider) newSession(ctx context.Context, userID string, scopes fosite.Arguments) *openid.DefaultSession {
	session := openid.NewDefaultSession()
	session.Subject = userID
	extra := map[string]any{}
	if acct, err := p.store.AccountByID(ctx, userID); err == nil {
		session.Username = acct.Nickname
		extra["email"] = acct.Email
		extra["nickname"] = acct.Nickname
	}
	// player claim 用"当前玩家"，与游戏会话选定的 profile 保持一致
	if player, err := p.store.PlayerByUser(ctx, userID); err == nil && player != nil {
		if session.Username == "" {
			session.Username = player.Name
		}
		extra["player"] = player.Name
	}
	session.Claims = &jwt.IDTokenClaims{
		Subject: userID,
		Extra:   extra,
	}
	return session
}

func (p *Provider) sessionUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	sess, err := p.store.Validate(r.Context(), c.Value, "")
	if err != nil {
		return "", false
	}
	return sess.UserID, true
}

// mirrorSessionUser 复用 web 前端的 httpOnly 会话 cookie（mythic_session）：
// cookie 作用域为 host（与端口无关），故同域部署下 API 侧也能读到，
// 使 OIDC 授权可直接继承皮肤站登录态（n 合一共享登录）。
func (p *Provider) mirrorSessionUser(r *http.Request) (string, bool) {
	token := yggdrasil.BearerToken(r)
	if token == "" {
		return "", false
	}
	sess, err := p.store.Validate(r.Context(), token, "")
	if err != nil {
		return "", false
	}
	return sess.UserID, true
}

// clientFirstParty 判断客户端是否为同部署第一方服务（免同意）。
func (p *Provider) clientFirstParty(ctx context.Context, id string) bool {
	clients, err := p.ListClients(ctx)
	if err != nil {
		return false
	}
	for _, c := range clients {
		if c.ID == id {
			return c.FirstParty
		}
	}
	return false
}

func (p *Provider) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/oauth2",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
}

// signTicket / verifyTicket：HMAC-SHA256 无状态流程凭证，密钥派生自 RSA 私钥。
func (p *Provider) signTicket(t ticket) string {
	t.Exp = time.Now().Add(ticketTTL).Unix()
	body, _ := json.Marshal(t)
	mac := hmac.New(sha256.New, deriveSecret(p.rsaKey))
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (p *Provider) verifyTicket(s, kind string) (ticket, bool) {
	var t ticket
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 {
		return t, false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return t, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return t, false
	}
	mac := hmac.New(sha256.New, deriveSecret(p.rsaKey))
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, false
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return t, false
	}
	if t.Kind != kind || time.Now().Unix() > t.Exp {
		return t, false
	}
	return t, true
}

func (p *Provider) handleListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := p.ListClients(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (p *Provider) handleSaveClient(w http.ResponseWriter, r *http.Request) {
	var c OAuthClient
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if c.Public {
		c.SecretHash = ""
	}
	if err := p.SaveClient(r.Context(), c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.audit(r, "admin.oauth_client.save", c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (p *Provider) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	if err := p.DeleteClient(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "admin.oauth_client.delete", chi.URLParam(r, "id"))
	w.WriteHeader(http.StatusNoContent)
}

// audit 记录管理操作；actor 由 AdminRole 中间件注入（静态令牌 → "token"，会话 → "user:<id>"）。
func (p *Provider) audit(r *http.Request, action, target string) {
	if err := p.store.WriteAudit(r.Context(), yggdrasil.AuditEntry{
		Actor: yggdrasil.AdminActor(r.Context()), ActorIP: yggdrasil.ClientIP(r), Action: action, Target: target,
	}); err != nil {
		log.Printf("oidc: write audit %s: %v", action, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
