package yggdrasil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/mojang"
	"github.com/Mythicove/shared/mc"
	"github.com/go-chi/chi/v5"
)

type mockResolver struct {
	tp  *mojang.TextureProfile
	err error
}

func (m *mockResolver) ResolveName(context.Context, string) (string, error) { return "realuuid", nil }
func (m *mockResolver) ResolveProfile(context.Context, string) (*mojang.TextureProfile, error) {
	return m.tp, m.err
}

func TestPremiumTextureFallback(t *testing.T) {
	kp, _ := keys.Generate()
	store := NewMemoryStore()
	fs, _ := filestorage.New(t.TempDir(), "")
	svc := NewService(store, kp, fs, ServiceOptions{ServerName: "t", ImplementationName: "t", SkinDomains: []string{"localhost"}})
	svc.SetResolver(&mockResolver{tp: &mojang.TextureProfile{SkinURL: "http://mojang-skin", Model: "slim"}})

	aliceUUID := mc.UUIDFromName("Alice")
	if err := store.SetPremium(context.Background(), aliceUUID, "realuuid"); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	svc.Register(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessionserver/session/minecraft/profile/" + aliceUUID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID != "realuuid" {
		t.Fatalf("expected premium uuid, got %q", profile.ID)
	}
	value, _ := base64.StdEncoding.DecodeString(profile.Properties[0].Value)
	var tex Textures
	if err := json.Unmarshal(value, &tex); err != nil {
		t.Fatal(err)
	}
	if tex.Textures.SKIN == nil || tex.Textures.SKIN.URL != "http://mojang-skin" {
		t.Fatalf("expected mojang skin fallback, got %+v", tex.Textures.SKIN)
	}
}
