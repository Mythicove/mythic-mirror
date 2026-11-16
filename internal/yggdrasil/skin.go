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

// ownedProfileByName 解析玩家名并校验会话范围：网站会话可管理所属账号的玩家，
// 启动器会话只能管理凭证绑定的玩家。未登录或不属于会话一律拒绝
// （不存在返回 ErrNotFound、不拥有也返回 ErrNotFound，不泄露玩家归属）。
// 皮肤/披风的所有写操作必须经此入口，避免"登录即可改他人皮肤"。
func (s *Service) ownedProfileByName(ctx context.Context, name string) (*ProfileData, error) {
	players, err := ScopedPlayers(ctx, s.store)
	if err != nil {
		return nil, err
	}
	for _, player := range players {
		if strings.EqualFold(player.Name, name) {
			return s.store.ProfileByUUID(ctx, player.ID)
		}
	}
	return nil, ErrNotFound
}

// UploadSkin 校验并存储皮肤。
func (s *Service) UploadSkin(ctx context.Context, name, model string, r io.Reader) error {
	if model != "slim" {
		model = "default"
	}
	pd, err := s.ownedProfileByName(ctx, name)
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
	pd, err := s.ownedProfileByName(ctx, name)
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
	pd, err := s.ownedProfileByName(ctx, playerName)
	if err != nil {
		return err
	}
	item, err := s.store.GetSkinLibItem(ctx, itemID)
	if err != nil {
		return err
	}
	if err := s.store.SetSkin(ctx, pd.Player.ID, item.TextureKey, item.Model); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	return nil
}

// ClearSkin 清除自定义皮肤，回落内置默认（Steve/Alex 按模型）。归属校验同上传。
func (s *Service) ClearSkin(ctx context.Context, name string) error {
	pd, err := s.ownedProfileByName(ctx, name)
	if err != nil {
		return err
	}
	if err := s.store.SetSkin(ctx, pd.Player.ID, "", pd.Model); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	return nil
}

// ClearCape 清除披风。归属校验同上传。
func (s *Service) ClearCape(ctx context.Context, name string) error {
	pd, err := s.ownedProfileByName(ctx, name)
	if err != nil {
		return err
	}
	if err := s.store.SetCape(ctx, pd.Player.ID, ""); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	return nil
}

// ApplyBuiltinSkin 把 vanilla 内置预设应用到自己的玩家（按体型取对应变体）。
// 内置 key 不进对象存储、不计配额；归属校验同上传。
func (s *Service) ApplyBuiltinSkin(ctx context.Context, name, id, model string) error {
	pd, err := s.ownedProfileByName(ctx, name)
	if err != nil {
		return err
	}
	if model == "" {
		model = pd.Model
	}
	if model != "slim" {
		model = "default"
	}
	key := texture.BuiltinKey(id, model)
	if _, err := texture.BuiltinBytes(key); err != nil {
		return ErrNotFound
	}
	if err := s.store.SetSkin(ctx, pd.Player.ID, key, model); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	return nil
}

// SetPlayerModel 切换体型（default/slim）。内置皮肤随之换宽/窄变体；
// 自定义皮肤仅改模型标记（臂宽渲染）。归属校验同上传。
func (s *Service) SetPlayerModel(ctx context.Context, name, model string) error {
	pd, err := s.ownedProfileByName(ctx, name)
	if err != nil {
		return err
	}
	if model != "slim" {
		model = "default"
	}
	key := pd.SkinKey
	if texture.IsBuiltin(key) {
		id, ok := texture.BuiltinID(key)
		if !ok {
			return ErrNotFound
		}
		key = texture.BuiltinKey(id, model)
	}
	if err := s.store.SetSkin(ctx, pd.Player.ID, key, model); err != nil {
		return err
	}
	s.invalidateProfile(ctx, pd.Player.ID)
	return nil
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
		_ = s.cdn.Purge(ctx, []string{strings.TrimRight(s.opts.TexturesBaseURL, "/") + "/textures/" + key})
	}
}

func (s *Service) serveTexture(w http.ResponseWriter, r *http.Request, key string) {
	// 内置预设不进对象存储：按 key 直接出内嵌图，且可长缓存（内容不可变）
	if texture.IsBuiltin(key) {
		b, err := texture.BuiltinBytes(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		_, _ = w.Write(b)
		return
	}
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
		// 未上传皮肤：回落内置默认（Steve/Alex 按模型），头像/渲染不再 404
		return texture.DefaultSkin(pd.Model == "slim"), nil
	}
	if texture.IsBuiltin(pd.SkinKey) {
		return texture.BuiltinImage(pd.SkinKey)
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
	switch kind {
	case "body":
		out = texture.Body(img, scale)
	case "back":
		out = texture.BodyBack(img, scale)
	case "side":
		out = texture.BodySide(img, scale)
	default:
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
