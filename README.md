# Mythic Mirror

**English** | [简体中文](./README.zh-CN.md)

Skin-station backend for Minecraft offline/private servers: Yggdrasil authentication + skin/cape distribution + account system. A single Go binary, free of Blessing Skin's PHP EOL risk.

## Status

Backend usable (M1–M3 + parts of M5/M6):

- **Full Yggdrasil endpoint set**: authserver / sessionserver / profile lookup, RSA signatures (1.19+), authlib-injector compatible
- **Skin pipeline**: PNG validation (incl. decompression-bomb pre-check), content-addressed storage, `/textures` `/skin` `/cape` distribution, CDN purge hooks
- **Texture rendering**: crafatar-style `/avatars` `/renders/head|body`, USM, legacy `MinecraftSkins/Cloaks` direct links
- **Account system**: registration / email verification / password recovery, GitHub OAuth, Microsoft account linking, invite codes, CAPTCHA (Turnstile / reCAPTCHA / hCaptcha / self-hosted Cap; protected scopes configurable via `MIRROR_CAPTCHA_SCOPES`, registration-only by default, web login optional)
- **Skin library**: publish / like / comment (comments are currently write-only; listing pending)
- **Admin console**: users / login methods / announcements / invite codes / skin review (companion UI lives in the `web` repository)
- **OIDC provider** (Fosite): `/oauth2/*`; tokens currently in memory (multi-instance needs Postgres/Redis)
- **Swappable storage/cache**: local disk or S3; in-process cache or Redis; SMTP / Resend / log mailer

Not done: 2FA, reports, quotas, audit log, comment reads, skin tags/search. Full gap list in `docs/plans/status.md`.

## Quick start

```bash
cp .env.example .env          # adjust as needed
go run ./cmd/server           # in-memory store without DATABASE_URL (development)
```

Production (together with hoard/web): `docker compose up -d --build` at the workspace root; see `docs/deploy/`.

## Development

```bash
make test          # go test ./... (pgstore integration tests need MIRROR_TEST_DATABASE_URL)
make generate      # sqlc generation
make check-spec    # OpenAPI contract drift gate (needs the api repository checked out alongside)
```

## Related repositories

- Docs/plans: [Mythicove/docs](https://github.com/Mythicove/docs)
- API contracts: [Mythicove/api](https://github.com/Mythicove/api)
- Shared Go module: [Mythicove/shared](https://github.com/Mythicove/shared)
- Frontend: [Mythicove/web](https://github.com/Mythicove/web)

## License

[Elastic License 2.0 (ELv2)](./LICENSE)

This repository was previously released under AGPL-3.0 (v0.1.0–v0.2.0) and relicensed to ELv2 on 2026-10-26; see [LICENSE.history](./LICENSE.history) for details.
