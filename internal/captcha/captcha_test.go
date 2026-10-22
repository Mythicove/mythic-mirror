package captcha

import (
	"context"
	"net/http"
	"net/http/httptest"
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
