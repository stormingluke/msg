# NATS Authenticated Pipeline Walkthrough

This walkthrough sets up JWT-based authentication for a three-stage NATS
message pipeline using **nsc** (NATS Security Credentials tool). By the end
you will have a running nats-server that enforces per-user subject permissions,
and three Go binaries communicating through it.

## Prerequisites

| Tool | Tested version | Install |
|------|---------------|---------|
| Go | 1.23+ | https://go.dev/dl |
| nats-server | 2.12+ | `go install github.com/nats-io/nats-server/v2@latest` |
| nsc | 2.12+ | `go install github.com/nats-io/nsc/v2@latest` |
| nats CLI | 0.3+ | `go install github.com/nats-io/natscli/nats@latest` |

## Project layout

```
msg/
├── cmd/
│   ├── publisher/main.go     # publishes Envelope to msg.raw
│   ├── processor/main.go     # subscribes msg.raw, enriches, publishes msg.enhanced
│   └── subscriber/main.go    # subscribes msg.enhanced, prints JSON
├── internal/
│   ├── natsclient/           # NATS connection wrapper (handles --creds)
│   └── messaging/            # Envelope type, Publish, Subscribe, Process helpers
├── Makefile                  # every step of this walkthrough
└── WALKTHROUGH.md            # you are here
```

## The pipeline at a glance

```
publisher ──msg.raw──▶ processor ──msg.enhanced──▶ subscriber
```

Every message is a JSON `Envelope`:

```go
// internal/messaging/message.go
type Envelope struct {
    ID          string            `json:"id"`
    Text        string            `json:"text"`
    PublishedAt time.Time         `json:"published_at"`
    Metadata    map[string]string `json:"metadata,omitempty"`
}
```

The publisher creates an Envelope with a random ID and user-supplied text. The
processor adds metadata (word count, timestamp, version). The subscriber
pretty-prints the final result.

---

## Step 1 — Bootstrap the nsc hierarchy

```
make setup-nsc
```

This creates an **operator → account → user** hierarchy that NATS uses for
JWT-based auth:

```
Operator: msg
└── Account: PIPELINE
    ├── User: publisher
    ├── User: subscriber
    └── User: processor
```

### What is nsc?

`nsc` manages cryptographic identities for NATS. An **operator** owns the
system. **Accounts** isolate groups of users (think tenants). **Users** are
individual identities that connect to the server.

### Subject permissions

Each user gets scoped permissions so they can only access the subjects they
need:

| User | Allowed publish | Allowed subscribe |
|------|----------------|-------------------|
| publisher | `msg.raw`, `_INBOX.>` | `_INBOX.>` |
| subscriber | `_INBOX.>` | `msg.raw`, `msg.enhanced`, `_INBOX.>` |
| processor | `msg.enhanced`, `_INBOX.>` | `msg.raw`, `_INBOX.>` |

The `_INBOX.>` wildcard is required because the nats.go client uses
request-reply internally during connection setup. Without it, connections fail
with a permissions error.

With only allow lists specified, NATS implicitly denies access to any subject
not listed — each user is locked to exactly the subjects above.

**Example:** the publisher's permissions are set with:

```bash
nsc edit user --name publisher --account PIPELINE \
    --allow-pub "msg.raw,_INBOX.>" \
    --allow-sub "_INBOX.>"
```

This means `publisher` can put messages onto `msg.raw` but cannot subscribe to
anything except its own reply inbox.

---

## Step 2 — Generate the server config

```
make server-config
```

This runs `nsc generate config --nats-resolver` to produce a `nats-server.conf`
that references the operator's JWT and signing key. It also creates a
`nats-data/` directory where the server stores resolved account JWTs at
runtime.

The generated config tells nats-server: "only accept connections from users who
present a valid JWT signed by this operator."

---

## Step 3 — Export credentials

```
make creds
```

This generates a `.creds` file for each user in `creds/`:

```
creds/
├── publisher.creds
├── subscriber.creds
└── processor.creds
```

A `.creds` file bundles a user's JWT and private nkey into one file. It is the
only thing a binary needs to authenticate. In the codebase, credentials are
loaded in `internal/natsclient/client.go`:

```go
// internal/natsclient/client.go
func New(cfg Config) (*Client, error) {
    opts := []nats.Option{
        nats.Name(cfg.ConnName),
    }
    if cfg.CredsFile != "" {
        opts = append(opts, nats.UserCredentials(cfg.CredsFile))
    }
    conn, err := nats.Connect(cfg.ServerURL, opts...)
    // ...
}
```

