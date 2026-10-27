# Mythic Mirror

[English](./README.md) | **简体中文**

面向 Minecraft 离线服和私服的皮肤站后端：Yggdrasil 认证、皮肤和披风分发、账号体系。Go 单二进制部署，避开 Blessing Skin 的 PHP EOL 风险。

## 状态

后端可用（M1 至 M3，以及 M5、M6 的部分）：

- Yggdrasil 全套端点：authserver、sessionserver、profile 查询；RSA 签名（1.19 及以上），可对接 authlib-injector。
- 皮肤链路：PNG 校验（含解压炸弹预检）、内容寻址存储、/textures、/skin、/cape 分发、CDN 刷新钩子。
- 纹理渲染：crafatar 风格的 /avatars 和 /renders/head、/renders/body，USM 锐化，旧版 MinecraftSkins、Cloaks 直链。
- 账号体系：注册、邮箱验证、找回密码、GitHub OAuth、Microsoft 正版绑定、邀请码、验证码（Turnstile、reCAPTCHA、hCaptcha、Cap 自托管；保护范围用 MIRROR_CAPTCHA_SCOPES 配置，默认仅注册，可选 web 登录）。
- 皮肤库：发布、点赞、评论。评论目前只写不读，列表待做。
- 管理后台：用户、登录方式、公告、邀请码、皮肤审核。配套 UI 在 web 仓库。
- OIDC Provider（Fosite）：/oauth2/* 端点。token 目前存内存，多实例待接 Postgres 或 Redis。
- 存储和缓存可切换：本地磁盘或 S3，进程内缓存或 Redis，邮件走 SMTP、Resend 或日志。

未做：2FA、举报、配额、审计日志、评论读取、皮肤标签和搜索。完整差距见 docs/plans/status.md。

## 快速开始

```bash
cp .env.example .env          # 按需修改
go run ./cmd/server           # 不设 DATABASE_URL 时用内存存储（开发）
```

生产环境（与 hoard、web 一起）在 workspace 根目录执行 docker compose up -d --build，见 docs/deploy/。

## 开发

```bash
make test          # go test ./...；pgstore 集成测试需要 MIRROR_TEST_DATABASE_URL
make generate      # sqlc 生成
make check-spec    # OpenAPI 契约漂移门，需要同级 checkout 的 api 仓库
```

## 相关仓库

- 文档和计划：[Mythicove/docs](https://github.com/Mythicove/docs)
- API 契约：[Mythicove/api](https://github.com/Mythicove/api)
- 共享 Go 模块：[Mythicove/shared](https://github.com/Mythicove/shared)
- 前端：[Mythicove/web](https://github.com/Mythicove/web)

## 许可证

[Elastic License 2.0 (ELv2)](./LICENSE) —— 源码可得，非 OSI 开源。许可理念与预期用法 FAQ 见文档站：[中文](https://docs.gov.skin/community/licensing/) · [English](https://docs.gov.skin/en/community/licensing/)。

自托管、修改、再分发全部免费，包括盈利性社区：在软件之上出售皮肤、披风、等级或接受赞助均可。ELv2 只限制三件事：把软件本身作为托管/管理服务提供给第三方、绕过 license key、移除版权声明。

本仓库曾以 AGPL-3.0 发布（v0.1.0、v0.2.0），2026-10-26 起换证为 ELv2。历史与影响见 [LICENSE.history](./LICENSE.history)。
