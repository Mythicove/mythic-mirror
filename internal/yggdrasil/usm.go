package yggdrasil

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// USMResponse 是 USM/CustomSkinLoader API 响应。
type USMResponse struct {
	PlayerName      string            `json:"player_name"`
	LastUpdate      int64             `json:"last_update"`
	ModelPreference []string          `json:"model_preference"`
	Skins           map[string]string `json:"skins"`
	Cape            string            `json:"cape,omitempty"`
	Elytra          string            `json:"elytra,omitempty"`
}

// USM 返回 USM API 响应。
func (s *Service) USM(ctx context.Context, uuid string) (*USMResponse, error) {
	pd, err := s.store.ProfileByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	model := pd.Model
	if model == "" {
		model = "default"
	}
	u := &USMResponse{
		PlayerName:      pd.Player.Name,
		LastUpdate:      time.Now().Unix(),
		ModelPreference: []string{model},
		Skins:           map[string]string{},
	}
	if pd.SkinKey != "" {
		if url, err := s.files.PresignGet(ctx, pd.SkinKey); err == nil {
			u.Skins["default"] = url.String()
			u.Skins["slim"] = url.String()
		}
	}
	if pd.CapeKey != "" {
		if url, err := s.files.PresignGet(ctx, pd.CapeKey); err == nil {
			u.Cape = url.String()
		}
	}
	return u, nil
}

// VerifyMojangRequest 是正版验证请求。
type VerifyMojangRequest struct {
	AccessToken string `json:"accessToken"`
	PlayerName  string `json:"playerName"`
}

// VerifyMojang 验证玩家名对应的正版账号，并标记 premium。
func (s *Service) VerifyMojang(ctx context.Context, req VerifyMojangRequest) error {
	if s.resolver == nil {
		return ErrNotFound
	}
	sess, err := s.store.Validate(ctx, req.AccessToken, "")
	if err != nil {
		return err
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return err
	}
	var target *Player
	for i := range players {
		if players[i].Name == req.PlayerName {
			target = &players[i]
		}
	}
	if target == nil {
		return ErrNotFound
	}
	if _, err := s.resolver.ResolveName(ctx, req.PlayerName); err != nil {
		return err
	}
	return s.store.SetPremium(ctx, target.ID, true)
}

func (s *Service) handleUSM(w http.ResponseWriter, r *http.Request) {
	u, err := s.USM(r.Context(), r.PathValue("uuid"))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Service) handleMojangVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyMojangRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	if err := s.VerifyMojang(r.Context(), req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "verification failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
