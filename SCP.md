# Integrating a New Service via Synadia Control Plane

This guide walks you through adding a new microservice to an **existing** NATS
cluster managed by Synadia Control Plane (SCP). It replaces the nsc-based
workflow in INTEGRATION.md with the SCP REST API.

SCP handles server configuration and JWT distribution automatically — you
create users, set permissions, download credentials, and you're done.

---

## 1. What You're Working With

NATS uses a three-level identity hierarchy:

```
System (managed by SCP)             ← the NATS cluster
└── Account                         ← your team's namespace
    ├── User: publisher             ← one per service
    ├── User: processor
    └── User: your-new-service      ← what you're adding
```

In the nsc world, you manage operator keys, push account JWTs, and generate
server configs locally. **With SCP, all of that is handled for you.** The only
artifact your service needs is a `.creds` file, which you download from SCP.

### What SCP replaces

| nsc step | SCP equivalent |
|----------|---------------|
| `nsc init` (create operator) | Already done — your system exists in SCP |
| `nsc add account` | Already done — your account exists in SCP |
| `nsc add user` | `POST /core/beta/accounts/{accountId}/nats-users` |
| `nsc edit user` (permissions) | `PATCH /core/beta/nats-users/{userId}` |
| `nsc generate creds` | `POST /core/beta/nats-users/{userId}/creds` |
| `nsc generate config` | Not needed — SCP manages server config |
| `nsc push` | Not needed — SCP pushes automatically |

### Why `.creds` files matter

A `.creds` file bundles a user JWT and private nkey. Your Go service passes it
via `nats.UserCredentials(path)` and the NATS client handles authentication
automatically. This part is identical whether you use nsc or SCP — the
difference is only in how you create the user and get the `.creds` file.

---

## 2. Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.23+ | https://go.dev/dl |

You do **not** need `nsc` or `nats-server` installed. All user and permission
management is done through the SCP API.

You will need:
- Your SCP **API base URL** (e.g. `https://cloud.synadia.com/api`)
- A **bearer token** for API authentication
- Your **system ID**, **account ID**, and **signing key group ID**

Get these from your team lead or the SCP dashboard.

---

## 3. Getting Your SCP Credentials

Log in to the Synadia Control Plane dashboard and navigate to your team. You
need three pieces of information:

1. **API base URL** — shown in the dashboard settings, typically
   `https://cloud.synadia.com/api` (or your self-hosted URL + `/api`)
2. **Bearer token** — generate from Team → Service Accounts or your personal
   API token
3. **IDs** — find your system ID, account ID, and signing key group ID in the
   dashboard. Each is a UUID string.

Verify your access works:

```bash
curl -s -H "Authorization: Bearer <your-token>" \
  <base-url>/core/beta/systems/<system-id> | jq .name
```

---

## 4. Adding a New User for Your Service

### Using the scp CLI

Build or run the `scp` tool from this repo:

```bash
# Create the user
go run ./cmd/scp create-user \
    --base-url <url> --token <token> \
    --account <account-id> \
    --name <your-service> \
    --sk-group <sk-group-id>
```

Note the user ID in the output — you'll need it for the next steps.

### Set subject permissions

```bash
go run ./cmd/scp update-permissions \
    --base-url <url> --token <token> \
    --user <user-id> \
    --allow-sub "msg.enhanced,_INBOX.>" \
    --allow-pub "msg.final,_INBOX.>"
```

### Download the credentials file

```bash
go run ./cmd/scp download-creds \
    --base-url <url> --token <token> \
    --user <user-id> \
    --output creds/<your-service>.creds
```

### Using the API directly

The `scp` CLI calls the same API you can call from any HTTP client:

```bash
# Create user
curl -X POST "<base-url>/core/beta/accounts/<account-id>/nats-users" \
    -H "Authorization: Bearer <token>" \
    -H "Content-Type: application/json" \
    -d '{"name": "<your-service>", "sk_group_id": "<sk-group-id>"}'

# Set permissions (use the user ID from the response above)
curl -X PATCH "<base-url>/core/beta/nats-users/<user-id>" \
    -H "Authorization: Bearer <token>" \
    -H "Content-Type: application/json" \
    -d '{
        "jwt_settings": {
            "pub": {"allow": ["msg.final", "_INBOX.>"]},
            "sub": {"allow": ["msg.enhanced", "_INBOX.>"]}
        }
    }'

# Download creds
curl -X POST "<base-url>/core/beta/nats-users/<user-id>/creds" \
    -H "Authorization: Bearer <token>" \
    -o creds/<your-service>.creds
```

---

## 5. Permissions Cheat Sheet

