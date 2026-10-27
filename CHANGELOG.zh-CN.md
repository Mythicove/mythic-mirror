# Changelog

[English](./CHANGELOG.md) | **简体中文**

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与语义化版本。

## [Unreleased]

### 新增

- **启动器独立密码**（[ADR-0012](https://mythicove.github.io/docs/architecture/adr-0012-credential-separation/)）：`player_credentials` 表（migration 00013），每个玩家最多 3 条，`GET/POST /account/players/{name}/credentials`、`DELETE /account/players/{name}/credentials/{id}`。撤销一条会经外键级联**立即**销毁由它建立的会话（不是等 24 小时过期）；改名时凭证跟随玩家（`ON UPDATE CASCADE`），删除玩家时一并清除
- **第三方绑定落库**：`user_identities(provider, subject, user_id, email)`。第三方登录**先认绑定**，未命中才按 email 匹配既有账号并当场写下绑定，此后 provider 侧改邮箱或邮箱被接管都不改变账号归属。`GET /account/identities` 返回绑定列表与站点是否强制绑定
- **认证策略**（`GET/PUT /api/admin/auth-policy`，存 settings 的 `auth_policy` 行）：`require_third_party_binding`（未绑定者不能用皮肤上传/皮肤库发布与应用/启动器登录/SSO；账号管理、绑定流程、举报与评论不受限）与 `max_players`（单账号玩家数上限，默认 1，存量超额不回收）。GET 附带只读计数 `password_only_users`（有主密码、无绑定、已验证且未封禁），只服务管理面板决策，不在登录热路径上计算

### 变更（破坏性）

- **`POST /authserver/authenticate` 与 `/authserver/signout` 的凭据换了**：`username` 现在是**玩家名**（不再是邮箱），`password` 是该玩家的**独立密码**（主密码在协议端点上完全不被查询）。响应里的 `availableProfiles`/`selectedProfile` 只有认证到的那一个玩家。存量部署升级后，用户在网站上补建独立密码之前无法从启动器登录（网站登录不受影响）
- **`POST /account/login` 响应形状**：从 Yggdrasil 形（含 `availableProfiles`/`selectedProfile`）改为 `{accessToken, clientToken}`。网站会话不代表任何玩家（`sessions.player_id` 为空串），玩哪个由前端从 `GET /account/players` 里选
- **`POST /account/exchange-code`** 不再返回 `playerName`/`playerId`
- **删除"当前玩家"机制**：`users.active_player_uuid` 列、`POST /account/players/{name}/activate`、`Player.active` 字段、`Store.PlayerByUser`/`SetActivePlayer` 全部移除
- `Signout` 由"校验密码后什么都不做"改为真正销毁**该玩家**的游戏会话（协议原文是注销账号所有令牌，但凭证属于单个玩家，越权清掉同账号其他玩家的会话不合理）
- OIDC ID token 的 `player` claim 只在账号恰好一个玩家时给出（多玩家时 IdP 无从知道用户想以谁的身份出现在 RP 上，猜一个不如不发；消费方只把它当显示名末位回退）

### 修复

- `login_methods` 的 `password` 开关此前**纯装饰**（只有 `internal/admin` 读它）：现在真正强制——关闭后 `/account/login`、`/oauth2/login/accept` 拒绝主密码，账号密码注册一并停止，前端登录页不再渲染密码表单。未绑定第三方的历史账号仍可用主密码登录与重置（宽限），绑定后即拒绝；纯密码账号归零时主密码登录自然完全失效，不需要第二个开关
- 被封禁/停用的账号此前仍可通过 `Authenticate` 拿到会话（`GetUserAuth` 不过滤 `status`）：现在一律拒绝；启动器侧另返回 `ErrAccountDisabled`
- `POST /account/players` 超出玩家数上限时返回 500（错误未映射）：现在 409 并带本地化文案

### 安全

- `POST /oauth2/login/accept`（IdP 登录表单，校验的正是主密码）此前只有全站兜底限流，是全站唯一能无限速试密码的端点：现在与 `/account/login`、`/authserver/*` 共用按 IP 的 `auth` 桶（10/s）
- 需要用户动作的拒绝返回**稳定错误码**而非本地化文案（`binding_required`、`password_login_disabled`），前端据此引导，不做文案匹配

### 新增（此前轮次，仍未发布）

- 验证码 driver 新增 [Cap](https://github.com/tiagozip/cap)（自托管 PoW 验证码）：`MIRROR_CAPTCHA_DRIVER=cap` + `MIRROR_CAPTCHA_CAP_URL`（Standalone 实例公网地址）+ `MIRROR_CAPTCHA_SITE_KEY`/`MIRROR_CAPTCHA_SECRET`；服务端校验走 Cap 的 reCAPTCHA 兼容 `POST {instance}/{siteKey}/siteverify`（JSON 体）；`/api/register-requirements` 增发 `captchaCapUrl` 供前端加载控件
- 验证码保护范围可配：`MIRROR_CAPTCHA_SCOPES`（逗号分隔，默认 `register`，可选 `login`，未登记值启动告警剔除）；`/api/register-requirements` 增发 `captchaScopes`。新增 web 专用登录端点 `POST /account/login`（login scope 启用时强制验证码，与 `/authserver/authenticate` 共用按 IP 的 auth 限流桶）；启动器走的 Yggdrasil `/authserver/authenticate` 永不强制验证码，游戏登录不受影响
- 品牌图标可管理：新增 `GET /api/brand`、`GET /api/brand/logo/{light,dark}`（公开）与 `GET/PUT/DELETE /api/admin/brand[/logo/{variant}]`（管理，审计落库）。自定义 PNG 存对象存储（local/S3），版本号用于缓存击穿；未自定义时前端回落内置默认图
- 站点信息/外观可管理：新增 `GET /api/site`、`GET /site/custom.css`、`GET /site/custom.js`（公开）与 `GET/PUT /api/admin/site`（管理，审计落库）。覆盖站点名/描述/版权/备案与自定义 CSS/JS；自定义资产以同源文件下发，兼容 `script-src/style-src 'self'` 的 CSP

## [0.2.0] - 2026-10-23

Phase 2（生产化与运维基线）：多副本正确性、安全加固与内容治理。

### 新增

- 多副本安全：OIDC（fosite）会话落 Postgres（`oidc_sessions`，JSONB 全量 + 惰性 GC）；Microsoft 绑定 state 落 cache 接口；限流新增 Redis 实现（`internal/redislimit`，Lua 原子固定窗口，fail-open）；goose 迁移 advisory-lock 串行化
- 审计日志：全部管理操作记录 actor（token/user:id）+ 来源 IP，`GET /api/admin/audit`（migration 00008）
- 配额：纹理存储按上传者归属登记（migration 00010 `files` + `users.storage_tier`，sha256 去重、超限回滚登记），`MIRROR_QUOTA_STORAGE_BYTES/TIERS/PUBLISHED`；超限 413
- 内容治理：举报 `POST /reports`（同目标唯一，409）；皮肤审核队列（migration 00011 `skinlib_items.status`，`MIRROR_SKIN_REVIEW=true` 发布进 under_review，公开列表只出 listed）；管理端 `GET /api/admin/reports`、`POST /api/admin/reports/{id}/resolve`、`GET /api/admin/skins`、`POST /api/admin/skins/{id}/status`
- web 会话加固支撑：`BearerToken` 回退读 httpOnly 会话 cookie（`mythic_session`，SameSite=Lax；启动器 Bearer 头优先）；CORS 白名单模式允许凭证
- HTML 邮件（multipart/alternative，i18n 三段式模板）

### 修复

- OIDC consent 后从未 grant scope（id_token/refresh_token 永远发不出）；userinfo `sub` 恒为空（`IntrospectToken` Merge 语义）
- 跨副本注册的 OIDC 客户端不感知（缓存未命中节流重载）
- SMTP 从未送达（go-mail host/port 误用 + 强制 STARTTLS + 吞错）

## [0.1.0] - 2026-10-23

首个可用版本：Phase 1（Mirror 可交付）W1–W4 完成。

### 新增

- Yggdrasil 认证全套端点（authserver/sessionserver/profile 查询）、RSA 签名 profile（1.19+）、authlib-injector 可对接
- 皮肤链路：PNG 校验（含解压炸弹预检）、内容寻址存储（本地/S3）、`/textures` `/skin` `/cape` 分发、CDN 刷新钩子
- 纹理渲染：`/avatars`、`/renders/head|body`、`/usm`、旧版 `MinecraftSkins|Cloaks` 直链
- 账号体系：注册、邮箱验证、找回密码（邮件链接指向前端 `MIRROR_WEB_URL`，文案 i18n）、GitHub OAuth、Microsoft 正版绑定、邀请码、验证码（Turnstile/reCAPTCHA/hCaptcha）
- 皮肤库：发布、详情、搜索（q）、排序（newest/likes）、分页（total）、点赞、评论（读写 + 管理删除）、"应用到我的皮肤"（`POST /skinlib/{id}/apply`）
- 管理后台 API：用户管理、登录方式注册表、公告、邀请码、皮肤/评论审核；支持静态 token 或 role=admin 会话
- OIDC Provider（Fosite）：`/oauth2/*`、`/.well-known/*`（token 存内存，多实例待 P2）
- 安全：可信代理感知的客户端 IP 解析（`shared/clientip`，防伪造 XFF）、按 IP 固定窗口限流（`shared/ratelimit.Memory`，TTL 清扫）、安全响应头 + CSP、`ReadHeaderTimeout`/`IdleTimeout`、CORS 白名单（`MIRROR_CORS_ORIGINS`，回退 `WEB_URL`）
- 工程化：sqlc 类型安全 SQL + CI 漂移检查、OpenAPI 契约漂移门（`TestSpecMatchesRoutes`）、Postgres 集成测试（CI service + 自动迁移）、gofmt 门、govulncheck

### 已知限制

- OIDC token 与 Microsoft 绑定 state 为进程内存储，多副本/重启失效（Phase 2 落库）
- 评论无举报入口；无 2FA、配额、审计日志（Phase 2）
