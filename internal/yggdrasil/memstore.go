package yggdrasil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/shared/mc"
)

// MemoryStore 是 Store 的内存参考实现（开发/测试用；生产用 pgstore）。
type MemoryStore struct {
	mu          sync.RWMutex
	users       map[string]string    // email -> passwordHash
	userIDs     map[string]string    // email -> userID
	nicknames   map[string]string    // userID -> nickname
	verified    map[string]bool      // userID -> verified
	players     map[string][]*Player // userID -> players
	byUUID      map[string]*ProfileData
	sessions    map[string]*Session // accessToken -> session
	serverBinds map[string]string   // serverID -> playerID
	tokens      map[string]tokenInfo
	inviteCodes map[string]bool
	skinlib     map[string]*SkinLibItem
	skinlibOrder []string
}

type tokenInfo struct {
	userID    string
	kind      string
	expiresAt time.Time
}

// NewMemoryStore 返回带种子数据的 Store：admin/password，玩家 Alice。
func NewMemoryStore() *MemoryStore {
	adminID := "seed-admin"
	alice := &Player{ID: mc.UUIDFromName("Alice"), Name: "Alice"}
	adminHash, _ := passwd.Hash("password")
	return &MemoryStore{
		users:       map[string]string{"admin": adminHash},
		userIDs:     map[string]string{"admin": adminID},
		nicknames:   map[string]string{adminID: "Admin"},
		verified:    map[string]bool{adminID: true},
		players:     map[string][]*Player{adminID: {alice}},
		byUUID:      map[string]*ProfileData{alice.ID: &ProfileData{Player: *alice}},
		sessions:    map[string]*Session{},
		serverBinds: map[string]string{},
		tokens:      map[string]tokenInfo{},
		inviteCodes: map[string]bool{},
		skinlib:     map[string]*SkinLibItem{},
		skinlibOrder: []string{},
	}
}

func (m *MemoryStore) Authenticate(_ context.Context, username, password string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hash, ok := m.users[username]
	if !ok {
		return "", ErrInvalidCredentials
	}
	valid, err := passwd.Verify(password, hash)
	if err != nil || !valid {
		return "", ErrInvalidCredentials
	}
	return m.userIDs[username], nil
}

func (m *MemoryStore) PlayerByUser(_ context.Context, userID string) (*Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ps := m.players[userID]
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	return ps[0], nil
}

func (m *MemoryStore) PlayersByUser(_ context.Context, userID string) ([]Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Player, 0, len(m.players[userID]))
	for _, p := range m.players[userID] {
		out = append(out, *p)
	}
	return out, nil
}

func (m *MemoryStore) AddPlayer(_ context.Context, userID, name string) (*Player, error) {
	player := &Player{ID: mc.UUIDFromName(name), Name: name}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pd := range m.byUUID {
		if pd.Player.Name == name {
			return nil, ErrAlreadyExists
		}
	}
	m.players[userID] = append(m.players[userID], player)
	m.byUUID[player.ID] = &ProfileData{Player: *player}
	return player, nil
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

func (m *MemoryStore) SetPremium(_ context.Context, uuid string, premium bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pd, ok := m.byUUID[uuid]
	if !ok {
		return ErrNotFound
	}
	pd.Premium = premium
	return nil
}

func (m *MemoryStore) CreateAccount(_ context.Context, email, passwordHash, nickname, playerName string) (*Account, error) {
	id := "user-" + randomToken()[:16]
	player := &Player{ID: mc.UUIDFromName(playerName), Name: playerName}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.userIDs[email]; exists {
		return nil, ErrAlreadyExists
	}
	for _, pd := range m.byUUID {
		if pd.Player.Name == playerName {
			return nil, ErrAlreadyExists
		}
	}
	m.users[email] = passwordHash
	m.userIDs[email] = id
	m.nicknames[id] = nickname
	m.players[id] = []*Player{player}
	m.byUUID[player.ID] = &ProfileData{Player: *player}
	return &Account{ID: id, Email: email, Nickname: nickname, Verified: false}, nil
}

func (m *MemoryStore) AccountByEmail(_ context.Context, email string) (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.userIDs[email]
	if !ok {
		return nil, ErrNotFound
	}
	return &Account{ID: id, Email: email, Nickname: m.nicknames[id], Verified: m.verified[id]}, nil
}

func (m *MemoryStore) CreateToken(_ context.Context, userID, kind string, ttl time.Duration) (string, error) {
	token := randomToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[token] = tokenInfo{userID: userID, kind: kind, expiresAt: time.Now().Add(ttl)}
	return token, nil
}

func (m *MemoryStore) ConsumeToken(_ context.Context, kind, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ti, ok := m.tokens[token]
	if !ok || ti.kind != kind || time.Now().After(ti.expiresAt) {
		return "", ErrInvalidToken
	}
	delete(m.tokens, token)
	return ti.userID, nil
}

func (m *MemoryStore) MarkVerified(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.players[userID]) == 0 {
		return ErrNotFound
	}
	m.verified[userID] = true
	return nil
}

func (m *MemoryStore) UpdatePassword(_ context.Context, userID, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var email string
	for e, id := range m.userIDs {
		if id == userID {
			email = e
			break
		}
	}
	if email == "" {
		return ErrNotFound
	}
	m.users[email] = passwordHash
	return nil
}

func (m *MemoryStore) CreateInviteCode(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inviteCodes[code] = true
	return nil
}

func (m *MemoryStore) ConsumeInviteCode(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.inviteCodes[code] {
		return ErrInvalidToken
	}
	delete(m.inviteCodes, code)
	return nil
}

func (m *MemoryStore) PublishSkin(_ context.Context, ownerUserID, playerName, textureKey, description, model string) (*SkinLibItem, error) {
	id := "sl-" + randomToken()[:16]
	item := &SkinLibItem{ID: id, OwnerUserID: ownerUserID, PlayerName: playerName, TextureKey: textureKey, Description: description, Model: model}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skinlib[id] = item
	m.skinlibOrder = append(m.skinlibOrder, id)
	return item, nil
}

func (m *MemoryStore) ListSkinLib(_ context.Context, offset, limit int) ([]SkinLibItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []SkinLibItem
	for i := len(m.skinlibOrder) - 1; i >= 0; i-- {
		if offset > 0 {
			offset--
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, *m.skinlib[m.skinlibOrder[i]])
	}
	return out, nil
}

func (m *MemoryStore) LikeSkin(_ context.Context, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.skinlib[itemID]
	if !ok {
		return ErrNotFound
	}
	item.Likes++
	return nil
}

func (m *MemoryStore) CommentSkin(_ context.Context, itemID, userID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skinlib[itemID]; !ok {
		return ErrNotFound
	}
	return nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