| Pattern | Use when | SCP JSON | Equivalent nsc flags |
|---------|----------|----------|---------------------|
| **Publish only** | Produces messages | `pub.allow: ["msg.raw","_INBOX.>"]` `sub.allow: ["_INBOX.>"]` | `--allow-pub msg.raw,_INBOX.> --allow-sub _INBOX.>` |
| **Subscribe only** | Consumes messages | `sub.allow: ["msg.final","_INBOX.>"]` `pub.allow: ["_INBOX.>"]` | `--allow-sub msg.final,_INBOX.> --allow-pub _INBOX.>` |
| **Processor** | Reads + writes | `sub.allow: ["msg.enhanced","_INBOX.>"]` `pub.allow: ["msg.final","_INBOX.>"]` | `--allow-sub msg.enhanced,_INBOX.> --allow-pub msg.final,_INBOX.>` |
| **Wildcard sub** | Monitors all | `sub.allow: ["msg.>","_INBOX.>"]` `pub.allow: ["_INBOX.>"]` | `--allow-sub msg.>,_INBOX.> --allow-pub _INBOX.>` |

The `_INBOX.>` entries are required — the NATS client uses request-reply
internally during connection setup.

---

## 6. Wiring Your Go Service

Your service code is **identical** whether you used nsc or SCP to generate
credentials. The service doesn't know or care — it just loads the `.creds`
file.

### Accept a `--creds` flag

```go
root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file")
```

### Pass it to `natsclient.New()`

```go
client, err := natsclient.New(natsclient.Config{
    ServerURL: flagServer,
    CredsFile: flagCreds,
    ConnName:  "your-service",
})
```

### Under the hood

`natsclient.New()` calls `nats.UserCredentials(cfg.CredsFile)`, which reads
the `.creds` file and handles the JWT+nkey authentication handshake:

```go
// internal/natsclient/client.go
if cfg.CredsFile != "" {
    opts = append(opts, nats.UserCredentials(cfg.CredsFile))
}
conn, err := nats.Connect(cfg.ServerURL, opts...)
```

---

## 7. Worked Example: postprocessor via SCP

Let's add the `postprocessor` service using SCP. It reads from `msg.enhanced`,
adds metadata, and publishes to `msg.final`.

### 7a. Create the user

```bash
go run ./cmd/scp create-user \
    --base-url $SCP_BASE_URL --token $SCP_TOKEN \
    --account $SCP_ACCOUNT_ID \
    --name postprocessor \
    --sk-group $SCP_SK_GROUP_ID
# Output: Created user "postprocessor" (id: <user-id>)
```

### 7b. Set permissions

```bash
go run ./cmd/scp update-permissions \
    --base-url $SCP_BASE_URL --token $SCP_TOKEN \
    --user <user-id> \
    --allow-sub "msg.enhanced,_INBOX.>" \
    --allow-pub "msg.final,_INBOX.>"
```

### 7c. Download credentials

```bash
go run ./cmd/scp download-creds \
    --base-url $SCP_BASE_URL --token $SCP_TOKEN \
    --user <user-id> \
    --output creds/postprocessor.creds
```

### 7d. Run it

```bash
go run ./cmd/postprocessor --creds creds/postprocessor.creds
```

No `nsc push` needed — SCP pushed the updated account JWT automatically when
you created the user and set permissions.

---

## 8. Using the Setup Command

For the complete pipeline, the `setup` command creates all 4 users in one shot:

```bash
go run ./cmd/scp setup \
    --base-url $SCP_BASE_URL --token $SCP_TOKEN \
    --system $SCP_SYSTEM_ID \
    --account $SCP_ACCOUNT_ID \
    --sk-group $SCP_SK_GROUP_ID
```

This:
1. Creates `publisher`, `subscriber`, `processor`, and `postprocessor` users
   (skips any that already exist)
2. Sets the correct subject permissions for each
3. Downloads `.creds` files to `creds/`

It replaces `make setup-nsc && make creds` entirely.

Or using the Makefile shortcut:

```bash
make setup-scp
```

(Requires `SCP_BASE_URL`, `SCP_TOKEN`, `SCP_SYSTEM_ID`, `SCP_ACCOUNT_ID`, and
`SCP_SK_GROUP_ID` environment variables.)

---

## 9. Programmatic Usage

For CI/CD pipelines or automation, use the Go library directly:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "msg/infrastructure"
)

