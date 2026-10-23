# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与语义化版本。

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
