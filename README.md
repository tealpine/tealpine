# Tealpine

**Tealpine** is an authentication and authorization proxy for [Model Context Protocol (MCP)](https://modelcontextprotocol.io) servers. It sits between MCP clients (such as Claude or other AI agents) and your upstream MCP servers, adding:

- **Authentication** — static bearer tokens or OIDC/OAuth2
- **Group-based authorization** — fine-grained RBAC per method and resource
- **Multi-server aggregation** — expose multiple MCP servers through a single endpoint with prefix namespacing
- **Upstream OIDC** — transparently handles OAuth2 flows for upstream servers that require authentication

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
  - [Server](#server)
  - [Upstream Servers](#upstream-servers)
  - [MCP Endpoints](#mcp-endpoints)
  - [Groups and Users](#groups-and-users)
  - [Authorization Rules](#authorization-rules)
- [Authentication](#authentication)
  - [Static Tokens](#static-tokens)
  - [OIDC / OAuth2](#oidc--oauth2)
- [API Endpoints](#api-endpoints)
- [Building](#building)
- [Development](#development)

---

## Overview

```
MCP Client (Claude, etc.)
        │
        ▼
  ┌───────────┐
  │  Tealpine │  ← authentication + authorization
  └───────────┘
    │       │
    ▼       ▼
upstream1  upstream2  ...  (MCP servers)
```

Tealpine exposes one or more MCP endpoints. Each endpoint proxies either a **single** upstream MCP server or **multiple** upstream servers aggregated under one endpoint (with tool/resource/prompt names prefixed to avoid collisions).

Authorization is enforced at two levels:

1. **Request-level** — rejects unauthorized MCP method calls outright.
2. **Filtering** — strips unauthorized items from `tools/list`, `resources/list`, and `prompts/list` responses so clients only see what they are allowed to use.

---

## Quick Start

**1. Download or build the binary** (see [Building](#building)).

**2. Create a config file** based on `config.example.json`:

```json
{
  "server": { "host": "localhost:8088" },
  "upstream": {
    "my-mcp": {
      "transport": "streamablehttp",
      "url": "http://localhost:7751/mcp"
    }
  },
  "mcps": {
    "my-mcp": {
      "path": "my-mcp",
      "transport": "streamablehttp",
      "upstream": "my-mcp"
    }
  },
  "groups": {
    "users": ["alice"]
  },
  "users": {
    "alice": { "token": "alicetoken" }
  }
}
```

**3. Run:**

```bash
./tealpine --config config.json
```

**4. Connect your MCP client** to `http://localhost:8088/my-mcp` with `Authorization: Bearer alicetoken`.

---

## Configuration

The server is configured via a single JSON file. See `config.example.json` for a full example.

### Server

```json
"server": {
  "host": "localhost:8088",
  "admin": ["admins"],
  "cors_allow_origin": "*",
  "auth_cookie_secure": false,
  "auth": { ... }
}
```

| Field | Description |
|---|---|
| `host` | Address the server listens on (`host:port`) |
| `admin` | List of group names whose members can access the `/tealpine/api/v1/status` endpoint |
| `cors_allow_origin` | Value for `Access-Control-Allow-Origin` header (optional) |
| `auth_cookie_secure` | Set the `Secure` flag on session cookies (set `true` in production with HTTPS) |
| `auth` | OIDC configuration (optional — see [OIDC / OAuth2](#oidc--oauth2)) |

### Upstream Servers

Define the upstream MCP servers tealpine connects to:

```json
"upstream": {
  "calculator": {
    "transport": "streamablehttp",
    "url": "http://localhost:7751/mcp",
    "bearer": "secret-token",
    "pingInterval": "30s",
    "reconnectDelay": "5s"
  },
  "hello": {
    "transport": "stdio",
    "cmd": "npx",
    "args": ["-y", "@my/mcp-server"]
  }
}
```

| Field | Description |
|---|---|
| `transport` | `streamablehttp` or `stdio` |
| `url` | HTTP endpoint (required for `streamablehttp`) |
| `bearer` | Bearer token to authenticate with the upstream (optional) |
| `cmd` / `args` | Command to launch (required for `stdio`) |
| `pingInterval` | How often to ping the upstream to detect disconnection (default: `30s`) |
| `reconnectDelay` | How long to wait before reconnecting after a failure (default: `5s`) |
| `auth` | Upstream OIDC override (optional — see [Upstream OIDC](#upstream-oidc)) |

Tealpine automatically reconnects to upstream servers on failure and tracks connection status.

### MCP Endpoints

Define what Tealpine exposes to clients:

**Single upstream:**

```json
"mcps": {
  "calc": {
    "path": "calc",
    "transport": "streamablehttp",
    "upstream": "calculator"
  }
}
```

Clients connect to `http://localhost:8088/calc`.

**Multiple upstreams aggregated (multi-proxy):**

```json
"mcps": {
  "all": {
    "path": "all",
    "transport": "streamablehttp",
    "upstreams": [
      { "name": "calculator", "prefix": "calc" },
      { "name": "hello",      "prefix": "hello" }
    ]
  }
}
```

Tools from `calculator` are exposed as `calc_<toolname>`, tools from `hello` as `hello_<toolname>`. Clients see a single unified MCP endpoint at `http://localhost:8088/all`.

> Either `upstream` or `upstreams` must be set — not both.

### Groups and Users

Users are defined with a static bearer token. Groups assign users to named sets used in authorization rules.

```json
"groups": {
  "admins":    ["alice"],
  "engineers": ["alice", "bob"],
  "readonly":  ["carol"]
},
"users": {
  "alice": { "token": "alicetoken" },
  "bob":   { "token": "bobtoken" },
  "carol": { "token": "caroltoken" }
}
```

> The `users` section is only required when using static token authentication. With OIDC, users and group memberships can be sourced from the identity provider (see [OIDC / OAuth2](#oidc--oauth2)).

### Authorization Rules

Per-endpoint authorization is configured in the `auth` field of each MCP entry. It is a map from **group name** to a list of rules:

```json
"mcps": {
  "all": {
    "path": "all",
    "transport": "streamablehttp",
    "upstreams": [...],
    "auth": {
      "engineers": [
        { "method": "tools/list", "allow": ["*"] },
        { "method": "tools/call", "allow": ["calc_*", "hello_greet"] }
      ],
      "readonly": [
        { "method": "tools/list", "allow": ["calc_*"] }
      ]
    }
  }
}
```

| Field | Description |
|---|---|
| `method` | MCP method to authorize: `tools/list`, `tools/call`, `resources/list`, `resources/read`, `prompts/list`, `prompts/get` |
| `allow` | List of allowed resource/tool names. Supports prefix wildcards (`calc_*`) and `*` for all |

**Behavior:**
- Users with **no matching group rule** are denied entirely.
- `tools/list` (and equivalent) responses are **filtered** — users only see items they are allowed to call.
- `tools/call` with a tool the user is not allowed to call is **rejected**.

---

## Authentication

### Static Tokens

Set a token per user in the `users` section. Clients authenticate with:

```
Authorization: Bearer <token>
```

### OIDC / OAuth2

Enable OIDC by adding an `auth` block to the server config:

```json
"server": {
  "auth": {
    "type": "oidc",
    "issuer_url": "https://your-idp.example.com",
    "client_id": "your-client-id",
    "client_secret": "${OIDC_CLIENT_SECRET}",
    "redirect_url": "http://localhost:8088/auth/callback"
  }
}
```

> `${ENV_VAR}` syntax is supported for secrets — the value is read from the environment at startup.

| Field | Description |
|---|---|
| `type` | Must be `oidc` |
| `issuer_url` | OIDC provider issuer URL |
| `client_id` | OAuth2 client ID |
| `client_secret` | OAuth2 client secret (supports `${ENV_VAR}`) |
| `redirect_url` | Callback URL registered with your IdP |
| `user_claim_field` | JWT claim to use as username (default: `email`) |
| `group_claim_field` | JWT claim to use as group list (default: `groups`) |
| `require_user_in_config` | If `true`, OIDC users must also exist in the `users` section (default: `false`) |

With OIDC enabled:
- Browser-based clients are redirected to the IdP via `/auth/login`.
- API clients can authenticate with a valid JWT access token or the session cookie obtained after login.
- Groups are extracted from the JWT and matched against authorization rules.

#### Upstream OIDC

If an upstream MCP server requires OAuth2 authentication (responds with `401` and `/.well-known/oauth-protected-resource` metadata), Tealpine will:

1. Detect the upstream's authorization server.
2. Register itself as an OAuth2 client with that server.
3. Provide a login URL at `/upstream/{name}/auth/login` for users to authorize.
4. Persist and reuse access tokens across restarts (stored in `tokens.json`).

---

## API Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET/POST` | `/auth/login` | Public | Initiate OIDC login |
| `GET` | `/auth/callback` | Public | OIDC authorization code callback |
| `GET` | `/auth/logout` | Public | Clear session |
| `GET` | `/upstream/{name}/auth/login` | Bearer/Session | Initiate upstream OIDC login |
| `GET` | `/upstream/{name}/auth/callback` | Public | Upstream OIDC callback |
| `GET` | `/.well-known/oauth-protected-resource` | Public | RFC 9728 resource metadata |
| `GET` | `/.well-known/openid-configuration` | Public | OpenID Connect discovery |
| `GET` | `/tealpine/api/v1/status` | Admin | Upstream connection status |
| `*` | `/{path}` | Bearer/Session | MCP proxy endpoint |

**Status response example:**

```json
{
  "clients": [
    { "name": "calculator", "status": "connected" },
    { "name": "hello",      "status": "disconnected" }
  ]
}
```

Possible status values: `connected`, `disconnected`, `login_required` (upstream needs OAuth2 authorization).

---

## Building

**Requirements:** Go 1.24+

```bash
# Build binary
make build

# Build and run (uses config.json in current directory)
make run

# Run tests
make test
```

The binary is built with `CGO_ENABLED=0` and embeds the git commit hash and build timestamp.

**Command-line flags:**

```
./tealpine --config <path>   Path to config JSON file (default: config.json)
           --tokens <path>   Path to tokens file for upstream OIDC (default: tokens.json)
```

---

## Development

```bash
# Start test MCP servers (calculator + temperature) in the background
make start-mcps

# Stop test MCP servers
make stop-mcps

# Launch a tmux session with 4 panes (2 MCP servers, 1 proxy, 1 shell)
make tmux

# Format code and tidy modules
make format
```

Test MCP servers (calculator, temperature, hello) are available under `test/mcp_servers/` and are used in integration tests.

---

## License

MIT
