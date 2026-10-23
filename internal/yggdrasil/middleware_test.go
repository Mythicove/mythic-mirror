package yggdrasil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mythicove/shared/ratelimit"
)

func TestClientIPStripsPort(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:5678": "1.2.3.4",
		"1.2.3.4":      "1.2.3.4",
		"[::1]:80":     "::1",
	}
	for addr, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = addr
		if got := ClientIP(r); got != want {
			t.Errorf("ClientIP(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestRateLimitSameIPDifferentPort(t *testing.T) {
	h := RateLimit(ratelimit.NewMemory(), "test", 2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 三条连接同源 IP、不同 ephemeral 端口：应共享同一限流桶。
	status := make([]int, 0, 3)
	for _, port := range []string{"40001", "40002", "40003"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "203.0.113.9:" + port
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		status = append(status, w.Code)
	}
	if status[0] != http.StatusOK || status[1] != http.StatusOK {
		t.Fatalf("first two should pass, got %v", status)
	}
	if status[2] != http.StatusTooManyRequests {
		t.Fatalf("third (same IP) should be 429, got %v", status)
	}
}

func TestAuthAcceptsSessionCookie(t *testing.T) {
	store := NewMemoryStore()
	token, err := store.CreateSession(context.Background(), "seed-admin", "ct")
	if err != nil {
		t.Fatal(err)
	}
	var gotUserID string
	h := Auth(store)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sess, _ := SessionFromContext(r.Context())
		gotUserID = sess.UserID
	}))

	// 仅 cookie（无 Authorization 头）→ 通过
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotUserID != "seed-admin" {
		t.Fatalf("cookie auth: code=%d user=%q", rec.Code, gotUserID)
	}

	// 无凭证 → 401
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credential: %d", rec.Code)
	}

	// 无效 cookie → 401
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bogus"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad cookie: %d", rec.Code)
	}

	// Authorization 头优先于 cookie
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bogus"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("header priority: %d", rec.Code)
	}
}
