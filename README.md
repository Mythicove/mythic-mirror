# Mythic Mirror

**English** | [简体中文](./README.zh-CN.md)

A skin-station backend for Minecraft offline and private servers: Yggdrasil authentication, skin and cape delivery, and an account system. It ships as a single Go binary and avoids the PHP EOL risk of Blessing Skin.

## Status

The backend is usable (M1 to M3, plus parts of M5 and M6):

- Full Yggdrasil endpoint set: authserver, sessionserver and profile lookup, with RSA signatures (1.19 and later) and authlib-injector support.
- Skin pipeline: PNG validation (including a decompression-bomb pre-check), content-addressed storage, /textures, /skin and /cape delivery, CDN purge hooks.
- Texture rendering: crafatar-style /avatars and /renders/head, /renders/body, USM sharpening, and direct links for legacy MinecraftSkins and Cloaks.
- Account system: registration, email verification, password recovery, GitHub OAuth, Microsoft account linking, invite codes and CAPTCHA (Turnstile, reCAPTCHA, hCaptcha or self-hosted Cap; protected scopes are configured with MIRROR_CAPTCHA_SCOPES, registration only by default, web login optional).
- Skin library: publish, like and comment. Comments are write-only for now; the listing view is pending.
- Admin console: users, login methods, announcements, invite codes and skin review. The companion UI lives in the web repository.
- OIDC provider (Fosite): the /oauth2/* endpoints. Tokens are in memory for now; multi-instance needs Postgres or Redis.
- Swappable storage and cache: local disk or S3, in-process cache or Redis, mail over SMTP, Resend or logs.

Not done: 2FA, reports, quotas, audit log, comment reads, skin tags and search. The full gap list is in docs/plans/status.md.

## Quick start

```bash
cp .env.example .env          # adjust as needed
go run ./cmd/server           # in-memory store when DATABASE_URL is unset (development)
```

For production (together with hoard and web), run docker compose up -d --build at the workspace root; see docs/deploy/.

## Development

```bash
make test          # go test ./...; pgstore integration tests need MIRROR_TEST_DATABASE_URL
make generate      # sqlc generation
make check-spec    # OpenAPI contract drift gate; needs the api repo checked out alongside
```

## Related repositories

- Docs and plans: [Mythicove/docs](https://github.com/Mythicove/docs)
- API contracts: [Mythicove/api](https://github.com/Mythicove/api)
- Shared Go module: [Mythicove/shared](https://github.com/Mythicove/shared)
- Frontend: [Mythicove/web](https://github.com/Mythicove/web)

## License

[Elastic License 2.0 (ELv2)](./LICENSE) — source-available, not OSI open source. The licensing philosophy and intended-use FAQ live on the docs site: [English](https://docs.gov.skin/en/community/licensing/) · [中文](https://docs.gov.skin/community/licensing/).

Self-hosting, modifying and redistributing are all free, including for a monetized community: selling skins, capes or ranks, or taking donations on top of the software, is fine. ELv2 only restricts offering this software itself to third parties as a hosted/managed service, circumventing license keys, and removing notices.

The repository was released under AGPL-3.0 (v0.1.0, v0.2.0) and moved to ELv2 on 2026-10-26. See [LICENSE.history](./LICENSE.history) for the history and what it means.