func main() {
    client := infrastructure.NewClient(
        os.Getenv("SCP_BASE_URL"),
        os.Getenv("SCP_TOKEN"),
    )
    ctx := context.Background()

    // Create a user
    user, err := client.CreateUser(ctx, "<account-id>", infrastructure.CreateUserRequest{
        Name:      "my-service",
        SKGroupID: "<sk-group-id>",
    })
    if err != nil {
        panic(err)
    }

    // Set permissions
    err = client.UpdateUserPermissions(ctx, user.ID, infrastructure.Permissions{
        Pub: &infrastructure.Permission{
            Allow: []string{"msg.output", "_INBOX.>"},
        },
        Sub: &infrastructure.Permission{
            Allow: []string{"msg.input", "_INBOX.>"},
        },
    })
    if err != nil {
        panic(err)
    }

    // Download creds
    creds, err := client.DownloadCreds(ctx, user.ID)
    if err != nil {
        panic(err)
    }
    os.WriteFile("my-service.creds", []byte(creds), 0o600)
    fmt.Println("Done:", user.ID)
}
```

---

## 10. Testing Locally

You can test the full pipeline locally. Use either `make setup-nsc` (nsc-based)
or `make setup-scp` (SCP-based) to create users and credentials — the rest of
the workflow is the same.

### With SCP (requires API access)

```bash
make setup-scp    # creates users + downloads creds via SCP API
```

### Then run the pipeline

Open 5 terminals:

```
Terminal 1:  make run-server
Terminal 2:  make push-accounts && make run-subscriber
Terminal 3:  make run-processor
Terminal 4:  make run-postprocessor
Terminal 5:  make run-publisher
```

The subscriber (listening on `msg.final`) should print messages with both
processor and postprocessor metadata:

```json
{
  "id": "a1b2c3d4e5f6a7b8",
  "text": "hello nats world",
  "published_at": "2026-03-24T10:00:00Z",
  "metadata": {
    "word_count": "3",
    "processed_at": "2026-03-24T10:00:00Z",
    "processor_version": "1.0.0",
    "char_count": "16",
    "postprocessed_at": "2026-03-24T10:00:00Z",
    "postprocessor_version": "1.0.0"
  }
}
```

---

## 11. SCP vs NSC Reference

| Task | nsc CLI | scp CLI | SCP API |
|------|---------|---------|---------|
| Create user | `nsc add user --name X --account Y` | `scp create-user --account Y --name X --sk-group Z` | `POST /accounts/{id}/nats-users` |
| Set permissions | `nsc edit user --name X --allow-pub ...` | `scp update-permissions --user ID --allow-pub ...` | `PATCH /nats-users/{id}` |
| Generate creds | `nsc generate creds --name X` | `scp download-creds --user ID -o file` | `POST /nats-users/{id}/creds` |
| Push to server | `nsc push --all` | Not needed | Not needed |
| Generate server config | `nsc generate config --nats-resolver` | Not needed | Not needed |
| List users | `nsc list keys` | `scp list-users --account ID` | `GET /accounts/{id}/nats-users` |
| Describe user | `nsc describe user X` | `scp describe-user --user ID` | `GET /nats-users/{id}` |
| Full setup | `make setup-nsc && make creds` | `scp setup --system ... --account ... --sk-group ...` | Compose API calls |

Key differences:
- SCP uses **IDs** (UUIDs) instead of names for accounts and users in API calls
- SCP **automatically pushes** account changes to the cluster — no manual push step
- SCP **manages server config** — no `nsc generate config` needed
- Permissions use **JSON arrays** instead of comma-separated flag values

---

## 12. Troubleshooting

### HTTP 401 Unauthorized

**Cause:** Invalid or expired bearer token.

**Fix:** Generate a new token from the SCP dashboard (Team → Service Accounts
or your personal API settings).

### HTTP 403 Forbidden

**Cause:** Your token doesn't have permission for this operation.

**Fix:** Check that your service account or user has the correct role in SCP.
You need at least "Member" access to manage NATS users.

### "sk_group_id is required" or invalid signing key group

**Cause:** Missing or incorrect signing key group ID.

**Fix:** Find the correct signing key group ID in the SCP dashboard under your
account's settings. Every user must belong to a signing key group.

### User already exists

**Cause:** A user with that name already exists in the account.

**Fix:** The `scp setup` command handles this gracefully — it skips creation
and updates permissions instead. For manual creation, use `scp list-users` to
find the existing user's ID.

### "Permissions Violation" when connecting

**Cause:** The user's permissions don't include the subjects your service
needs, or `_INBOX.>` is missing.

**Fix:**
```bash
go run ./cmd/scp update-permissions \
    --base-url <url> --token <token> \
    --user <user-id> \
    --allow-sub "<your-subjects>,_INBOX.>" \
    --allow-pub "<your-subjects>,_INBOX.>"
```

### Credentials work with nsc but not SCP (or vice versa)

**Cause:** The `.creds` file format is identical — both contain a user JWT and
nkey. If one works and the other doesn't, the issue is likely different
permission settings.

**Fix:** Compare permissions using `nsc describe user` vs `scp describe-user`
and ensure they match.

### Service connects but messages don't flow

**Cause:** Subject mismatch between pipeline stages.

**Fix:** Verify the chain:
```
publisher --out msg.raw
processor --in msg.raw --out msg.enhanced
postprocessor --in msg.enhanced --out msg.final
subscriber --subject msg.final
```
Each `--out` must exactly match the next stage's `--in`.
