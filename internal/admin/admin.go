// Package admin 提供管理后台：用户管理、登录方式、公告、邀请码、皮肤库审核。
package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/storage"
)

// LoginMethod 是登录方式（供前端渲染登录按钮）。
type LoginMethod struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // password | oauth
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Order   int    `json:"order"`
}

// Announcement 是站点公告。
type Announcement struct {
	Title   string `json:"title" validate:"max=120"`
	Body    string `json:"body" validate:"max=500"`
	Enabled bool   `json:"enabled"`
}

const (
	settingLoginMethods = "login_methods"
	settingAnnouncement = "announcement"
	settingBrand        = "brand"
)

// BrandVariant 是品牌图标的明暗变体。
type BrandVariant string

// 合法的品牌图标变体。
const (
	BrandLight BrandVariant = "light"
	BrandDark  BrandVariant = "dark"
)

// MaxBrandLogoBytes 是单张品牌图标的大小上限（后台上传为低频小图，1MiB 足够）。
const MaxBrandLogoBytes = 1 << 20

// BrandConfig 是品牌图标配置：哪些变体被自定义过 + 用于缓存击穿的版本号。
type BrandConfig struct {
	Light   bool `json:"light"`
	Dark    bool `json:"dark"`
	Version int  `json:"version"`
}

// ErrBrandStorage 表示未注入对象存储（brand 图标依赖它持久化）。
var ErrBrandStorage = errors.New("brand storage not configured")

// ErrInvalidBrandVariant 表示变体不是 light/dark。
var ErrInvalidBrandVariant = errors.New("invalid brand variant")

// ErrInvalidBrandLogo 表示上传的不是合法 PNG 或超出大小上限。
var ErrInvalidBrandLogo = errors.New("brand logo must be a PNG within size limit")

// Service 是管理后台服务。
type Service struct {
	store      yggdrasil.Store
	adminToken string
	providers  map[string]string // providerID -> label
	files      storage.Storage   // 品牌图标等管理上传的二进制（可选注入）
}

// NewService 构造管理服务。
func NewService(store yggdrasil.Store, adminToken string, providers map[string]string) *Service {
	return &Service{store: store, adminToken: adminToken, providers: providers}
}

// SetStorage 注入对象存储（品牌图标上传/读取用）。
func (s *Service) SetStorage(f storage.Storage) { s.files = f }

func (s *Service) defaultMethods() []LoginMethod {
	methods := []LoginMethod{{ID: "password", Type: "password", Label: "Password", Enabled: true}}
	ids := make([]string, 0, len(s.providers))
	for id := range s.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		methods = append(methods, LoginMethod{ID: id, Type: "oauth", Label: s.providers[id], Enabled: true})
	}
	for i := range methods {
		methods[i].Order = i
	}
	return methods
}

// LoginMethods 返回登录方式（默认顺序 + 管理员覆盖）。
func (s *Service) LoginMethods(ctx context.Context) ([]LoginMethod, error) {
	defaults := s.defaultMethods()
	raw, err := s.store.GetSetting(ctx, settingLoginMethods)
	if err != nil || len(raw) == 0 {
		return defaults, nil
	}
	var saved []LoginMethod
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) == 0 {
		return defaults, nil
	}
	byID := map[string]LoginMethod{}
	for _, d := range defaults {
		byID[d.ID] = d
	}
	seen := map[string]bool{}
	out := make([]LoginMethod, 0, len(defaults))
	for _, sm := range saved {
		if d, ok := byID[sm.ID]; ok {
			d.Enabled = sm.Enabled
			out = append(out, d)
			seen[sm.ID] = true
		}
	}
	for _, d := range defaults {
		if !seen[d.ID] {
			out = append(out, d)
		}
	}
	for i := range out {
		out[i].Order = i
	}
	return out, nil
}

// UpdateLoginMethods 持久化登录方式顺序与开关。
func (s *Service) UpdateLoginMethods(ctx context.Context, methods []LoginMethod) error {
	valid := map[string]bool{}
	for _, d := range s.defaultMethods() {
		valid[d.ID] = true
	}
	cleaned := make([]LoginMethod, 0, len(methods))
	for _, m := range methods {
		if valid[m.ID] {
			cleaned = append(cleaned, LoginMethod{ID: m.ID, Enabled: m.Enabled})
		}
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, settingLoginMethods, b)
}

