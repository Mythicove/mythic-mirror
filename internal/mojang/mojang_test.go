package mojang

import (
	"context"
	"encoding/base64"
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

	c := NewWithURL(srv.URL, srv.URL)
	id, err := c.ResolveName(context.Background(), "Alice")
	if err != nil || id != "9e6c7a5e4b3c2d1a" {
		t.Fatalf("resolve: %v %q", err, id)
	}
	if _, err := c.ResolveName(context.Background(), "Nobody"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestResolveProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/minecraft/profile/abc" {
			value := base64.StdEncoding.EncodeToString([]byte(
				`{"textures":{"SKIN":{"url":"http://mojang-skin","metadata":{"model":"slim"}},"CAPE":{"url":"http://mojang-cape"}}}`))
			_, _ = w.Write([]byte(`{"id":"abc","name":"x","properties":[{"name":"textures","value":"` + value + `"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewWithURL(srv.URL, srv.URL)
	tp, err := c.ResolveProfile(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if tp.SkinURL != "http://mojang-skin" || tp.CapeURL != "http://mojang-cape" || tp.Model != "slim" {
		t.Fatalf("profile: %+v", tp)
	}
}
