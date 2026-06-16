# Connecting via a Third-Party OAuth / OIDC Provider

The Tribal Knowledge web app can delegate user sign-in to any standards-compliant
**OpenID Connect (OIDC)** identity provider — for example **Zitadel**, **Okta**,
Auth0, Keycloak, Microsoft Entra ID, or Google Workspace. Users sign in at the
provider, and the app provisions (or links) a local user record on first login.

This guide covers the end-to-end setup. It assumes the server is already built and
running (see [`mcp-config.md`](./mcp-config.md) for build instructions).

---

## How it works

The app implements the standard **OIDC Authorization Code flow**:

1. The user hits `GET /auth/oidc/login`.
2. The server sets a short-lived, signed `oidc_state` CSRF cookie and redirects the
   browser to the provider's authorization endpoint.
3. The user authenticates at the provider (and consents, if prompted).
4. The provider redirects back to `GET /auth/oidc/callback?code=…&state=…`.
5. The server validates the `state` against the cookie, exchanges the `code` for an
   ID token, and **verifies the token signature** via the provider's discovery
   document.
6. The `email`, `name`, and `sub` (subject) claims are read. The user is looked up by
   `sub` (`ExternalID`), then by `email`, and **upserted**. New users are auto-assigned
   to a team by email domain (see [Team assignment](#team-assignment)).
7. A 24-hour `session` cookie is issued and the browser is redirected to `/`.

Relevant scopes requested: `openid profile email`. The provider **must** return
`email` and `name` claims in the ID token.

### What lives where

| Setting | Stored in | Configured via |
|---|---|---|
| Provider mode (`local` / `oidc`) | Database | Admin UI → **Auth Configuration**, or `PUT /api/admin/auth-config` |
| OIDC Issuer URL | Database | Admin UI / API |
| OIDC Client ID | Database | Admin UI / API |
| Redirect URL | Database | Admin UI / API |
| **OIDC Client Secret** | **Environment only** (never the DB) | `OIDC_CLIENT_SECRET` env var |

> The client secret is intentionally kept out of the database and the API surface. It
> is read from the `OIDC_CLIENT_SECRET` environment variable at startup. Changing it
> requires a server restart.

---

## Step 1 — Set the client secret in the environment

Add the secret your provider issues to the server's environment (e.g. `.env`, the
container env, or your secrets manager):

```bash
OIDC_CLIENT_SECRET=<the-client-secret-from-your-provider>
```

Restart the server after setting it.

---

## Step 2 — Register the application at your provider

You need three values back from the provider: an **Issuer URL**, a **Client ID**, and
a **Client Secret**. Register a **Web / server-side** application (confidential client,
Authorization Code grant) and set the redirect URI to:

```
https://<your-host>/auth/oidc/callback
```

For local development this is typically `http://localhost:8080/auth/oidc/callback`.

> Use HTTPS in production. The `session` and `oidc_state` cookies are issued with the
> `Secure` flag, so sign-in will not work over plain HTTP except on `localhost`.

### Zitadel

1. In the Zitadel Console, open your **Project** → **Applications** → **New**.
2. Choose application type **Web**, then authentication method **Code** (PKCE optional;
   the app uses the confidential client + secret flow).
3. **Redirect URIs:** `https://<your-host>/auth/oidc/callback`
4. **Post-logout redirect URIs:** `https://<your-host>/` (optional).
5. Create the app, then copy the **Client ID** and **Client Secret**.
6. **Issuer URL:** your Zitadel instance base URL, e.g.
   `https://<your-instance>.zitadel.cloud` (self-hosted: your instance domain).
   You can confirm it resolves discovery at
   `https://<your-instance>.zitadel.cloud/.well-known/openid-configuration`.
7. Ensure the **email** and **profile** scopes are granted so the `email`/`name`
   claims are present. In Zitadel, verify the user has a verified email.

### Okta

1. In the Okta Admin Console, go to **Applications → Applications → Create App
   Integration**.
2. Sign-in method: **OIDC - OpenID Connect**. Application type: **Web Application**.
3. **Grant type:** Authorization Code.
4. **Sign-in redirect URIs:** `https://<your-host>/auth/oidc/callback`
5. **Sign-out redirect URIs:** `https://<your-host>/` (optional).
6. Assign the app to the appropriate users/groups.
7. Save, then copy the **Client ID** and **Client Secret** from the app's **General**
   tab.
8. **Issuer URL:** your Okta org or custom authorization server URL, e.g.
   `https://<your-org>.okta.com` or
   `https://<your-org>.okta.com/oauth2/<auth-server-id>`. Confirm discovery at
   `<issuer>/.well-known/openid-configuration`.

> **Issuer must match exactly.** The value you enter as the Issuer URL must be byte-for-byte
> the `issuer` returned by the provider's discovery document — no trailing slash
> mismatch. Token verification fails otherwise.

---

## Step 3 — Configure the app

Sign in as a **superadmin** (the bootstrap key set via `SUPERADMIN_KEY`), then either
use the UI or the API.

### Option A — Admin UI

1. Open the web app → **Auth Configuration** page.
2. Set **Provider** to **OIDC**.
3. Fill in:
   - **OIDC Issuer URL** — e.g. `https://<your-instance>.zitadel.cloud` or
     `https://<your-org>.okta.com`
   - **OIDC Client ID** — from your provider
   - **Redirect URL** — `https://<your-host>/auth/oidc/callback`
4. Click **Save**.

### Option B — REST API

```bash
curl -X PUT https://<your-host>/api/admin/auth-config \
  -H "Authorization: Bearer <superadmin-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "oidc",
    "oidc_issuer": "https://<your-instance>.zitadel.cloud",
    "oidc_client_id": "<client-id>",
    "oidc_redirect_url": "https://<your-host>/auth/oidc/callback"
  }'
```

Read the current config back with `GET /api/admin/auth-config`.

> The client **secret** is *not* part of this payload — it comes only from
> `OIDC_CLIENT_SECRET` in the environment (Step 1).

---

## Step 4 — Sign in

Once OIDC is configured, the app's **Sign in** page automatically shows a **Sign in
with SSO** button as the primary action. Clicking it kicks off the redirect to your
provider; after a successful login the user lands back on `/` with an active 24-hour
session. There is no sign-up step — corporate users are provisioned automatically on
first login.

The superadmin can still sign in with the bootstrap key via the **Administrator
sign-in** link on the same page (see [Superadmin](#superadmin-access)).

> The SSO button can also be reached directly at `https://<your-host>/auth/oidc/login`.

---

## Team assignment (whitelists)

Each team has a **whitelist** — its list of `domain_patterns` (editable in **Admin →
Teams**). A pattern is a regular expression matched against the user's email address,
so it can match a whole domain or specific people:

| Pattern | Matches |
|---|---|
| `@acme\.com$` | everyone at `acme.com` |
| `@(acme\|globex)\.com$` | two domains |
| `^alice@acme\.com$` | one specific person |

On each OIDC login the app provisions the user and assigns them to the **first enabled
team whose whitelist matches** their email. For example `alice@acme.com` lands in the
team whose patterns include `@acme\.com$`.

- New users default to the `member` role.
- If **no** team whitelist matches, the user is placed in the reserved **Unassigned**
  team (seeded automatically at startup). An admin can move them from **Admin → Teams /
  Users** at any time.
- Users still parked in **Unassigned** are re-evaluated on every login, so adding a
  matching whitelist pattern later moves them into the right team on their next sign-in.
- An admin's **manual** team assignment is never overridden by the whitelist.
- Existing users (matched by OIDC subject or email) keep their current role.

Configure each team's whitelist before rolling OIDC out, so members land in the right
place automatically.

---

## Superadmin access

The single privileged bootstrap account does **not** go through the identity provider.
It signs in with the `SUPERADMIN_KEY` (set in the server environment) via the
**Administrator sign-in** link on the login page — paste the key and continue. Use this
account to perform the initial OIDC configuration and to manage teams and whitelists.

---

## Switching back to local auth

Set **Provider** back to **Local (email + password)** in the Auth Configuration page
(or `"provider": "local"` via the API). The `/auth/oidc/*` endpoints return
`400 OIDC not configured` whenever the provider is not set to `oidc`, and `/auth/login`
(email + password) returns `400 local auth not enabled` whenever it is.

---

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `400 OIDC not configured` | Provider is still set to `local`. Switch it to `oidc` and save. |
| `500 OIDC provider init failed` | Issuer URL is wrong/unreachable, or discovery (`/.well-known/openid-configuration`) failed. Verify the issuer resolves and matches exactly. |
| `400 invalid oauth state` | The `oidc_state` cookie was lost or didn't round-trip. Usually cookies blocked, mismatched host, or a stale/expired (>5 min) login attempt. Retry from `/auth/oidc/login`. |
| `401 OIDC exchange failed` | Wrong `OIDC_CLIENT_SECRET`, mismatched redirect URI, or the provider rejected the code. Confirm the secret and that the registered redirect URI exactly equals your configured Redirect URL. |
| Login works but user has no team | No email-domain mapping for the user's domain. Add a mapping or assign the team manually. |
| Sign-in fails over HTTP | Cookies are `Secure`; serve the app over HTTPS (or use `localhost` for local dev). |
| Empty name/email on the new user | Provider isn't releasing `profile`/`email` claims. Grant those scopes and ensure the user has a verified email. |

---

## Security notes

- The authorization-code `state` is validated against a SHA-256-hashed, `HttpOnly`,
  `Secure` cookie scoped to `/auth/oidc/callback`, mitigating CSRF on the callback.
- ID tokens are signature-verified against the provider's published keys and checked
  for the expected audience (client ID).
- Sessions are random 256-bit tokens; only their SHA-256 hash is stored server-side.
  The cookie is `HttpOnly`, `Secure`, `SameSite=Lax`, and expires after 24 hours.
- Rotate `OIDC_CLIENT_SECRET` by updating the env var and restarting the server.
