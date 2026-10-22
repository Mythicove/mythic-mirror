package yggdrasil

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mythicove/shared/mc"
)

func TestUSM(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	ar := authenticate(t, srv.URL)
	resp, err := authedPost(t, srv.URL+"/skin/Alice?model=slim", ar.AccessToken, makeSkinPNG(t, 64, 64))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/usm/" + mc.UUIDFromName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usm status %d", resp.StatusCode)
	}
	var u USMResponse
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.PlayerName != "Alice" || len(u.Skins) == 0 {
		t.Fatalf("usm: %+v", u)
	}
}
