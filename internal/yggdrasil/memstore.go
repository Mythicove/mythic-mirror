package yggdrasil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/shared/mc"
	"github.com/google/uuid"
)

// MemoryStore 是 Store 的内存参考实现（开发/测试用；生产用 pgstore）。
type MemoryStore struct {
	mu           sync.RWMutex
	users        map[string]string           // email -> passwordHash
	userIDs      map[string]string           // email -> userID
	nicknames    map[string]string           // userID -> nickname
	verified     map[string]bool             // userID -> verified
	players      map[string][]*Player        // userID -> players
	credentials  map[string][]*memCredential // playerUUID -> 启动器独立密码
	identities   map[string][]Identity       // userID -> 第三方绑定
	byUUID       map[string]*ProfileData
	sessions     map[string]*Session // accessToken -> session
	serverBinds  map[string]string   // serverID -> playerID
	tokens       map[string]tokenInfo
	inviteCodes  map[string]bool
	skinlib      map[string]*SkinLibItem
	skinlibOrder []string
	skinComments []*SkinComment
	reports      []*Report
	audit        []AuditEntry
	roles        map[string]string // userID -> role
	statuses     map[string]string // userID -> status
	settings     map[string][]byte
	tiers        map[string]string     // userID -> storage tier（默认 free）
	fileRecords  map[string]fileRecord // 纹理键 -> 归属与字节数（配额统计，sha256 去重）
}

type fileRecord struct {
	owner string
	size  int64
}

type tokenInfo struct {
	userID    string
	kind      string
	expiresAt time.Time
}

// memCredential 是一条启动器独立密码（内存实现）。
type memCredential struct {
	id         string
	name       string
	hash       string
	createdAt  time.Time
	lastUsedAt time.Time // 零值 = 从未使用
}

// NewMemoryStore 返回带种子数据的 Store：主密码 admin@example.com/password，玩家 Alice，
// 以及 Alice 的一条启动器独立密码（同为 "password"，故启动器登录用 Alice/password）。
func NewMemoryStore() *MemoryStore {
	adminID := "seed-admin"
	alice := &Player{ID: mc.UUIDFromName("Alice"), Name: "Alice"}
	adminHash, _ := passwd.Hash("password")
	return &MemoryStore{
		users:     map[string]string{"admin@example.com": adminHash},
		userIDs:   map[string]string{"admin@example.com": adminID},
		nicknames: map[string]string{adminID: "Admin"},
		verified:  map[string]bool{adminID: true},
		players:   map[string][]*Player{adminID: {alice}},
		credentials: map[string][]*memCredential{alice.ID: {{
			id: "seed-cred", name: "seed", hash: adminHash, createdAt: time.Now(),
		}}},
		identities:   map[string][]Identity{},
		byUUID:       map[string]*ProfileData{alice.ID: {Player: *alice, UserID: adminID}},
		sessions:     map[string]*Session{},
		serverBinds:  map[string]string{},
		tokens:       map[string]tokenInfo{},
		inviteCodes:  map[string]bool{},
		skinlib:      map[string]*SkinLibItem{},
		skinlibOrder: []string{},
		roles:        map[string]string{adminID: "admin"},
		statuses:     map[string]string{adminID: "active"},
		settings:     map[string][]byte{},
		tiers:        map[string]string{},
		fileRecords:  map[string]fileRecord{},
	}
}

func (m *MemoryStore) UserStorage(_ context.Context, userID string) (string, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tier := m.tiers[userID]
	if tier == "" {
		tier = "free"
	}
	var used int64
	for _, rec := range m.fileRecords {
		if rec.owner == userID {
			used += rec.size
		}
	}
	return tier, used, nil
}

