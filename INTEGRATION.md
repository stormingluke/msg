# Integrating a New Service with the NATS Pipeline

This guide walks you through adding a new microservice to an **existing** NATS
cluster that uses JWT-based authentication. No prior NATS or nsc experience is
required.

By the end, you will understand the auth model, have credentials for your
service, and see it running in the pipeline.

---

## 1. What You're Working With

NATS uses a three-level identity hierarchy for authentication:

```
Operator (connectizer)          ← owns the cluster
└── Account (high-007)          ← your team's namespace
    ├── User: publisher         ← one per service
    ├── User: processor
    └── User: your-new-service  ← what you're adding
```

**Operator** — the cluster admin identity. You won't touch this.
**Account** — isolates a group of services. All your team's services share the
`high-007` account.
**User** — one per service. Each user gets a `.creds` file that bundles a JWT
(identity) and an nkey (private key). Your service passes this file at startup
to authenticate.

### What's in the S3 tar?

The tar distributed from S3 contains two directory trees:

```
.config/nsc.json                            ← nsc configuration (points to the store)
.local/share/nats/nsc/
├── stores/
│   └── connectizer/                        ← operator store
│       ├── connectizer.jwt
│       └── accounts/
│           └── high-007/
│               ├── high-007.jwt
│               └── users/                  ← existing user JWTs
└── keys/
    └── keys/                               ← signing keys (nkeys)
```

This gives you everything `nsc` needs to manage users and generate credentials
locally.

### Why `.creds` files matter

A `.creds` file is the **only runtime artifact** your service needs. It
contains the user JWT and private nkey. Your Go code passes it via
`nats.UserCredentials(path)` and the NATS client handles the rest — presenting
the JWT and signing a challenge during the connection handshake.

---

## 2. Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.23+ | https://go.dev/dl |
| nsc | 2.12+ | `go install github.com/nats-io/nsc/v2@latest` |
| nats CLI | 0.3+ (optional) | `go install github.com/nats-io/natscli/nats@latest` |

You do **not** need `nats-server` locally — the cluster is already running.
You only need `nsc` to create your user and generate credentials.

---

## 3. Setting Up Your Local Environment

Download the nsc tar from S3 and unpack it into your home directory:

```bash
# Download (your team lead will provide the exact URL)
aws s3 cp s3://your-bucket/nsc-config.tar.gz /tmp/nsc-config.tar.gz

# Unpack into the standard nsc locations
cd ~
tar xzf /tmp/nsc-config.tar.gz
```

This places files at:
- `~/.config/nsc.json` — nsc config
- `~/.local/share/nats/nsc/stores/` — operator and account JWTs
- `~/.local/share/nats/nsc/keys/` — signing keys

Verify the setup:

```bash
nsc list keys
```

You should see keys for the `connectizer` operator and `high-007` account. If
you get "no operator found", check that the tar was unpacked to the correct
paths.

You can also verify the account:

```bash
nsc describe account high-007
```

---

## 4. Adding a New User for Your Service

Each service needs its own user identity with scoped subject permissions.

```bash
# Create the user under the high-007 account
nsc add user --name <your-service> --account high-007
```

Then set subject permissions. The permissions you need depend on what your
service does:

```bash
nsc edit user --name <your-service> --account high-007 \
    --allow-sub "<subjects-to-read>" \
    --allow-pub "<subjects-to-write>,_INBOX.>" \
    --deny-pub ">"
```

### Permissions cheat sheet

| Pattern | Use when | Example flags |
|---------|----------|---------------|
| **Publish only** | Service produces messages but doesn't consume | `--allow-pub "msg.raw" --allow-sub "_INBOX.>" --deny-sub ">"` |
| **Subscribe only** | Service consumes messages but doesn't produce | `--allow-sub "msg.final,_INBOX.>" --allow-pub "_INBOX.>" --deny-pub ">"` |
| **Processor (sub + pub)** | Service reads from one subject, writes to another | `--allow-sub "msg.enhanced,_INBOX.>" --allow-pub "msg.final,_INBOX.>" --deny-pub ">"` |
| **Wildcard subscriber** | Service monitors all messages under a prefix | `--allow-sub "msg.>,_INBOX.>" --allow-pub "_INBOX.>" --deny-pub ">"` |

