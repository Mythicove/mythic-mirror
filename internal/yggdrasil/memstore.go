package yggdrasil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/Mythicove/shared/mc"
)

// MemoryStore 是 Store 的内存参考实现（开发/测试用；M3 换 Postgres）。
// 密码为明文比较，仅用于 M1 联调；正式实现用 Argon2id（ADR-0007）。
type MemoryStore struct {
	mu          sync.RWMutex
	users       map[string]string  // username -> password
	userIDs     map[string]string  // username -> userID
	players     map[string]*Player // userID -> player（单玩家简化）
	byUUID      map[string]*ProfileData
	sessions    map[string]*Session // accessToken -> session
	serverBinds map[string]string   // serverID -> playerID
}

// NewMemoryStore 返回带种子数据的 Store：admin/password，玩家 Alice。
func NewMemoryStore() *MemoryStore {
	adminID := "seed-admin"
	alice := &Player{ID: mc.UUIDFromName("Alice"), Name: "Alice"}
	return &MemoryStore{
		users:       map[string]string{"admin": "password"},
		userIDs:     map[string]string{"admin": adminID},
		players:     map[string]*Player{adminID: alice},
		byUUID:      map[string]*ProfileData{alice.ID: &ProfileData{Player: *alice}},
		sessions:    map[string]*Session{},
		serverBinds: map[string]string{},
	}
}

func (m *MemoryStore) Authenticate(_ context.Context, username, password string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pw, ok := m.users[username]
	if !ok || pw != password {
		return "", ErrInvalidCredentials
	}
	return m.userIDs[username], nil
}

func (m *MemoryStore) PlayerByUser(_ context.Context, userID string) (*Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.players[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *MemoryStore) CreateSession(_ context.Context, userID, clientToken string) (string, error) {
	p, err := m.PlayerByUser(context.Background(), userID)
	if err != nil {
		return "", err
	}
	accessToken := randomToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[accessToken] = &Session{UserID: userID, PlayerID: p.ID, AccessToken: accessToken, ClientToken: clientToken}
	return accessToken, nil
}

func (m *MemoryStore) Validate(_ context.Context, accessToken, clientToken string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[accessToken]
	if !ok {
		return nil, ErrInvalidToken
	}
	if clientToken != "" && s.ClientToken != clientToken {
		return nil, ErrInvalidToken
	}
	return s, nil
}

func (m *MemoryStore) Refresh(_ context.Context, accessToken, clientToken string) (string, error) {
	s, err := m.Validate(context.Background(), accessToken, clientToken)
	if err != nil {
		return "", err
	}
	newToken := randomToken()
	m.mu.Lock()
	delete(m.sessions, accessToken)
	m.sessions[newToken] = &Session{UserID: s.UserID, PlayerID: s.PlayerID, AccessToken: newToken, ClientToken: s.ClientToken}
	m.mu.Unlock()
	return newToken, nil
}

func (m *MemoryStore) Invalidate(_ context.Context, accessToken, clientToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[accessToken]
	if !ok || (clientToken != "" && s.ClientToken != clientToken) {
		return ErrInvalidToken
	}
	delete(m.sessions, accessToken)
	return nil
}

func (m *MemoryStore) Signout(_ context.Context, username, password string) error {
	_, err := m.Authenticate(context.Background(), username, password)
	return err
}

func (m *MemoryStore) BindServer(_ context.Context, accessToken, selectedProfile, serverID string) error {
	s, err := m.Validate(context.Background(), accessToken, "")
	if err != nil {
		return err
	}
	if s.PlayerID != selectedProfile {
		return ErrInvalidToken
	}
	m.mu.Lock()
	m.serverBinds[serverID] = s.PlayerID
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) HasJoined(_ context.Context, username, serverID string) (*ProfileData, error) {
	m.mu.RLock()
	playerID, ok := m.serverBinds[serverID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	pd, ok := m.byUUID[playerID]
	if !ok || pd.Player.Name != username {
		return nil, ErrNotFound
	}
	return pd, nil
}

func (m *MemoryStore) ProfileByUUID(_ context.Context, uuid string) (*ProfileData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pd, ok := m.byUUID[uuid]
	if !ok {
		return nil, ErrNotFound
	}
	return pd, nil
}

func (m *MemoryStore) ProfileByName(_ context.Context, name string) (*ProfileData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pd := range m.byUUID {
		if pd.Player.Name == name {
			return pd, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryStore) LookupNames(_ context.Context, names []string) ([]Player, error) {
	out := make([]Player, 0, len(names))
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range names {
		for _, pd := range m.byUUID {
			if pd.Player.Name == n {
				out = append(out, pd.Player)
			}
		}
	}
	return out, nil
}

func (m *MemoryStore) SetSkin(_ context.Context, uuid, key, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pd, ok := m.byUUID[uuid]
	if !ok {
		return ErrNotFound
	}
	pd.SkinKey = key
	pd.Model = model
	return nil
}

func (m *MemoryStore) SetCape(_ context.Context, uuid, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pd, ok := m.byUUID[uuid]
	if !ok {
		return ErrNotFound
	}
	pd.CapeKey = key
	return nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
