// Package admin 提供管理后台：用户管理、登录方式、公告、邀请码、皮肤库审核。
package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
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
)

// Service 是管理后台服务。
type Service struct {
	store      yggdrasil.Store
	adminToken string
	providers  map[string]string // providerID -> label
}

// NewService 构造管理服务。
func NewService(store yggdrasil.Store, adminToken string, providers map[string]string) *Service {
	return &Service{store: store, adminToken: adminToken, providers: providers}
}

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