The `_INBOX.>` entries are required — the NATS client uses request-reply
internally during connection setup. Without them, connections fail with a
permissions violation.

The `--deny-pub ">"` prevents your service from accidentally publishing to
subjects it shouldn't. Always include it.

### Generate the credentials file

```bash
nsc generate creds --account high-007 --name <your-service> > <your-service>.creds
```

This produces a single file your service uses at runtime. Keep it secure —
anyone with this file can authenticate as your service.

---

## 5. Pushing the Updated Account

After adding a user or changing permissions, push the updated account JWT to
the running cluster:

```bash
nsc push --account high-007
```

This uploads the account JWT (which now includes your new user) to the
cluster's built-in resolver. The change takes effect immediately — existing
connections are not disrupted, and your new user can connect right away.

If you get a connection error, make sure you can reach the cluster (check VPN,
firewall rules, etc.) and that you have the correct operator signing key in
your local nsc store.

---

## 6. Wiring Your Go Service

All services in this repo follow the same connection pattern. The key pieces:

### 1. Accept a `--creds` flag

```go
root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file")
```

### 2. Pass it to `natsclient.New()`

```go
client, err := natsclient.New(natsclient.Config{
    ServerURL: flagServer,
    CredsFile: flagCreds,
    ConnName:  "your-service",
})
```

### 3. Under the hood

`natsclient.New()` calls `nats.UserCredentials(cfg.CredsFile)`, which reads
your `.creds` file and handles the JWT+nkey authentication handshake
automatically:

```go
// internal/natsclient/client.go
if cfg.CredsFile != "" {
    opts = append(opts, nats.UserCredentials(cfg.CredsFile))
}
conn, err := nats.Connect(cfg.ServerURL, opts...)
```

That's it. No manual JWT parsing, no key management in your code.

---

## 7. Worked Example: postprocessor

Let's walk through adding `cmd/postprocessor` — a service that reads from
`msg.enhanced`, adds more metadata, and publishes to `msg.final`. This is a
concrete example of steps 4–6.

### 7a. Create the user and set permissions

```bash
nsc add user --name postprocessor --account high-007

nsc edit user --name postprocessor --account high-007 \
    --allow-sub "msg.enhanced,_INBOX.>" \
    --allow-pub "msg.final,_INBOX.>" \
    --deny-pub ">"
```

The postprocessor can subscribe to `msg.enhanced` and publish to `msg.final`.
Nothing else.

### 7b. Generate credentials

```bash
nsc generate creds --account high-007 --name postprocessor > creds/postprocessor.creds
```

### 7c. Push the account

```bash
nsc push --account high-007
```

### 7d. Write the enhancer

Create `internal/messaging/postprocess.go`:

```go
package messaging

import (
    "fmt"
    "time"
)

func PostprocessEnhancer(in Envelope) (Envelope, error) {
    out := in
    if out.Metadata == nil {
        out.Metadata = make(map[string]string)
    }
    out.Metadata["char_count"] = fmt.Sprintf("%d", len(in.Text))
    out.Metadata["postprocessed_at"] = time.Now().UTC().Format(time.RFC3339)
    out.Metadata["postprocessor_version"] = "1.0.0"
    return out, nil
}
```

This follows the same `EnhancerFunc` signature as `DefaultEnhancer`. The
`Process()` function in `processor.go` accepts any `EnhancerFunc`, so the
postprocessor reuses it directly.

### 7e. Write the binary

Create `cmd/postprocessor/main.go` — it's nearly identical to
`cmd/processor/main.go` with different defaults:

