package yggdrasil

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/shared/mc"
	"github.com/go-chi/chi/v5"
)

func makeSkinPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadSkinFlow(t *testing.T) {
	kp, _ := keys.Generate()
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryStore(), kp, fs, ServiceOptions{
		ServerName: "t", ImplementationName: "t", SkinDomains: []string{"localhost"},
	})
	r := chi.NewRouter()
	svc.Register(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	fs.SetBaseURL(srv.URL)

	// 1. authenticate
	ar := authenticate(t, srv.URL)

	// 2. upload skin（需认证）
	resp, err := authedPost(t, srv.URL+"/skin/Alice?model=slim", ar.AccessToken, makeSkinPNG(t, 64, 64))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("upload skin status %d", resp.StatusCode)
	}

	// 3. join
	joinBody, _ := json.Marshal(JoinRequest{
		AccessToken: ar.AccessToken, SelectedProfile: ar.SelectedProfile.ID, ServerID: "srv1",
	})
	resp, _ = http.Post(srv.URL+"/sessionserver/session/minecraft/join", "application/json", bytes.NewReader(joinBody))
	resp.Body.Close()

	// 3. hasJoined returns profile with SKIN url + slim model
	resp, err = http.Get(srv.URL + "/sessionserver/session/minecraft/hasJoined?username=Alice&serverId=srv1")
	if err != nil {
		t.Fatal(err)
	}
	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	value, _ := base64.StdEncoding.DecodeString(profile.Properties[0].Value)
	var tex Textures
	if err := json.Unmarshal(value, &tex); err != nil {
		t.Fatal(err)
	}
	if tex.Textures.SKIN == nil || tex.Textures.SKIN.URL == "" {
		t.Fatal("missing skin url")
	}
	if tex.Textures.SKIN.Metadata == nil || tex.Textures.SKIN.Metadata.Model != "slim" {
		t.Fatalf("expected slim model, got %+v", tex.Textures.SKIN.Metadata)
	}

	// 4. download the texture
	resp, err = http.Get(tex.Textures.SKIN.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("texture download status %d", resp.StatusCode)
	}

	// 5. avatar render
	resp, err = http.Get(srv.URL + "/avatars/" + mc.UUIDFromName("Alice") + "?size=64")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar status %d", resp.StatusCode)
	}
}

func TestApplySkinFromLib(t *testing.T) {
	ctx := context.Background()
	kp, _ := keys.Generate()
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	svc := NewService(store, kp, fs, ServiceOptions{
		ServerName: "t", ImplementationName: "t", SkinDomains: []string{"localhost"},
	})
	r := chi.NewRouter()
	svc.Register(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// 皮肤库里放一个条目（slim 模型）
	item, err := store.PublishSkin(ctx, "seed-admin", "Alice", "lib-texture.png", "from lib", "slim", "")
	if err != nil {
		t.Fatal(err)
	}

	ar := authenticate(t, srv.URL)

	// 应用到自己名下的玩家
	resp, err := authedPost(t, srv.URL+"/skinlib/"+item.ID+"/apply", ar.AccessToken, []byte(`{"playerName":"Alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("apply status %d", resp.StatusCode)
	}
	pd, err := store.ProfileByUUID(ctx, mc.UUIDFromName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	if pd.SkinKey != "lib-texture.png" || pd.Model != "slim" {
		t.Fatalf("skin not applied: %+v", pd)
	}

	// 应用到不属于自己的玩家名 → 404
	resp, _ = authedPost(t, srv.URL+"/skinlib/"+item.ID+"/apply", ar.AccessToken, []byte(`{"playerName":"NotMine"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("apply not-owned status %d", resp.StatusCode)
	}

	// 未认证 → 401
	resp, _ = http.Post(srv.URL+"/skinlib/"+item.ID+"/apply", "application/json", bytes.NewReader([]byte(`{"playerName":"Alice"}`)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("apply unauth status %d", resp.StatusCode)
	}
}

func authenticate(t *testing.T, base string) *AuthenticateResponse {
	t.Helper()
	resp, err := http.Post(base+"/authserver/authenticate", "application/json",
		bytes.NewReader([]byte(`{"username":"Alice","password":"password"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ar AuthenticateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		t.Fatal(err)
	}
	return &ar
}

func authedPost(t *testing.T, url, token string, body []byte) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}
