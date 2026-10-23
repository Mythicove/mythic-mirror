package yggdrasil

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Register 注册所有路由到 chi 路由器。
func (s *Service) Register(r chi.Router) {
	r.Get("/", s.handleMeta)

	// 认证端点：更严格的限流（防爆破）
	r.Group(func(r chi.Router) {
		r.Use(RateLimit(s.limiter, "auth", 10, time.Second))
		r.Post("/authserver/authenticate", s.handleAuthenticate)
		r.Post("/authserver/refresh", s.handleRefresh)
		r.Post("/authserver/validate", s.handleValidate)
		r.Post("/authserver/invalidate", s.handleInvalidate)
		r.Post("/authserver/signout", s.handleSignout)
	})

	r.Post("/sessionserver/session/minecraft/join", s.handleJoin)
	r.Get("/sessionserver/session/minecraft/hasJoined", s.handleHasJoined)
	r.Get("/sessionserver/session/minecraft/profile/{uuid}", s.handleProfile)
	r.Post("/api/profiles/minecraft", s.handleLookupNames)

	r.Get("/skin/{name}", s.handleGetSkin)
	r.Get("/cape/{name}", s.handleGetCape)

	// 需认证的写操作
	r.Group(func(r chi.Router) {
		r.Use(Auth(s.store))
		r.Post("/skin/{name}", s.handleUploadSkin)
		r.Post("/cape/{name}", s.handleUploadCape)
		r.Post("/mojang/verify", s.handleMojangVerify)
		r.Post("/skinlib/{id}/apply", s.handleApplySkin)
	})

	r.Get("/textures/{key}", s.handleTexture)
	r.Get("/avatars/{uuid}", s.handleAvatar)
	r.Get("/renders/head/{uuid}", s.handleHead)
	r.Get("/renders/body/{uuid}", s.handleBody)
	r.Get("/usm/{uuid}", s.handleUSM)
	r.Get("/MinecraftSkins/{name}", s.handleLegacySkin)
	r.Get("/MinecraftCloaks/{name}", s.handleLegacyCape)
}

func (s *Service) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Meta())
}

func (s *Service) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	var req AuthenticateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	resp, err := s.Authenticate(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid credentials. Invalid username or password.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	resp, err := s.Refresh(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid token.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Service) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	if err := s.Validate(r.Context(), req); err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid token.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleInvalidate(w http.ResponseWriter, r *http.Request) {
	var req InvalidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	if err := s.Invalidate(r.Context(), req); err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid token.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleSignout(w http.ResponseWriter, r *http.Request) {
	var req SignoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	if err := s.Signout(r.Context(), req); err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid credentials.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	if err := s.Join(r.Context(), req); err != nil {
		writeError(w, http.StatusForbidden, "ForbiddenOperationException", "Invalid token.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleHasJoined(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	serverID := r.URL.Query().Get("serverId")
	profile, err := s.HasJoined(r.Context(), username, serverID)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Service) handleProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.ProfileByUUID(r.Context(), chi.URLParam(r, "uuid"))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Service) handleLookupNames(w http.ResponseWriter, r *http.Request) {
	var names []string
	if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "bad request")
		return
	}
	players, err := s.LookupNames(r.Context(), names)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "InternalError", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, players)
}

func (s *Service) handleUploadSkin(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	if err := s.UploadSkin(r.Context(), chi.URLParam(r, "name"), model, r.Body); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			writeError(w, http.StatusRequestEntityTooLarge, "QuotaExceededException", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleUploadCape(w http.ResponseWriter, r *http.Request) {
	if err := s.UploadCape(r.Context(), chi.URLParam(r, "name"), r.Body); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			writeError(w, http.StatusRequestEntityTooLarge, "QuotaExceededException", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleApplySkin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerName string `json:"playerName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerName == "" {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", "playerName required")
		return
	}
	err := s.ApplySkinFromLib(r.Context(), chi.URLParam(r, "id"), req.PlayerName)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case err == ErrNotFound:
		writeError(w, http.StatusNotFound, "IllegalArgumentException", "not found")
	default:
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", err.Error())
	}
}

func (s *Service) handleGetSkin(w http.ResponseWriter, r *http.Request) {
	s.serveSkin(w, r, chi.URLParam(r, "name"))
}

func (s *Service) handleGetCape(w http.ResponseWriter, r *http.Request) {
	s.serveCape(w, r, chi.URLParam(r, "name"))
}

func (s *Service) handleLegacySkin(w http.ResponseWriter, r *http.Request) {
	s.serveSkin(w, r, strings.TrimSuffix(chi.URLParam(r, "name"), ".png"))
}

func (s *Service) handleLegacyCape(w http.ResponseWriter, r *http.Request) {
	s.serveCape(w, r, strings.TrimSuffix(chi.URLParam(r, "name"), ".png"))
}

func (s *Service) serveSkin(w http.ResponseWriter, r *http.Request, name string) {
	pd, err := s.store.ProfileByName(r.Context(), name)
	if err != nil || pd.SkinKey == "" {
		http.NotFound(w, r)
		return
	}
	s.serveTexture(w, r, pd.SkinKey)
}

func (s *Service) serveCape(w http.ResponseWriter, r *http.Request, name string) {
	pd, err := s.store.ProfileByName(r.Context(), name)
	if err != nil || pd.CapeKey == "" {
		http.NotFound(w, r)
		return
	}
	s.serveTexture(w, r, pd.CapeKey)
}

func (s *Service) handleTexture(w http.ResponseWriter, r *http.Request) {
	s.serveTexture(w, r, chi.URLParam(r, "key"))
}

func (s *Service) handleAvatar(w http.ResponseWriter, r *http.Request) {
	size := intQuery(r, "size", 128)
	scale := size / 8
	if scale < 1 {
		scale = 1
	}
	s.render(w, r, "face", scale)
}

func (s *Service) handleHead(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "face", intQuery(r, "scale", 8))
}

func (s *Service) handleBody(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "body", intQuery(r, "scale", 4))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{
		"error":        code,
		"errorMessage": msg,
	})
}
