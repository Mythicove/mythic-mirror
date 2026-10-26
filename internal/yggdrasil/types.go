// Package yggdrasil 实现 Yggdrasil 协议（authlib-injector 服务端规范）。
package yggdrasil

// Player 是玩家标识（profile 摘要）。
type Player struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Legacy bool   `json:"legacy,omitempty"`
	// Active 标记这是账号当前选用的玩家（建会话时优先用它）。
	Active bool `json:"active,omitempty"`
	// Premium 标记已绑定微软正版（premium_uuid 非空）：名字来自 Mojang，不可本地改名。
	Premium bool `json:"premium,omitempty"`
}

// Property 是 profile 属性（textures 等）。
type Property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

// Profile 是带属性的玩家资料。
type Profile struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Properties []Property `json:"properties,omitempty"`
}

// User 是 authenticate 响应里的 user 段。
type User struct {
	ID         string     `json:"id,omitempty"`
	Properties []Property `json:"properties,omitempty"`
}

// AuthenticateRequest 是登录请求。
type AuthenticateRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	ClientToken string `json:"clientToken,omitempty"`
	RequestUser bool   `json:"requestUser,omitempty"`
}

// AuthenticateResponse 是登录响应。
type AuthenticateResponse struct {
	AccessToken       string   `json:"accessToken"`
	ClientToken       string   `json:"clientToken"`
	AvailableProfiles []Player `json:"availableProfiles"`
	SelectedProfile   *Player  `json:"selectedProfile,omitempty"`
	User              *User    `json:"user,omitempty"`
}

// RefreshRequest 是刷新请求。
type RefreshRequest struct {
	AccessToken     string  `json:"accessToken"`
	ClientToken     string  `json:"clientToken"`
	SelectedProfile *Player `json:"selectedProfile,omitempty"`
	RequestUser     bool    `json:"requestUser,omitempty"`
}

// RefreshResponse 是刷新响应。
type RefreshResponse struct {
	AccessToken     string  `json:"accessToken"`
	ClientToken     string  `json:"clientToken"`
	SelectedProfile *Player `json:"selectedProfile,omitempty"`
	User            *User   `json:"user,omitempty"`
}

// ValidateRequest 是校验请求。
type ValidateRequest struct {
	AccessToken string `json:"accessToken"`
	ClientToken string `json:"clientToken,omitempty"`
}

// InvalidateRequest 是失效请求。
type InvalidateRequest struct {
	AccessToken string `json:"accessToken"`
	ClientToken string `json:"clientToken"`
}

// SignoutRequest 是登出请求。
type SignoutRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JoinRequest 是 join 请求。
type JoinRequest struct {
	AccessToken     string `json:"accessToken"`
	SelectedProfile string `json:"selectedProfile"`
	ServerID        string `json:"serverId"`
}

// Textures 是 textures 属性解码后的结构。
type Textures struct {
	Timestamp   int64         `json:"timestamp"`
	ProfileID   string        `json:"profileId"`
	ProfileName string        `json:"profileName"`
	Textures    TexturesValue `json:"textures"`
}

// TexturesValue 承载 SKIN/CAPE。
type TexturesValue struct {
	SKIN *TextureInfo `json:"SKIN,omitempty"`
	CAPE *TextureInfo `json:"CAPE,omitempty"`
}

// TextureInfo 是单个纹理。
type TextureInfo struct {
	URL      string           `json:"url"`
	Metadata *TextureMetadata `json:"metadata,omitempty"`
}

// TextureMetadata 是纹理元数据（如 slim 模型）。
type TextureMetadata struct {
	Model string `json:"model,omitempty"`
}
