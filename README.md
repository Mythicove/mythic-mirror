# Mythic Mirror

面向 Minecraft 离线服/私服的皮肤站后端：Yggdrasil 认证 + 皮肤/披风分发 + 账号体系，Go 单二进制，摆脱 Blessing Skin 的 PHP EOL 风险。

## 状态

后端可用（M1–M3 + M5/M6 部分）：

- **Yggdrasil 全套端点**：authserver / sessionserver / profile 查询，RSA 签名（1.19+），authlib-injector 可对接
- **皮肤链路**：PNG 校验（含解压炸弹预检）、内容寻址存储、`/textures` `/skin` `/cape` 分发、CDN 刷新钩子
- **纹理渲染**：crafatar 风格 `/avatars` `/renders/head|body`、USM、旧版 `MinecraftSkins/Cloaks` 直链
- **账号体系**:注册/邮箱验证/找回密码、GitHub OAuth、Microsoft 正版绑定、邀请码、验证码（Turnstile/reCAPTCHA/hCaptcha）
- **皮肤库**：发布/点赞/评论（评论目前只写不读，列表待做）
- **管理后台**：用户/登录方式/公告/邀请码/皮肤审核（配套 UI 在 `web` 仓库）
- **OIDC Provider**（Fosite）：`/oauth2/*`，token 目前存内存（多实例待接 Postgres/Redis）
- **存储/缓存可切换**：本地磁盘或 S3；进程内缓存或 Redis；SMTP/Resend/日志邮件

未做：2FA、举报、配额、审计日志、评论读取、皮肤标签/搜索。完整差距见 `docs/plans/status.md`。

## 快速开始

```bash
cp .env.example .env          # 按需改
go run ./cmd/server           # 无 DATABASE_URL 时用内存存储（开发）
```

生产（与 hoard/web 一起）在 workspace 根目录 `docker compose up -d --build`，见 `docs/deploy/`。

## 开发

```bash
make test          # go test ./...（pgstore 集成测试需 MIRROR_TEST_DATABASE_URL）
make generate      # sqlc 生成
make check-spec    # OpenAPI 契约漂移门（需同级 checkout 的 api 仓库）
```

## 相关仓库

- 文档/计划：[Mythicove/docs](https://github.com/Mythicove/docs)
- API 契约：[Mythicove/api](https://github.com/Mythicove/api)
- 共享 Go 模块：[Mythicove/shared](https://github.com/Mythicove/shared)
- 前端：[Mythicove/web](https://github.com/Mythicove/web)

## 许可证

[AGPL-3.0](./LICENSE)
