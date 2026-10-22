package yggdrasil

import "context"

type ctxKey struct{}

// WithSession 把会话写入 context。
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// SessionFromContext 从 context 读取会话。
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(*Session)
	return s, ok
}
