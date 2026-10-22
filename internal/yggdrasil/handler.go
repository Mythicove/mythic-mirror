package yggdrasil

import (
	"encoding/json"
	"net/http"
)

// Register 注册所有 Yggdrasil 路由到 mux。
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
