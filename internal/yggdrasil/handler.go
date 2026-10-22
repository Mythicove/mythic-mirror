package yggdrasil

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Register 注册所有路由到 mux。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleMeta)
	mux.HandleFunc("POST /authserver/authenticate", s.handleAuthenticate)
	mux.HandleFunc("POST /authserver/refresh", s.handleRefresh)
	mux.HandleFunc("POST /authserver/validate", s.handleValidate)
	mux.HandleFunc("POST /authserver/invalidate", s.handleInvalidate)
	mux.HandleFunc("POST /authserver/signout", s.handleSignout)
	mux.HandleFunc("POST /sessionserver/session/minecraft/join", s.handleJoin)
	mux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", s.handleHasJoined)
	mux.HandleFunc("GET /sessionserver/session/minecraft/profile/{uuid}", s.handleProfile)
	mux.HandleFunc("POST /api/profiles/minecraft", s.handleLookupNames)
	mux.HandleFunc("POST /skin/{name}", s.handleUploadSkin)
	mux.HandleFunc("GET /skin/{name}", s.handleGetSkin)
	mux.HandleFunc("POST /cape/{name}", s.handleUploadCape)
	mux.HandleFunc("GET /cape/{name}", s.handleGetCape)
	mux.HandleFunc("GET /textures/{key}", s.handleTexture)
	mux.HandleFunc("GET /avatars/{uuid}", s.handleAvatar)
	mux.HandleFunc("GET /renders/head/{uuid}", s.handleHead)
	mux.HandleFunc("GET /renders/body/{uuid}", s.handleBody)
	mux.HandleFunc("GET /usm/{uuid}", s.handleUSM)
	mux.HandleFunc("POST /mojang/verify", s.handleMojangVerify)
	mux.HandleFunc("GET /MinecraftSkins/{name}", s.handleLegacySkin)
	mux.HandleFunc("GET /MinecraftCloaks/{name}", s.handleLegacyCape)
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
	uuid := r.PathValue("uuid")
	profile, err := s.ProfileByUUID(r.Context(), uuid)
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
	name := r.PathValue("name")
	model := r.URL.Query().Get("model")
	if err := s.UploadSkin(r.Context(), name, model, r.Body); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleUploadCape(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.UploadCape(r.Context(), name, r.Body); err != nil {
		writeError(w, http.StatusBadRequest, "IllegalArgumentException", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleGetSkin(w http.ResponseWriter, r *http.Request) {
	s.serveSkin(w, r, r.PathValue("name"))
}

func (s *Service) handleGetCape(w http.ResponseWriter, r *http.Request) {
	s.serveCape(w, r, r.PathValue("name"))
}

func (s *Service) handleLegacySkin(w http.ResponseWriter, r *http.Request) {
	s.serveSkin(w, r, strings.TrimSuffix(r.PathValue("name"), ".png"))
}

func (s *Service) handleLegacyCape(w http.ResponseWriter, r *http.Request) {
	s.serveCape(w, r, strings.TrimSuffix(r.PathValue("name"), ".png"))
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
	s.serveTexture(w, r, r.PathValue("key"))
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