func (m *MemoryStore) CountPublished(_ context.Context, userID string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var n int64
	for _, item := range m.skinlib {
		if item.OwnerUserID == userID {
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) RecordTexture(_ context.Context, key, ownerUserID string, size int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.fileRecords[key]; ok {
		return false, nil
	}
	m.fileRecords[key] = fileRecord{owner: ownerUserID, size: size}
	return true, nil
}

func (m *MemoryStore) UnrecordTexture(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fileRecords, key)
	return nil
}

// Authenticate 校验网站主密码；被封禁/停用的账号不给会话（与 pgstore 的 status 过滤一致）。
func (m *MemoryStore) Authenticate(_ context.Context, email, password string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hash, ok := m.users[email]
	if !ok {
		return "", ErrInvalidCredentials
	}
	valid, err := passwd.Verify(password, hash)
	if err != nil || !valid {
		return "", ErrInvalidCredentials
	}
	userID := m.userIDs[email]
	if m.statuses[userID] != "active" {
		return "", ErrInvalidCredentials
	}
	return userID, nil
}

// AuthenticatePlayer 校验启动器登录（玩家名 + 该玩家的独立密码），主密码不参与。
func (m *MemoryStore) AuthenticatePlayer(_ context.Context, playerName, password string) (*LauncherAuth, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pd := m.byPlayerNameLocked(playerName)
	if pd == nil {
		return nil, ErrInvalidCredentials
	}
	if m.statuses[pd.UserID] != "active" {
		return nil, ErrAccountDisabled
	}
	for _, c := range m.credentials[pd.Player.ID] {
		ok, err := passwd.Verify(password, c.hash)
		if err != nil || !ok {
			continue
		}
		c.lastUsedAt = time.Now()
		return &LauncherAuth{Player: pd.Player, UserID: pd.UserID, CredentialID: c.id}, nil
	}
	return nil, ErrInvalidCredentials
}

// byPlayerNameLocked 按玩家名找档案。调用方须持读锁或写锁。
func (m *MemoryStore) byPlayerNameLocked(name string) *ProfileData {
	for _, pd := range m.byUUID {
		if pd.Player.Name == name {
			return pd
		}
	}
	return nil
}

func (m *MemoryStore) PlayersByUser(_ context.Context, userID string) ([]Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Player, 0, len(m.players[userID]))
	for _, p := range m.players[userID] {
		cp := *p
		if pd := m.byUUID[p.ID]; pd != nil {
			cp.Premium = pd.PremiumUUID != ""
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *MemoryStore) CreatePlayerCredential(_ context.Context, playerUUID, name, passwordHash string) (*PlayerCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := &memCredential{id: uuid.NewString(), name: name, hash: passwordHash, createdAt: time.Now()}
	m.credentials[playerUUID] = append(m.credentials[playerUUID], c)
	return m.credentialViewLocked(c), nil
}

func (m *MemoryStore) ListPlayerCredentials(_ context.Context, playerUUID string) ([]PlayerCredential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.credentials[playerUUID]
	out := make([]PlayerCredential, 0, len(list))
	for _, c := range list {
		out = append(out, *m.credentialViewLocked(c))
	}
	return out, nil
}

// DeletePlayerCredential 撤销独立密码，并连带销毁由它建立的会话（对齐 pgstore 的外键级联）。
func (m *MemoryStore) DeletePlayerCredential(_ context.Context, playerUUID, credentialID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.credentials[playerUUID]
	for i, c := range list {
		if c.id != credentialID {
			continue
		}
		m.credentials[playerUUID] = append(list[:i], list[i+1:]...)
		for tok, s := range m.sessions {
			if s.CredentialID == credentialID {
				delete(m.sessions, tok)
			}
		}
		return nil
	}
	return ErrNotFound
}

// credentialViewLocked 把内部凭证转成对外视图（不含哈希），最近使用的排前面。
// 调用方须持读锁或写锁。
func (m *MemoryStore) credentialViewLocked(c *memCredential) *PlayerCredential {
	view := &PlayerCredential{ID: c.id, Name: c.name, CreatedAt: c.createdAt.Format(time.RFC3339)}
	if !c.lastUsedAt.IsZero() {
		view.LastUsedAt = c.lastUsedAt.Format(time.RFC3339)
	}
	return view
}

func (m *MemoryStore) UpsertIdentity(_ context.Context, provider, subject, userID, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range m.identities[userID] {
		if id.Provider == provider && id.Subject == subject {
			m.identities[userID][i].Email = email
			return nil
		}
	}
	m.identities[userID] = append(m.identities[userID], Identity{
		Provider: provider, Subject: subject, Email: email, CreatedAt: time.Now().Format(time.RFC3339),
	})
	return nil
}

func (m *MemoryStore) IdentityUser(_ context.Context, provider, subject string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for userID, ids := range m.identities {
		for _, id := range ids {
			if id.Provider == provider && id.Subject == subject {
				return userID, nil
			}
		}
	}
	return "", ErrNotFound
}

func (m *MemoryStore) Identities(_ context.Context, userID string) ([]Identity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Identity, len(m.identities[userID]))
	copy(out, m.identities[userID])
	return out, nil
}

func (m *MemoryStore) CountPasswordOnlyUsers(_ context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var n int64
	for email, hash := range m.users {
		userID := m.userIDs[email]
		if hash == "" || m.statuses[userID] != "active" || !m.verified[userID] {
			continue
		}
		if len(m.identities[userID]) == 0 {
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) RenamePlayer(_ context.Context, userID, oldName, newName string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var target *Player
	for _, p := range m.players[userID] {
		if p.Name == oldName {
			target = p
			break
		}
	}
	if target == nil {
		return nil, ErrNotFound
	}
	oldID := target.ID
	pd := m.byUUID[oldID]
	if pd != nil && pd.PremiumUUID != "" {
		return nil, ErrPremiumBound
	}
	newID := mc.UUIDFromName(newName)
	for _, other := range m.byUUID {
		if other.Player.Name == newName || other.Player.ID == newID {
			return nil, ErrAlreadyExists
		}
	}
	delete(m.byUUID, oldID)
	target.ID = newID
	target.Name = newName
	if pd != nil {
		pd.Player = *target
		m.byUUID[newID] = pd
	}
	// 旧 uuid 的会话/服务器绑定已无对应档案，一律失效（启动器需重新登录）；
	// 独立密码跟随玩家本身（对齐 pgstore 的 ON UPDATE CASCADE），不失效。
	m.dropPlayerSessionsLocked(oldID)
	if creds := m.credentials[oldID]; creds != nil {
		m.credentials[newID] = creds
		delete(m.credentials, oldID)
	}
	cp := *target
	return &cp, nil
}

func (m *MemoryStore) DeletePlayer(_ context.Context, userID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := m.players[userID]
	idx := -1
	for i, p := range ps {
		if p.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	if len(ps) <= 1 {
		return ErrLastPlayer
	}
	victim := ps[idx]
	m.players[userID] = append(ps[:idx], ps[idx+1:]...)
	delete(m.byUUID, victim.ID)
	m.dropPlayerSessionsLocked(victim.ID)
	delete(m.credentials, victim.ID)
	return nil
}

// dropPlayerSessionsLocked 清掉指向某玩家的会话与服务器绑定。调用方须持写锁。
func (m *MemoryStore) dropPlayerSessionsLocked(playerID string) {
	for tok, s := range m.sessions {
		if s.PlayerID == playerID {
			delete(m.sessions, tok)
		}
	}
	for sid, pid := range m.serverBinds {
		if pid == playerID {
			delete(m.serverBinds, sid)
		}
	}
}

func (m *MemoryStore) AddPlayer(_ context.Context, userID, name string, limit int) (*Player, error) {
	player := &Player{ID: mc.UUIDFromName(name), Name: name}
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > 0 && len(m.players[userID]) >= limit {
		return nil, ErrPlayerLimit
	}
	for _, pd := range m.byUUID {
		if pd.Player.Name == name {
			return nil, ErrAlreadyExists
		}
	}
	m.players[userID] = append(m.players[userID], player)
	m.byUUID[player.ID] = &ProfileData{Player: *player, UserID: userID}
	return player, nil
}

// CreateSession 建会话；playerID/credentialID 均空 = 网站/OIDC 会话（不代表任何玩家）。
func (m *MemoryStore) CreateSession(_ context.Context, userID, playerID, credentialID, clientToken string) (string, error) {
	accessToken := randomToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[accessToken] = &Session{
		UserID: userID, PlayerID: playerID, CredentialID: credentialID,
		AccessToken: accessToken, ClientToken: clientToken,
	}
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
	m.sessions[newToken] = &Session{
		UserID: s.UserID, PlayerID: s.PlayerID, CredentialID: s.CredentialID,
		AccessToken: newToken, ClientToken: s.ClientToken,
	}
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

func (m *MemoryStore) Signout(_ context.Context, playerName, password string) error {
	auth, err := m.AuthenticatePlayer(context.Background(), playerName, password)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for tok, s := range m.sessions {
		if s.PlayerID == auth.Player.ID {
			delete(m.sessions, tok)
		}
	}
	return nil
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

func (m *MemoryStore) SetPremium(_ context.Context, uuid, premiumUUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pd, ok := m.byUUID[uuid]
	if !ok {
		return ErrNotFound
	}
	pd.PremiumUUID = premiumUUID
	return nil
}

func (m *MemoryStore) CreateAccount(_ context.Context, email, passwordHash, nickname, playerName string) (*Account, error) {
	id := uuid.NewString()
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
	m.byUUID[player.ID] = &ProfileData{Player: *player, UserID: id}
	m.roles[id] = "user"
	m.statuses[id] = "active"
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

func (m *MemoryStore) AccountByID(_ context.Context, userID string) (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.nicknames[userID]; !ok {
		return nil, ErrNotFound
	}
	var email string
	for e, id := range m.userIDs {
		if id == userID {
			email = e
			break
		}
	}
	return &Account{ID: userID, Email: email, Nickname: m.nicknames[userID], Verified: m.verified[userID]}, nil
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

func (m *MemoryStore) PublishSkin(_ context.Context, ownerUserID, playerName, textureKey, description, model, status string) (*SkinLibItem, error) {
	if status == "" {
		status = "listed"
	}
	id := uuid.NewString()
	item := &SkinLibItem{ID: id, OwnerUserID: ownerUserID, PlayerName: playerName, TextureKey: textureKey, Description: description, Model: model, Status: status, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skinlib[id] = item
	m.skinlibOrder = append(m.skinlibOrder, id)
	return item, nil
}

func (m *MemoryStore) ListSkinLib(_ context.Context, query, sort string, offset, limit int) ([]SkinLibItem, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 过滤（公开列表只含 listed）
	var matched []SkinLibItem
	for i := len(m.skinlibOrder) - 1; i >= 0; i-- {
		it := *m.skinlib[m.skinlibOrder[i]]
		if it.Status != "" && it.Status != "listed" {
			continue
		}
		if query != "" && !containsFold(it.PlayerName, query) && !containsFold(it.Description, query) {
			continue
		}
		matched = append(matched, it)
	}
	total := int64(len(matched))
	if sort == "likes" {
		sortByLikes(matched)
	}
	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], total, nil
}

func (m *MemoryStore) AdminListSkins(_ context.Context, status string, limit, offset int) ([]SkinLibItem, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matched []SkinLibItem
	for i := len(m.skinlibOrder) - 1; i >= 0; i-- {
		it := *m.skinlib[m.skinlibOrder[i]]
		if status != "" && it.Status != status {
			continue
		}
		matched = append(matched, it)
	}
	total := int64(len(matched))
	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], total, nil
}

func (m *MemoryStore) SetSkinStatus(_ context.Context, itemID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.skinlib[itemID]
	if !ok {
		return ErrNotFound
	}
	item.Status = status
	return nil
}

func (m *MemoryStore) ReportTarget(_ context.Context, reporterUserID, targetType, targetID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.reports {
		if r.ReporterUserID == reporterUserID && r.TargetType == targetType && r.TargetID == targetID {
			return ErrAlreadyExists
		}
	}
	name := m.nicknames[reporterUserID]
	m.reports = append(m.reports, &Report{
		ID: uuid.NewString(), ReporterUserID: reporterUserID, ReporterName: name,
		TargetType: targetType, TargetID: targetID, Reason: reason, Status: "pending",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func (m *MemoryStore) ListReports(_ context.Context, status string, limit, offset int) ([]Report, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matched []Report
	for i := len(m.reports) - 1; i >= 0; i-- {
		r := *m.reports[i]
		if status != "" && r.Status != status {
			continue
		}
		matched = append(matched, r)
	}
	total := int64(len(matched))
	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], total, nil
}

func (m *MemoryStore) ResolveReport(_ context.Context, reportID, reviewerUserID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.reports {
		if r.ID == reportID {
			r.Status = status
			r.ReviewedBy = reviewerUserID
			r.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
			return nil
		}
	}
	return ErrNotFound
}

// sortByLikes 按点赞数降序稳定排序（同票保持传入顺序，即最新在前）。
func sortByLikes(items []SkinLibItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Likes > items[j-1].Likes; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func (m *MemoryStore) GetSkinLibItem(_ context.Context, itemID string) (*SkinLibItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.skinlib[itemID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *item
	return &cp, nil
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
	m.skinComments = append(m.skinComments, &SkinComment{
		ID:         uuid.NewString(),
		ItemID:     itemID,
		UserID:     userID,
		AuthorName: m.nicknames[userID],
		Content:    content,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func (m *MemoryStore) ListSkinComments(_ context.Context, itemID string) ([]SkinComment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []SkinComment{}
	for _, c := range m.skinComments {
		if c.ItemID == itemID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *MemoryStore) DeleteSkinComment(_ context.Context, commentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.skinComments {
		if c.ID == commentID {
			m.skinComments = append(m.skinComments[:i], m.skinComments[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryStore) WriteAudit(_ context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	m.audit = append(m.audit, e)
	return nil
}

func (m *MemoryStore) ListAudit(_ context.Context, q string, limit, offset int) ([]AuditEntry, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	match := func(e AuditEntry) bool {
		return q == "" || containsFold(e.Action, q) || containsFold(e.Actor, q) || containsFold(e.Target, q)
	}
	var filtered []AuditEntry
	for _, e := range m.audit {
		if match(e) {
			filtered = append(filtered, e)
		}
	}
	total := int64(len(filtered))
	out := make([]AuditEntry, 0, limit)
	for i := len(filtered) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, filtered[i])
	}
	return out, total, nil
}

func (m *MemoryStore) ListUsers(_ context.Context, query string, limit, offset int) ([]AdminUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []AdminUser
	skipped := 0
	for email, id := range m.userIDs {
		if query != "" && !containsFold(email, query) && !containsFold(m.nicknames[id], query) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(out) >= limit {
			break
		}
		names := make([]string, 0, len(m.players[id]))
		for _, p := range m.players[id] {
			names = append(names, p.Name)
		}
		role := m.roles[id]
		if role == "" {
			role = "user"
		}
		status := m.statuses[id]
		if status == "" {
			status = "active"
		}
		out = append(out, AdminUser{
			ID: id, Email: email, Nickname: m.nicknames[id], Role: role, Status: status,
			Verified: m.verified[id], PlayerNames: names,
		})
	}
	return out, nil
}

func (m *MemoryStore) CountUsers(_ context.Context, query string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var n int64
	for email, id := range m.userIDs {
		if query != "" && !containsFold(email, query) && !containsFold(m.nicknames[id], query) {
			continue
		}
		n++
	}
	return n, nil
}

func (m *MemoryStore) UpdateUser(_ context.Context, userID, role, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nicknames[userID]; !ok {
		return ErrNotFound
	}
	m.roles[userID] = role
	m.statuses[userID] = status
	return nil
}

func (m *MemoryStore) UserRole(_ context.Context, userID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	role, ok := m.roles[userID]
	if !ok {
		return "", ErrNotFound
	}
	if role == "" {
		role = "user"
	}
	return role, nil
}

func (m *MemoryStore) DeleteSkinLibItem(_ context.Context, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skinlib[itemID]; !ok {
		return ErrNotFound
	}
	delete(m.skinlib, itemID)
	return nil
}

func (m *MemoryStore) SetSetting(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[key] = value
	return nil
}

func (m *MemoryStore) GetSetting(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.settings[key]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
