package account

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
)

// RegisterRoutes 注册账户相关路由。
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /account/register", s.handleRegister)
	mux.HandleFunc("POST /account/verify", s.handleVerify)
	mux.HandleFunc("POST /account/forgot-password", s.handleForgot)
	mux.HandleFunc("POST /account/reset-password", s.handleReset)
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	resp, err := s.Register(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, yggdrasil.ErrAlreadyExists):
			writeErr(w, http.StatusConflict, "email or player name already exists")
		case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrWeakPassword),
			errors.Is(err, ErrInvalidName), errors.Is(err, ErrNameTaken):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.VerifyEmail(r.Context(), req.Token); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleForgot(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	_ = s.ForgotPassword(r.Context(), req)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleReset(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.ResetPassword(r.Context(), req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
