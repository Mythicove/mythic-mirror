package yggdrasil

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/go-chi/chi/v5"
)

func newTestServer(t *testing.T) (*httptest.Server, *keys.KeyPair) {
	t.Helper()
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryStore(), kp, fs, ServiceOptions{
		ServerName:         "test",
		ImplementationName: "test",
		SkinDomains:        []string{"localhost"},
	})
	r := chi.NewRouter()
	svc.Register(r)
	return httptest.NewServer(r), kp
}

func TestAuthenticateHasJoinedProfile(t *testing.T) {
	srv, kp := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/authserver/authenticate", "application/json",
		bytes.NewReader([]byte(`{"username":"Alice","password":"password"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticate status %d", resp.StatusCode)
	}
	var ar AuthenticateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ar.AccessToken == "" || ar.SelectedProfile == nil {
		t.Fatal("missing token/profile")
	}

	joinBody, _ := json.Marshal(JoinRequest{
		AccessToken: ar.AccessToken, SelectedProfile: ar.SelectedProfile.ID, ServerID: "srv1",
	})
	resp, err = http.Post(srv.URL+"/sessionserver/session/minecraft/join", "application/json",
		bytes.NewReader(joinBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("join status %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/sessionserver/session/minecraft/hasJoined?username=Alice&serverId=srv1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hasJoined status %d", resp.StatusCode)
	}
	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(profile.Properties) == 0 {
		t.Fatal("no properties in profile")
	}

	prop := profile.Properties[0]
	digest := sha256.Sum256([]byte(prop.Value))
	sig, _ := base64.StdEncoding.DecodeString(prop.Signature)
	if err := rsa.VerifyPKCS1v15(kp.PublicKey(), crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature verify: %v", err)
	}
}

func TestMeta(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("meta status %d", resp.StatusCode)
	}
	var meta map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["signaturePublickey"]; !ok {
		t.Fatal("meta missing signaturePublickey")
	}
}

func TestInvalidCredentials(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/authserver/authenticate", "application/json",
		bytes.NewReader([]byte(`{"username":"Alice","password":"wrong"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
