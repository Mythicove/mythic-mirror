package mojang

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/profiles/minecraft/Alice" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"9e6c7a5e4b3c2d1a","name":"Alice"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewWithURL(srv.URL)
	id, err := c.ResolveName(context.Background(), "Alice")
	if err != nil || id != "9e6c7a5e4b3c2d1a" {
		t.Fatalf("resolve: %v %q", err, id)
	}
	if _, err := c.ResolveName(context.Background(), "Nobody"); err == nil {
		t.Fatal("expected not found error")
	}
}