When `--creds` is passed on the command line, `nats.UserCredentials()` reads the
file and presents the JWT + signed nonce to the server during the TLS-like
handshake.

---

## Step 4 — Start the server

```
make run-server
```

Runs `nats-server -c ./nats-server.conf` in the foreground. Leave this terminal
open.

---

## Step 5 — Push account JWTs

In a **second terminal**:

```
make push-accounts
```

This runs `nsc push --all --system-account SYS`, which uploads the PIPELINE
account JWT to the running server's resolver. Until this step, the server knows
the operator but has no account data — user connections would be rejected.

You only need to run this once (or again after editing account/user settings).

---

## Step 6 — Run the subscriber

Still in the second terminal:

```
make run-subscriber
```

This starts:

```bash
go run ./cmd/subscriber --subject msg.enhanced --creds creds/subscriber.creds
```

The subscriber connects with `subscriber.creds`, subscribes to `msg.enhanced`,
and waits. Each received Envelope is printed as indented JSON:

```go
// cmd/subscriber/main.go
sub, err := messaging.Subscribe(client.Conn, flagSubject, flagQueue, func(env messaging.Envelope) error {
    out, _ := json.MarshalIndent(env, "", "  ")
    fmt.Println(string(out))
    return nil
})
```

---

## Step 7 — Run the processor

In a **third terminal**:

```
make run-processor
```

This starts:

```bash
go run ./cmd/processor --creds creds/processor.creds
```

The processor subscribes to `msg.raw` in the `processors` queue group,
applies `DefaultEnhancer`, and publishes the enriched message to
`msg.enhanced`:

```go
// internal/messaging/processor.go
func DefaultEnhancer(in Envelope) (Envelope, error) {
    out := in
    if out.Metadata == nil {
        out.Metadata = make(map[string]string)
    }
    out.Metadata["word_count"] = fmt.Sprintf("%d", len(strings.Fields(in.Text)))
    out.Metadata["processed_at"] = time.Now().UTC().Format(time.RFC3339)
    out.Metadata["processor_version"] = "1.0.0"
    return out, nil
}
```

---

## Step 8 — Publish messages

In a **fourth terminal**:

```
make run-publisher
```

This runs:

```bash
go run ./cmd/publisher --message "hello nats world" --count 5 --interval 500ms \
    --creds creds/publisher.creds
```

The publisher creates 5 Envelopes 500ms apart and sends each to `msg.raw`.

---

## Expected output

In the **subscriber** terminal you should see 5 messages like:

```json
{
  "id": "a1b2c3d4e5f6a7b8",
  "text": "hello nats world",
  "published_at": "2026-03-24T10:00:00Z",
  "metadata": {
    "word_count": "3",
    "processed_at": "2026-03-24T10:00:00Z",
    "processor_version": "1.0.0"
  }
}
```

The `metadata` block was added by the processor — it did not exist when the
publisher sent the message.

---

## Verifying permissions work

Because each user has scoped subject permissions, using the wrong credentials
will fail. For example, trying to subscribe with the publisher's credentials:

```bash
go run ./cmd/subscriber --creds creds/publisher.creds --subject msg.raw
```

This will fail because `publisher` only has subscribe access to `_INBOX.>` —
NATS implicitly denies everything not in the allow list. You should see a
permissions violation error from the server.

Similarly, the subscriber cannot publish:

```bash
go run ./cmd/publisher --creds creds/subscriber.creds --message "sneaky"
```

This fails because `subscriber` only has publish access to `_INBOX.>` — NATS
implicitly denies everything not in the allow list.

---

## Quick reference

| Command | What it does |
|---------|-------------|
| `make setup-nsc` | Create operator, account, users, and set permissions |
| `make server-config` | Generate `nats-server.conf` from nsc |
| `make creds` | Export `.creds` files to `creds/` |
| `make run-server` | Start nats-server (foreground, terminal 1) |
| `make push-accounts` | Push account JWTs to the running server |
| `make run-subscriber` | Start subscriber on `msg.enhanced` (terminal 2) |
| `make run-processor` | Start processor (terminal 3) |
| `make run-publisher` | Publish 5 test messages (terminal 4) |
| `make demo` | Print the above instructions |
| `make clean` | Remove generated files |

---

## Cleanup

```
make clean
```

This removes `creds/`, `nats-data/`, `nats-server.conf`, and any compiled
binaries. It does **not** remove the nsc store (`~/.local/share/nats/nsc/stores`)
— delete that manually if you want a fully clean slate.
