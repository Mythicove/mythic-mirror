# Changelog

**English** | [简体中文](./CHANGELOG.zh-CN.md)

This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and semantic versioning.

## [Unreleased]

### Added

- **Independent launcher passwords** ([ADR-0012](https://docs.gov.skin/architecture/adr-0012-credential-separation/)): `player_credentials` table (migration 00013), max 3 per player, `GET/POST /account/players/{name}/credentials`, `DELETE /account/players/{name}/credentials/{id}`. Revoking one **immediately** destroys the sessions established through it via foreign-key cascade (no waiting for the 24h expiry); credentials follow the player across renames (`ON UPDATE CASCADE`) and are removed when the player is deleted
- **Third-party bindings persisted**: `user_identities(provider, subject, user_id, email)`. Third-party login **checks the binding first**; only on a miss does it match an existing account by email and write the binding on the spot. After that, provider-side email changes or email takeover no longer change account ownership. `GET /account/identities` returns the binding list and whether the site enforces binding
- **Auth policy** (`GET/PUT /api/admin/auth-policy`, stored as the `auth_policy` settings row): `require_third_party_binding` (unbound accounts cannot use skin upload / skin-library publish & apply / launcher login / SSO; account management, the binding flow, reports and comments stay unrestricted) and `max_players` (per-account player cap, default 1, existing excess not reclaimed). GET includes the read-only counter `password_only_users` (has primary password, no binding, verified, not banned), computed only for admin-panel decisions, never on the login hot path

### Changed (breaking)

- **Credentials for `POST /authserver/authenticate` and `/authserver/signout` changed**: `username` is now the **player name** (no longer the email) and `password` is that player's **independent password** (the primary password is never queried on protocol endpoints). `availableProfiles`/`selectedProfile` in the response contain only the authenticated player. After upgrading an existing deployment, users cannot log in from launchers until they create an independent password on the website (website login unaffected)
- **`POST /account/login` response shape**: changed from Yggdrasil-shaped (with `availableProfiles`/`selectedProfile`) to `{accessToken, clientToken}`. A website session represents no player (`sessions.player_id` is an empty string); which player to use is chosen by the frontend from `GET /account/players`
- **`POST /account/exchange-code`** no longer returns `playerName`/`playerId`
- **"Current player" mechanism removed**: `users.active_player_uuid` column, `POST /account/players/{name}/activate`, the `Player.active` field, and `Store.PlayerByUser`/`SetActivePlayer` all deleted
- `Signout` changed from "verify the password then do nothing" to actually destroying **that player's** game sessions (the protocol text says sign out all tokens of the account, but credentials belong to a single player, so wiping other players' sessions of the same account would be overreach)
- The OIDC ID token `player` claim is only emitted when the account has exactly one player (with multiple players the IdP cannot know which identity the user wants on the RP, and guessing is worse than omitting; consumers treat it only as a last-resort display name)

### Fixed

- The `password` switch in `login_methods` was previously **purely decorative** (only `internal/admin` read it): now actually enforced: when disabled, `/account/login` and `/oauth2/login/accept` reject the primary password, password-based registration stops as well, and the frontend login page no longer renders the password form. Legacy accounts without a third-party binding may still log in and reset with the primary password (grace period); once bound, rejected. When password-only accounts reach zero, primary-password login naturally dies completely, and no second switch is needed
- Banned/deactivated accounts could previously still obtain sessions via `Authenticate` (`GetUserAuth` did not filter `status`): now uniformly rejected; the launcher side additionally returns `ErrAccountDisabled`
- `POST /account/players` returned 500 when exceeding the player cap (error not mapped): now 409 with localized copy

### Security

- `POST /oauth2/login/accept` (the IdP login form, which verifies exactly the primary password) previously had only the site-wide fallback rate limit, and it was the one endpoint where passwords could be tried without a speed limit. It now shares the per-IP `auth` bucket (10/s) with `/account/login` and `/authserver/*`
- Rejections requiring user action return **stable error codes** instead of localized copy (`binding_required`, `password_login_disabled`); the frontend guides based on codes, never on copy matching

### Added (earlier rounds, still unreleased)

- CAPTCHA drivers gain [Cap](https://github.com/tiagozip/cap) (self-hosted PoW CAPTCHA): `MIRROR_CAPTCHA_DRIVER=cap` + `MIRROR_CAPTCHA_CAP_URL` (public URL of the standalone instance) + `MIRROR_CAPTCHA_SITE_KEY`/`MIRROR_CAPTCHA_SECRET`; server-side verification uses Cap's reCAPTCHA-compatible `POST {instance}/{siteKey}/siteverify` (JSON body); `/api/register-requirements` additionally emits `captchaCapUrl` for the frontend widget
- Configurable CAPTCHA scopes: `MIRROR_CAPTCHA_SCOPES` (comma-separated, default `register`, optional `login`; unregistered values warned and dropped at startup); `/api/register-requirements` additionally emits `captchaScopes`. New web-only login endpoint `POST /account/login` (CAPTCHA enforced when the login scope is enabled, sharing the per-IP auth rate-limit bucket with `/authserver/authenticate`); the launcher-facing Yggdrasil `/authserver/authenticate` never enforces CAPTCHA, so game login is unaffected
- Brand icons manageable: new `GET /api/brand`, `GET /api/brand/logo/{light,dark}` (public) and `GET/PUT/DELETE /api/admin/brand[/logo/{variant}]` (admin, audited). Custom PNGs stored in object storage (local/S3) with a version number for cache busting; the frontend falls back to built-in defaults when not customized
- Site info/appearance manageable: new `GET /api/site`, `GET /site/custom.css`, `GET /site/custom.js` (public) and `GET/PUT /api/admin/site` (admin, audited). Overrides site name/description/copyright/filing and custom CSS/JS; custom assets are served same-origin, compatible with a `script-src/style-src 'self'` CSP
- Registration & verification policy moves to the admin panel as the **single source**: new `GET/PUT /api/admin/registration` (settings KV, audited). The legacy env vars `MIRROR_DISABLE_REGISTRATION`/`MIRROR_INVITE_ONLY`/`MIRROR_EMAIL_DOMAIN_WHITELIST`/`MIRROR_CAPTCHA_SCOPES` are seeded into the DB once at upgrade via the new `configseed` (idempotent under the migration advisory lock, never overwrites admin-set values, admin deleting the row = reset to default) and ignored afterwards (startup WARN if still set)

## [0.2.0] - 2026-10-23

Phase 2 (production readiness & ops baseline): multi-replica correctness, security hardening, content governance.

### Added

- Multi-replica safety: OIDC (fosite) sessions persisted to Postgres (`oidc_sessions`, full JSONB + lazy GC); Microsoft binding state persisted via the cache interface; Redis rate-limit implementation (`internal/redislimit`, atomic Lua fixed window, fail-open); goose migrations serialized via advisory lock
- Audit log: all admin operations record actor (token/user:id) + source IP, `GET /api/admin/audit` (migration 00008)
- Quotas: texture storage registered per uploader (migration 00010 `files` + `users.storage_tier`, sha256 dedup, registration rolled back when exceeded), `MIRROR_QUOTA_STORAGE_BYTES/TIERS/PUBLISHED`; 413 when exceeded
- Content governance: reports `POST /reports` (unique per target, 409); skin review queue (migration 00011 `skinlib_items.status`, `MIRROR_SKIN_REVIEW=true` sends publishes to under_review, public listing shows only listed); admin endpoints `GET /api/admin/reports`, `POST /api/admin/reports/{id}/resolve`, `GET /api/admin/skins`, `POST /api/admin/skins/{id}/status`
- Web session hardening support: `BearerToken` falls back to reading the httpOnly session cookie (`mythic_session`, SameSite=Lax; launcher Bearer header takes precedence); CORS allowlist mode permits credentials
- HTML emails (multipart/alternative, i18n three-part templates)

### Fixed

- OIDC consent never granted scopes (id_token/refresh_token could never be issued); userinfo `sub` always empty (`IntrospectToken` Merge semantics)
- OIDC clients registered on another replica were not picked up (cache-miss reload throttled)
- SMTP never delivered (go-mail host/port misuse + forced STARTTLS + swallowed errors)

## [0.1.0] - 2026-10-23

First usable version: Phase 1 (Mirror deliverable) W1 to W4 complete.

### Added

- Full Yggdrasil auth endpoint set (authserver/sessionserver/profile lookup), RSA-signed profiles (1.19+), authlib-injector compatible
- Skin pipeline: PNG validation (incl. decompression-bomb pre-check), content-addressed storage (local/S3), `/textures` `/skin` `/cape` distribution, CDN purge hooks
- Texture rendering: `/avatars`, `/renders/head|body`, `/usm`, legacy `MinecraftSkins|Cloaks` direct links
- Account system: registration, email verification, password recovery (email links point at the frontend `MIRROR_WEB_URL`, i18n copy), GitHub OAuth, Microsoft account linking, invite codes, CAPTCHA (Turnstile/reCAPTCHA/hCaptcha)
- Skin library: publish, detail, search (q), sorting (newest/likes), pagination (total), likes, comments (read+write + admin delete), "apply to my skin" (`POST /skinlib/{id}/apply`)
- Admin API: user management, login-method registry, announcements, invite codes, skin/comment review; static token or role=admin session
- OIDC provider (Fosite): `/oauth2/*`, `/.well-known/*` (tokens in memory; multi-instance pending P2)
- Security: trusted-proxy-aware client IP resolution (`shared/clientip`, spoofed-XFF protection), per-IP fixed-window rate limiting (`shared/ratelimit.Memory`, TTL sweep), security response headers + CSP, `ReadHeaderTimeout`/`IdleTimeout`, CORS allowlist (`MIRROR_CORS_ORIGINS`, falls back to `WEB_URL`)
- Engineering: sqlc type-safe SQL + CI drift check, OpenAPI contract drift gate (`TestSpecMatchesRoutes`), Postgres integration tests (CI service + auto-migrate), gofmt gate, govulncheck

### Known limitations

- OIDC tokens and Microsoft binding state stored in-process; lost across replicas/restarts (persisted in Phase 2)
- No report entry for comments; no 2FA, quotas, audit log (Phase 2)
