package pgstore

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/url"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/openid"
)

// session kinds
const (
	kindAuthorizeCode = "authorize_code"
	kindAccessToken   = "access_token"
	kindRefreshToken  = "refresh_token"
	kindPKCE          = "pkce"
	kindOIDCSession   = "oidc_session"
)

// retention 是行保留期（惰性 GC 用）。过期判定由 fosite 依据 session 内时间执行，
// 这里只决定死行何时可清理：refresh token 寿命 30 天，保留 45 天足够覆盖。
const retention = 45 * 24 * time.Hour

// OIDCStore 把 fosite 会话类存储落 Postgres（多副本共享 + 重启不丢）。
// 每条记录是完整 fosite.Request 的 JSON 序列化（含 openid.DefaultSession）。
type OIDCStore struct {
	pool *pgxpool.Pool
}

var _ oidc.SessionPersister = (*OIDCStore)(nil)

// OIDC 返回绑定同一连接池的 fosite 会话存储。
func (s *Store) OIDC() *OIDCStore {
	return &OIDCStore{pool: s.pool}
}

type oidcRequestRecord struct {
	RequestID         string                 `json:"request_id"`
	Client            *fosite.DefaultClient  `json:"client"`
	RequestedScope    []string               `json:"requested_scope"`
	GrantedScope      []string               `json:"granted_scope"`
	RequestedAudience []string               `json:"requested_audience"`
	GrantedAudience   []string               `json:"granted_audience"`
	Form              url.Values             `json:"form"`
	RequestedAt       time.Time              `json:"requested_at"`
	Session           *openid.DefaultSession `json:"session"`
}

func toRecord(req fosite.Requester) (*oidcRequestRecord, error) {
	sess, ok := req.GetSession().(*openid.DefaultSession)
	if !ok {
		return nil, fosite.ErrServerError.WithDebug("oidc store: unexpected session type")
	}
	cli, ok := req.GetClient().(*fosite.DefaultClient)
	if !ok {
		return nil, fosite.ErrServerError.WithDebug("oidc store: unexpected client type")
	}
	return &oidcRequestRecord{
		RequestID:         req.GetID(),
		Client:            cli,
		RequestedScope:    req.GetRequestedScopes(),
		GrantedScope:      req.GetGrantedScopes(),
		RequestedAudience: req.GetRequestedAudience(),
		GrantedAudience:   req.GetGrantedAudience(),
		Form:              req.GetRequestForm(),
		RequestedAt:       req.GetRequestedAt(),
		Session:           sess,
	}, nil
}

func (r *oidcRequestRecord) toRequest() *fosite.Request {
	return &fosite.Request{
		ID:                r.RequestID,
		RequestedAt:       r.RequestedAt,
		Client:            r.Client,
		RequestedScope:    fosite.Arguments(r.RequestedScope),
		GrantedScope:      fosite.Arguments(r.GrantedScope),
		RequestedAudience: fosite.Arguments(r.RequestedAudience),
		GrantedAudience:   fosite.Arguments(r.GrantedAudience),
		Form:              r.Form,
		Session:           r.Session,
	}
}

func (o *OIDCStore) create(ctx context.Context, kind, signature string, req fosite.Requester) error {
	rec, err := toRecord(req)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = o.pool.Exec(ctx, `
		INSERT INTO oidc_sessions (kind, signature, request_id, active, data, expires_at)
		VALUES ($1, $2, $3, true, $4, now() + $5::interval)
		ON CONFLICT (kind, signature) DO UPDATE
		SET request_id = EXCLUDED.request_id, active = true, data = EXCLUDED.data, expires_at = EXCLUDED.expires_at`,
		kind, signature, rec.RequestID, data, retention.String())
	if err != nil {
		return err
	}
	o.maybeGC(ctx)
	return nil
}

// get 返回记录及其 active 标志（active 存于列，不在 JSON 内）。
func (o *OIDCStore) get(ctx context.Context, kind, signature string) (*oidcRequestRecord, bool, bool, error) {
	var active bool
	var data []byte
	err := o.pool.QueryRow(ctx,
		`SELECT active, data FROM oidc_sessions WHERE kind = $1 AND signature = $2`,
		kind, signature).Scan(&active, &data)
	if err == pgx.ErrNoRows {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	var rec oidcRequestRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, false, false, err
	}
	return &rec, active, true, nil
}

func (o *OIDCStore) setActive(ctx context.Context, kind, signature string, active bool) error {
	tag, err := o.pool.Exec(ctx,
		`UPDATE oidc_sessions SET active = $3 WHERE kind = $1 AND signature = $2`,
		kind, signature, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fosite.ErrNotFound
	}
	return nil
}

func (o *OIDCStore) delete(ctx context.Context, kind, signature string) error {
	_, err := o.pool.Exec(ctx,
		`DELETE FROM oidc_sessions WHERE kind = $1 AND signature = $2`, kind, signature)
	return err
}

