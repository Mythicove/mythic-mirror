# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与语义化版本。

## [Unreleased]

### 新增

- 验证码 driver 新增 [Cap](https://github.com/tiagozip/cap)（自托管 PoW 验证码）：`MIRROR_CAPTCHA_DRIVER=cap` + `MIRROR_CAPTCHA_CAP_URL`（Standalone 实例公网地址）+ `MIRROR_CAPTCHA_SITE_KEY`/`MIRROR_CAPTCHA_SECRET`；服务端校验走 Cap 的 reCAPTCHA 兼容 `POST {instance}/{siteKey}/siteverify`（JSON 体）；`/api/register-requirements` 增发 `captchaCapUrl` 供前端加载控件

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