| Flag | Processor | Postprocessor |
|------|-----------|---------------|
| `--in` | `msg.raw` | `msg.enhanced` |
| `--out` | `msg.enhanced` | `msg.final` |
| `--queue` | `processors` | `postprocessors` |
| ConnName | `"processor"` | `"postprocessor"` |
| Enhancer | `DefaultEnhancer` | `PostprocessEnhancer` |

The `run()` function calls the same `messaging.Process()` with different
arguments:

```go
return messaging.Process(ctx, client.Conn, flagIn, flagOut, flagQueue, messaging.PostprocessEnhancer)
```

### 7f. Run it

```bash
go run ./cmd/postprocessor --creds creds/postprocessor.creds
```

Or using the Makefile:

```bash
make run-postprocessor
```

---

## 8. Testing Locally

You can test the full pipeline locally using the project's Makefile. This
simulates the cluster setup on your machine.

### One-time setup

```bash
make setup-nsc       # create operator, account, users (including postprocessor)
make server-config   # generate nats-server.conf
make creds           # generate .creds files for all users
```

### Run the 5-stage pipeline

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

The full data flow:

```
publisher → msg.raw → processor → msg.enhanced → postprocessor → msg.final → subscriber
```

---

## 9. Permissions Reference

Common permission patterns for services in the pipeline:

| Role | Allow publish | Allow subscribe | Example service |
|------|--------------|-----------------|-----------------|
| Producer | `msg.<subject>` | `_INBOX.>` | publisher |
| Consumer | `_INBOX.>` | `msg.<subject>,_INBOX.>` | subscriber |
| Processor | `msg.<out>,_INBOX.>` | `msg.<in>,_INBOX.>` | processor, postprocessor |
| Monitor | `_INBOX.>` | `msg.>,_INBOX.>` | dashboard, logger |

All roles should include `--deny-pub ">"` (or `--deny-sub ">"` for producers)
to prevent accidental wildcard access.

### Subject naming conventions

- `msg.raw` — unprocessed messages from publishers
- `msg.enhanced` — messages enriched by the first processor stage
- `msg.final` — messages ready for final consumption
- `msg.>` — wildcard matching all message subjects
- `_INBOX.>` — NATS internal reply subjects (always allow)

---

## 10. Troubleshooting

### "Permissions Violation" on connect

**Cause:** Your user doesn't have `_INBOX.>` in its allow-subscribe list.

**Fix:**
```bash
nsc edit user --name <your-service> --account high-007 \
    --allow-sub "<your-subjects>,_INBOX.>"
```
Then `nsc push --account high-007`.

### "Permissions Violation" when publishing/subscribing

**Cause:** Your user doesn't have permission for the subject you're using.

**Fix:** Check what your service is trying to publish/subscribe to and update
permissions accordingly. Use `nsc describe user <name>` to see current
permissions.

### "No responders" or timeouts

**Cause:** No service is consuming from the subject you're publishing to, or
the consuming service hasn't started yet.

**Fix:** Make sure all pipeline stages are running. Check that subjects match
end-to-end (`--out` of one stage matches `--in` of the next).

### "User authentication expired"

**Cause:** The user JWT has expired.

**Fix:** Regenerate credentials:
```bash
nsc generate creds --account high-007 --name <your-service> > <your-service>.creds
```
Restart your service with the new `.creds` file.

### "Account not found" or "No matching account"

**Cause:** The account JWT hasn't been pushed to the server, or the server
doesn't know about the account yet.

**Fix:**
```bash
nsc push --account high-007
```

### Stale permissions after editing a user

**Cause:** You edited the user in nsc but didn't push the updated account.

**Fix:** Always run `nsc push` after any `nsc edit user` or `nsc add user`.
The server resolves accounts from its local cache, which is only updated on
push.

### Service connects but messages don't flow

**Cause:** Subject mismatch between stages.

**Fix:** Verify the chain:
```
publisher --out msg.raw
processor --in msg.raw --out msg.enhanced
postprocessor --in msg.enhanced --out msg.final
subscriber --subject msg.final
```
Each `--out` must exactly match the next stage's `--in`.