func (o *OIDCStore) deactivateByRequest(ctx context.Context, kind, requestID string) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE oidc_sessions SET active = false WHERE kind = $1 AND request_id = $2`, kind, requestID)
	return err
}

func (o *OIDCStore) deleteByRequest(ctx context.Context, kind, requestID string) error {
	_, err := o.pool.Exec(ctx,
		`DELETE FROM oidc_sessions WHERE kind = $1 AND request_id = $2`, kind, requestID)
	return err
}

// maybeGC 惰性清理过期行（约 1/64 的写操作触发一次）。
func (o *OIDCStore) maybeGC(ctx context.Context) {
	if rand.IntN(64) != 0 {
		return
	}
	_, _ = o.pool.Exec(ctx, `DELETE FROM oidc_sessions WHERE expires_at < now()`)
}

// —— fosite 存储接口（authorize code）——

func (o *OIDCStore) CreateAuthorizeCodeSession(ctx context.Context, code string, req fosite.Requester) error {
	return o.create(ctx, kindAuthorizeCode, code, req)
}

func (o *OIDCStore) GetAuthorizeCodeSession(ctx context.Context, code string, _ fosite.Session) (fosite.Requester, error) {
	rec, active, ok, err := o.get(ctx, kindAuthorizeCode, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fosite.ErrNotFound
	}
	if !active {
		return rec.toRequest(), fosite.ErrInvalidatedAuthorizeCode
	}
	return rec.toRequest(), nil
}

func (o *OIDCStore) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	return o.setActive(ctx, kindAuthorizeCode, code, false)
}

// —— access token ——

func (o *OIDCStore) CreateAccessTokenSession(ctx context.Context, signature string, req fosite.Requester) error {
	return o.create(ctx, kindAccessToken, signature, req)
}

func (o *OIDCStore) GetAccessTokenSession(ctx context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	rec, _, ok, err := o.get(ctx, kindAccessToken, signature)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fosite.ErrNotFound
	}
	return rec.toRequest(), nil
}

func (o *OIDCStore) DeleteAccessTokenSession(ctx context.Context, signature string) error {
	return o.delete(ctx, kindAccessToken, signature)
}

// —— refresh token ——

func (o *OIDCStore) CreateRefreshTokenSession(ctx context.Context, signature, _ string, req fosite.Requester) error {
	return o.create(ctx, kindRefreshToken, signature, req)
}

func (o *OIDCStore) GetRefreshTokenSession(ctx context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	rec, active, ok, err := o.get(ctx, kindRefreshToken, signature)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fosite.ErrNotFound
	}
	if !active {
		return rec.toRequest(), fosite.ErrInactiveToken
	}
	return rec.toRequest(), nil
}

func (o *OIDCStore) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	return o.delete(ctx, kindRefreshToken, signature)
}

func (o *OIDCStore) RevokeRefreshToken(ctx context.Context, requestID string) error {
	return o.deactivateByRequest(ctx, kindRefreshToken, requestID)
}

func (o *OIDCStore) RevokeAccessToken(ctx context.Context, requestID string) error {
	return o.deleteByRequest(ctx, kindAccessToken, requestID)
}

// RotateRefreshToken 与 fosite MemoryStore 语义一致：撤销旧 refresh + access。
func (o *OIDCStore) RotateRefreshToken(ctx context.Context, requestID, _ string) error {
	if err := o.RevokeRefreshToken(ctx, requestID); err != nil {
		return err
	}
	return o.RevokeAccessToken(ctx, requestID)
}

// —— PKCE ——

func (o *OIDCStore) CreatePKCERequestSession(ctx context.Context, code string, req fosite.Requester) error {
	return o.create(ctx, kindPKCE, code, req)
}

func (o *OIDCStore) GetPKCERequestSession(ctx context.Context, code string, _ fosite.Session) (fosite.Requester, error) {
	rec, _, ok, err := o.get(ctx, kindPKCE, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fosite.ErrNotFound
	}
	return rec.toRequest(), nil
}

func (o *OIDCStore) DeletePKCERequestSession(ctx context.Context, code string) error {
	return o.delete(ctx, kindPKCE, code)
}

// —— OpenID Connect session ——

func (o *OIDCStore) CreateOpenIDConnectSession(ctx context.Context, code string, req fosite.Requester) error {
	return o.create(ctx, kindOIDCSession, code, req)
}

func (o *OIDCStore) GetOpenIDConnectSession(ctx context.Context, code string, _ fosite.Requester) (fosite.Requester, error) {
	rec, _, ok, err := o.get(ctx, kindOIDCSession, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fosite.ErrNotFound
	}
	return rec.toRequest(), nil
}

func (o *OIDCStore) DeleteOpenIDConnectSession(ctx context.Context, code string) error {
	return o.delete(ctx, kindOIDCSession, code)
}
