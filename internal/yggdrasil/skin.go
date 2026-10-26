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
	"strings"

	"github.com/Mythicove/mythic-mirror/internal/texture"
	"github.com/Mythicove/shared/storage"
	"github.com/go-chi/chi/v5"
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
	key, added, err := s.storeTexture(ctx, norm, pd.UserID)
	if err != nil {
		return err
	}
	if err := s.checkStorageQuota(ctx, pd.UserID); err != nil {
		if added {
			_ = s.store.UnrecordTexture(ctx, key)
		}
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
	key, added, err := s.storeTexture(ctx, norm, pd.UserID)
	if err != nil {
		return err
	}
	if err := s.checkStorageQuota(ctx, pd.UserID); err != nil {
		if added {
			_ = s.store.UnrecordTexture(ctx, key)
		}
		return err
	}
	if err := s.store.SetCape(ctx, pd.Player.ID, key); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	s.purgeTexture(ctx, key)
	return nil
}

// ApplySkinFromLib 把皮肤库条目应用到当前会话用户的指定玩家（会话取自 context）。
func (s *Service) ApplySkinFromLib(ctx context.Context, itemID, playerName string) error {
	sess, ok := SessionFromContext(ctx)
	if !ok {
		return ErrInvalidToken
	}
	item, err := s.store.GetSkinLibItem(ctx, itemID)
	if err != nil {
		return err
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return err
	}
	for _, p := range players {
		if strings.EqualFold(p.Name, playerName) {
			if err := s.store.SetSkin(ctx, p.ID, item.TextureKey, item.Model); err != nil {
				return err
			}
			s.invalidateProfile(ctx, p.ID)
			return nil
		}
	}
	return ErrNotFound
}

func (s *Service) storeTexture(ctx context.Context, data []byte, ownerUserID string) (key string, added bool, err error) {
	sum := sha256.Sum256(data)
	key = hex.EncodeToString(sum[:]) + ".png"
	_, err = s.files.Put(ctx, storage.Object{
		Key:         key,
		Size:        int64(len(data)),
		ContentType: "image/png",
		Body:        bytes.NewReader(data),
	})
	if err != nil {
		return "", false, err
	}
	added, err = s.store.RecordTexture(ctx, key, ownerUserID, int64(len(data)))
	if err != nil {
		return "", false, err
	}
	return key, added, nil
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
	s.renderUUID(w, r, chi.URLParam(r, "uuid"), kind, scale)
}

// renderUUID 按 uuid 渲染贴图切片（face/body）；by-name 端点复用。
func (s *Service) renderUUID(w http.ResponseWriter, r *http.Request, uuid, kind string, scale int) {
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
