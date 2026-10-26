package captcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("response") == "good" {
			_, _ = w.Write([]byte(`{"success":true}`))
		} else {
			_, _ = w.Write([]byte(`{"success":false}`))
		}
	}))
	defer srv.Close()

	c := NewWithURL(srv.URL, "secret")
	if ok, _ := c.Verify(context.Background(), "good", "1.2.3.4"); !ok {
		t.Fatal("expected success")
	}
	if ok, _ := c.Verify(context.Background(), "bad", ""); ok {
		t.Fatal("expected failure")
	}
}

// TestVerifyCap 按 Cap Standalone 的 siteverify 契约验证：
// 路径 /{siteKey}/siteverify、JSON 请求体 {secret,response}、返回 {success}。
func TestVerifyCap(t *testing.T) {
	const siteKey = "d9256640cb53"
	var gotPath, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		var body struct {
			Secret   string `json:"secret"`
			Response string `json:"response"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success":false,"error":"Missing required parameters"}`))
			return
		}
		if body.Secret == "key-secret" && strings.HasPrefix(body.Response, siteKey+":") {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"error":"Invalid site key or secret"}`))
	}))
	defer srv.Close()

	c := NewCap(srv.URL+"/", siteKey, "key-secret")
	ok, err := c.Verify(context.Background(), siteKey+":abc:sol", "1.2.3.4")
	if err != nil || !ok {
		t.Fatalf("expected success, got ok=%v err=%v", ok, err)
	}
	if gotPath != "/"+siteKey+"/siteverify" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type %q", gotContentType)
	}
	if ok, _ := c.Verify(context.Background(), "other:x:y", ""); ok {
		t.Fatal("expected failure for foreign site key token")
	}
	c.secret = "wrong"
	if ok, _ := c.Verify(context.Background(), siteKey+":abc:sol", ""); ok {
		t.Fatal("expected failure for wrong secret")
	}
}