// ListUsers 返回用户列表。
func (s *Service) ListUsers(ctx context.Context, query string, limit, offset int) ([]yggdrasil.AdminUser, int64, error) {
	users, err := s.store.ListUsers(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountUsers(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UpdateUser 更新用户角色/状态。
func (s *Service) UpdateUser(ctx context.Context, userID, role, status string) error {
	if role != "" && role != "user" && role != "admin" {
		return errors.New("invalid role")
	}
	if status != "" && status != "active" && status != "banned" && status != "suspended" {
		return errors.New("invalid status")
	}
	return s.store.UpdateUser(ctx, userID, role, status)
}

// GenerateInviteCodes 生成 n 个邀请码。
func (s *Service) GenerateInviteCodes(ctx context.Context, n int) ([]string, error) {
	if n < 1 || n > 100 {
		return nil, errors.New("n out of range")
	}
	codes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		code := inviteCode()
		if err := s.store.CreateInviteCode(ctx, code); err != nil {
			return codes, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// DeleteSkinLibItem 删除皮肤库条目。
func (s *Service) DeleteSkinLibItem(ctx context.Context, itemID string) error {
	return s.store.DeleteSkinLibItem(ctx, itemID)
}

// DeleteComment 删除皮肤库评论（审核）。
func (s *Service) DeleteComment(ctx context.Context, commentID string) error {
	return s.store.DeleteSkinComment(ctx, commentID)
}

// Announcement 返回当前公告。
func (s *Service) Announcement(ctx context.Context) (Announcement, error) {
	raw, err := s.store.GetSetting(ctx, settingAnnouncement)
	if err != nil {
		return Announcement{}, nil
	}
	var a Announcement
	if err := json.Unmarshal(raw, &a); err != nil {
		return Announcement{}, nil
	}
	return a, nil
}

// SetAnnouncement 设置公告。
func (s *Service) SetAnnouncement(ctx context.Context, a Announcement) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, settingAnnouncement, b)
}

func inviteCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// brandKey 是品牌图标在对象存储中的固定键（非内容寻址：品牌图需稳定 URL）。
func brandKey(v BrandVariant) string { return "brand/logo-" + string(v) + ".png" }

// ParseBrandVariant 把路径参数解析为合法变体。
func ParseBrandVariant(s string) (BrandVariant, error) {
	switch BrandVariant(s) {
	case BrandLight, BrandDark:
		return BrandVariant(s), nil
	default:
		return "", ErrInvalidBrandVariant
	}
}

// pngSignature 是 PNG 文件头；品牌图标只接受 PNG（与皮肤纹理一致，避免 SVG 脚本面）。
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Brand 返回当前品牌图标配置（未设置过则为零值）。
func (s *Service) Brand(ctx context.Context) (BrandConfig, error) {
	raw, err := s.store.GetSetting(ctx, settingBrand)
	if err != nil {
		return BrandConfig{}, nil
	}
	var c BrandConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return BrandConfig{}, nil
	}
	return c, nil
}

// SetBrandLogo 校验并存储某变体的品牌图标，版本号自增用于客户端缓存击穿。
func (s *Service) SetBrandLogo(ctx context.Context, v BrandVariant, data []byte) error {
	if s.files == nil {
		return ErrBrandStorage
	}
	if len(data) == 0 || len(data) > MaxBrandLogoBytes || !bytes.HasPrefix(data, pngSignature) {
		return ErrInvalidBrandLogo
	}
	if _, err := s.files.Put(ctx, storage.Object{
		Key:         brandKey(v),
		Size:        int64(len(data)),
		ContentType: "image/png",
		Body:        bytes.NewReader(data),
	}); err != nil {
		return err
	}
	return s.touchBrand(ctx, func(c *BrandConfig) {
		if v == BrandLight {
			c.Light = true
		} else {
			c.Dark = true
		}
	})
}

// DeleteBrandLogo 移除某变体的自定义图标（回落到前端内置默认图）。
func (s *Service) DeleteBrandLogo(ctx context.Context, v BrandVariant) error {
	if s.files == nil {
		return ErrBrandStorage
	}
	_ = s.files.Delete(ctx, brandKey(v))
	return s.touchBrand(ctx, func(c *BrandConfig) {
		if v == BrandLight {
			c.Light = false
		} else {
			c.Dark = false
		}
	})
}

// BrandLogo 读取某变体的自定义图标字节；未自定义时返回 io.EOF 供 handler 映射 404。
func (s *Service) BrandLogo(ctx context.Context, v BrandVariant) ([]byte, error) {
	if s.files == nil {
		return nil, io.EOF
	}
	rc, err := s.files.Get(ctx, brandKey(v))
	if err != nil {
		return nil, io.EOF
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, MaxBrandLogoBytes+1))
}

// touchBrand 更新品牌配置并自增版本号（单一写入点，避免 version 漏加）。
func (s *Service) touchBrand(ctx context.Context, mutate func(*BrandConfig)) error {
	c, err := s.Brand(ctx)
	if err != nil {
		return err
	}
	mutate(&c)
	c.Version++
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, settingBrand, b)
}
