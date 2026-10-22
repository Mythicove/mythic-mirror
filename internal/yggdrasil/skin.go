package yggdrasil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"io"
	"net/http"
	"strconv"

	"github.com/Mythicove/mythic-mirror/internal/texture"
	"github.com/Mythicove/shared/storage"
)

// UploadSkin 校验并存储皮肤。
func (s *Service) UploadSkin(ctx context.Context, name, model string, r io.Reader) error {
	if model != "slim" {
		model = "default"
	}
	pd, err := s.store.ProfileByName(ctx, name)
	if err != nil {
		return err
	}
	data, err := texture.ReadAllLimit(r, texture.MaxTextureBytes)
	if err != nil {
		return err
	}
	norm, _, _, err := texture.ValidateSkin(data)
	if err != nil {
		return err
	}
	key, err := s.storeTexture(ctx, norm)
	if err != nil {
		return err
	}
	if err := s.store.SetSkin(ctx, pd.Player.ID, key, model); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	s.purgeTexture(ctx, key)
	return nil
}

// UploadCape 校验并存储披风。
func (s *Service) UploadCape(ctx context.Context, name string, r io.Reader) error {
	pd, err := s.store.ProfileByName(ctx, name)
	if err != nil {
		return err
	}
	data, err := texture.ReadAllLimit(r, texture.MaxTextureBytes)
	if err != nil {
		return err
	}
	norm, _, _, err := texture.ValidateCape(data)
	if err != nil {
		return err
	}
	key, err := s.storeTexture(ctx, norm)
	if err != nil {
		return err
	}
	if err := s.store.SetCape(ctx, pd.Player.ID, key); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	s.purgeTexture(ctx, key)
	return nil
}

func (s *Service) storeTexture(ctx context.Context, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:]) + ".png"
	_, err := s.files.Put(ctx, storage.Object{
		Key:         key,
		Size:        int64(len(data)),
		ContentType: "image/png",
		Body:        bytes.NewReader(data),
	})
	return key, err
}

// purgeTexture 触发 CDN 缓存刷新（可选）。
func (s *Service) purgeTexture(ctx context.Context, key string) {
	if s.cdn != nil {
		_ = s.cdn.Purge(ctx, []string{"/textures/" + key})
	}
}

func (s *Service) serveTexture(w http.ResponseWriter, r *http.Request, key string) {
	rc, err := s.files.Get(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "image/png")
	_, _ = io.Copy(w, rc)
}

func (s *Service) skinImage(ctx context.Context, uuid string) (image.Image, error) {
	pd, err := s.store.ProfileByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if pd.SkinKey == "" {
		return nil, ErrNotFound
	}
	rc, err := s.files.Get(ctx, pd.SkinKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return png.Decode(rc)
}

func (s *Service) render(w http.ResponseWriter, r *http.Request, kind string, scale int) {
	uuid := r.PathValue("uuid")
	img, err := s.skinImage(r.Context(), uuid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var out image.Image
	if kind == "body" {
		out = texture.Body(img, scale)
	} else {
		out = texture.Face(img, scale)
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, out)
}

func intQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
