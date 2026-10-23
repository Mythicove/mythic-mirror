package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v3"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/openid"
	"github.com/ory/fosite/token/jwt"
)

const sessionCookie = "mythicove_oidc_session"

// RegisterRoutes 注册 OIDC 路由与客户端管理端点。
func (p *Provider) RegisterRoutes(r chi.Router) {
	r.Get("/.well-known/openid-configuration", p.handleDiscovery)
	r.Get("/.well-known/jwks.json", p.handleJWKS)
	r.Get("/oauth2/auth", p.handleAuthorize)
	r.Post("/oauth2/auth", p.handleAuthorize)
	r.Get("/oauth2/login", p.handleLoginForm)
	r.Post("/oauth2/login", p.handleLogin)
	r.Post("/oauth2/token", p.handleToken)
	r.Post("/oauth2/revoke", p.handleRevoke)
	r.Get("/oauth2/userinfo", p.handleUserinfo)
	r.Post("/oauth2/userinfo", p.handleUserinfo)

	r.Group(func(r chi.Router) {
		r.Use(yggdrasil.Admin(p.adminTokenValue))
		r.Get("/api/admin/oauth-clients", p.handleListClients)
		r.Post("/api/admin/oauth-clients", p.handleSaveClient)
		r.Delete("/api/admin/oauth-clients/{id}", p.handleDeleteClient)
	})
}

// SetAdminToken 设置客户端管理端点的静态管理员令牌。
func (p *Provider) SetAdminToken(token string) {
	p.adminTokenValue = token
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

func (p *Provider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.sessionUser(r)
	if !ok {
		// 未登录 → 跳转登录页（保留原始查询串）
		http.Redirect(w, r, "/oauth2/login?"+r.URL.RawQuery, http.StatusFound)
		return
	}

	ar, err := p.provider.NewAuthorizeRequest(r.Context(), r)
	if err != nil {
		p.provider.WriteAuthorizeError(r.Context(), w, ar, err)
		return
	}

	if r.Method == http.MethodPost {
		if r.FormValue("approve") != "yes" {
			p.provider.WriteAuthorizeError(r.Context(), w, ar, fosite.ErrAccessDenied)
			return
		}
		session := p.newSession(r.Context(), userID, ar.GetRequestedScopes())
		resp, err := p.provider.NewAuthorizeResponse(r.Context(), ar, session)
		if err != nil {
			p.provider.WriteAuthorizeError(r.Context(), w, ar, err)
			return
		}
		p.provider.WriteAuthorizeResponse(r.Context(), w, ar, resp)
		return
	}

	writeHTML(w, consentPage(ar.GetClient().GetID(), ar.GetRequestedScopes(), r.URL.String()))
}

func (p *Provider) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	writeHTML(w, loginPage(r.URL.RawQuery))
}

func (p *Provider) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	userID, err := p.store.Authenticate(r.Context(), r.FormValue("email"), r.FormValue("password"))
	if err != nil {
		writeHTML(w, loginPage(r.FormValue("req")))
		return
	}
	token, err := p.store.CreateSession(r.Context(), userID, "oidc")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/oauth2",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
	http.Redirect(w, r, "/oauth2/auth?"+r.FormValue("req"), http.StatusFound)
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
	_, _, err := p.provider.IntrospectToken(r.Context(), token, fosite.AccessToken, session)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	claims := map[string]any{"sub": session.GetSubject()}
	if session.Username != "" {
		claims["preferred_username"] = session.Username
	}
	if session.Claims != nil && session.Claims.Extra != nil {
		for k, v := range session.Claims.Extra {
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
	if players, err := p.store.PlayersByUser(ctx, userID); err == nil && len(players) > 0 {
		if session.Username == "" {
			session.Username = players[0].Name
		}
		extra["player"] = players[0].Name
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
	writeJSON(w, http.StatusCreated, c)
}

func (p *Provider) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	if err := p.DeleteClient(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func loginPage(req string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>Sign in</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1026;color:#e6e9f5;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}.card{background:#1a1f3a;border:1px solid #2a3150;border-radius:16px;padding:32px;width:320px}h1{font-size:20px}input{width:100%;box-sizing:border-box;background:#0b1026;border:1px solid #2a3150;color:#e6e9f5;border-radius:10px;padding:10px 12px;margin:8px 0}button{width:100%;background:#6366f1;color:#fff;border:none;border-radius:10px;padding:10px;margin-top:8px;cursor:pointer}</style></head>
<body><form class="card" method="post" action="/oauth2/login"><h1>Sign in</h1><input type="hidden" name="req" value="` + req + `"><input name="email" placeholder="Email" autocomplete="username"><input type="password" name="password" placeholder="Password" autocomplete="current-password"><button type="submit">Continue</button></form></body></html>`
}

func consentPage(clientID string, scopes fosite.Arguments, action string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>Authorize</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1026;color:#e6e9f5;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}.card{background:#1a1f3a;border:1px solid #2a3150;border-radius:16px;padding:32px;width:360px}h1{font-size:20px}p{color:#9aa0b8;font-size:14px}button{background:#6366f1;color:#fff;border:none;border-radius:10px;padding:10px 16px;margin-right:8px;cursor:pointer}.deny{background:transparent;border:1px solid #2a3150;color:#9aa0b8}</style></head>
<body><form class="card" method="post" action="` + action + `"><h1>Authorize ` + clientID + `</h1><p>This app wants access to: <code>` + strings.Join(scopes, ", ") + `</code></p><button type="submit" name="approve" value="yes">Approve</button><button class="deny" type="submit" name="approve" value="no">Deny</button></form></body></html>`
}
