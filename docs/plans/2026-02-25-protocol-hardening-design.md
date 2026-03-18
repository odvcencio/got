# Graft Protocol Hardening — Design

**Goal:** Make graft's wire protocol production-grade: bulletproof transport, pack-compressed transfers with zstd and delta encoding, sideband framing for progress/errors, and smart negotiation with server-advertised limits.

**Audience:** graft CLI client (`pkg/remote`) and orchard server (handler side).

---

## Current State

Graft uses a JSON-over-HTTP protocol with these endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/refs` | GET | Advertise all refs as JSON map |
| `/refs` | POST | Atomic CAS ref updates |
| `/objects/batch` | POST | Batch negotiate & fetch objects (JSON) |
| `/objects/{hash}` | GET | Fetch single object by hash |
| `/objects` | POST | Push objects (newline-delimited JSON) |

Objects transfer as individual JSON blobs. No compression. No delta encoding on wire. Hardcoded buffer limits and timeouts. No capability negotiation. No progress reporting.

### Known Issues

- **Response buffer mismatch:** `ListRefs`/`BatchObjects` capped at 2MB, `GetObject` allows 32MB. Large ref lists silently truncate.
- **No hash format validation:** Server can inject empty strings or garbage as hashes.
- **Hardcoded 60s HTTP timeout:** Large fetches on slow connections die.
- **No Content-Type validation:** Server HTML error pages cause cryptic JSON unmarshal failures.
- **No retry logic:** One network blip = immediate failure.
- **No compression:** Full objects on wire, no deltas, no zstd/gzip.
- **No progress reporting:** Fetch/push operations are silent until completion or failure.
- **Hardcoded batch limits:** 50K objects, 20K haves — constants mirroring orchard's current server config.
- **No ref pagination:** All refs fetched in one call regardless of count.

---

## Layer 1: Transport Robustness

Foundation layer. Make HTTP calls reliable and predictable.

### Retry with Exponential Backoff

New `retryDo()` method wrapping `http.Client.Do()`:
- 3 attempts, backoff: 1s, 2s, 4s
- Retry on: network errors, HTTP 429 (rate limit), HTTP 5xx (server error)
- Do NOT retry: 4xx client errors, auth failures
- Request body buffered once, replayed on retry (for POST requests)

### Configurable Timeouts

- Replace hardcoded 60s with `ClientOptions.Timeout` field (default 60s)
- Per-request context support — callers pass `context.WithTimeout` for individual operations
- Batch operations use longer default (5 minutes)

### Consistent Response Limits

Fix the 2MB vs 32MB mismatch. Per-endpoint limits:

| Endpoint | Limit | Rationale |
|----------|-------|-----------|
| `ListRefs` | 8MB | Handles repos with thousands of refs |
| `BatchObjects` | 64MB | Object payloads are large |
| `GetObject` | 32MB | Single object (keep current) |
| `UpdateRefs` | 1MB | Small JSON response |
| `PushObjects` | 1MB | Small ack response |

### Content-Type Validation

After every response, check `Content-Type` header:
- JSON endpoints must return `application/json`
- Pack endpoints must return `application/x-graft-pack`
- Wrong type returns structured error: `"server returned text/html, expected application/json (status 502)"`
- Catches proxy/CDN error pages that currently cause cryptic unmarshal errors

### Files

- `pkg/remote/client.go` — retry logic, timeout config, buffer limits, content-type checks

---

## Layer 2: Protocol Validation & Capabilities

Hash validation, structured errors, and capability negotiation that enables Layer 3.

### Hash Format Validation

New `ValidateHash(h Hash) error`:
- Check non-empty
- Check length (SHA-256 = 64 hex chars)
- Check hex charset
- Called at every protocol boundary: after `ListRefs`, `BatchObjects`, `GetObject`
- Reject malformed hashes before they infect the local store

### Structured Error Responses

Server errors return JSON body on all non-2xx responses:

```json
{"error": "ref not found", "code": "ref_not_found", "detail": "heads/main"}
```

Client parses into typed `RemoteError` with code, message, detail. Falls back gracefully if server sends non-JSON (content-type check from Layer 1 catches this).

### Capability Header

Every request includes:
- `Graft-Protocol: 1`
- `Graft-Capabilities: pack,zstd,sideband`

Server responds with:
- `Graft-Capabilities: pack,zstd,sideband` (intersection of what both support)

Layer 3 features activate only when both sides agree. Clean degradation — if server doesn't support pack transport, falls back to current JSON.

### Files

- New `pkg/remote/protocol.go` — hash validation, `RemoteError` type, capability parsing
- Modified `pkg/remote/client.go` — header injection, error handling

---

## Layer 3: Pack Wire Format + Zstd + Sideband Framing

The showcase layer. Three interlocking pieces.

### 3a: Pack-on-Wire

Use graft's existing pack format (`pack_reader.go`, `pack_writer.go`, `pack_delta.go`) as the transfer encoding instead of individual JSON objects.

**Fetch (server to client):**
- Client: POST `/objects/batch` with `Accept: application/x-graft-pack`
- Request body stays JSON: `{"wants": [...], "haves": [...], "max_objects": 50000}`
- Server responds with pack stream — Content-Type: `application/x-graft-pack`
- Pack contains delta-compressed objects (OFS deltas against the have-set)
- Client unpacks using existing `pack_reader.go`

**Push (client to server):**
- Client: POST `/objects` with `Content-Type: application/x-graft-pack`
- Body is a pack file — delta-compressed against objects the server already has
- Server responds with JSON ack
- Replaces current newline-delimited JSON push

**Why this works:** The pack reader/writer already handle delta resolution, object type headers, and hash verification. We pipe them over HTTP instead of to disk.

### 3b: Zstd Compression

Wrap HTTP bodies in zstd — not gzip. Zstd is faster to decompress, better compression ratios, and Go has excellent support via `github.com/klauspost/compress/zstd`.

- Push requests: `Content-Encoding: zstd` on body
- Fetch requests: `Accept-Encoding: zstd`, server compresses response
- Compresses the already-compact pack format — typically 2-4x further reduction
- Streaming-friendly: zstd supports streaming encode/decode

**Net effect:** A push that currently sends 100MB of JSON-encoded objects becomes ~8-15MB of delta-compressed, zstd-wrapped pack data.

### 3c: Sideband Framing

Binary framing that multiplexes data with progress and errors. Real-time feedback during fetch/push.

Frame format:

```
[4 bytes: frame length, big-endian uint32]
[1 byte: channel]
  0x01 = data     (pack bytes)
  0x02 = progress ("Receiving objects: 45% (1234/2741)")
  0x03 = error    ("object store full")
[N bytes: payload]
```

- Wraps the pack stream — data channel carries pack bytes, progress channel carries status text
- Client renders progress bars during clone/fetch/push
- Error channel lets server report issues mid-stream instead of just closing the connection
- Only activates when both sides advertise `sideband` capability (from Layer 2)

### Files

- New `pkg/remote/pack_transport.go` — pack encode/decode for wire (wraps existing pack reader/writer)
- New `pkg/remote/sideband.go` — frame reader/writer
- New `pkg/remote/compress.go` — zstd encode/decode wrappers
- Modified `pkg/remote/client.go` — content negotiation, new code paths when capabilities match

---

## Layer 4: Configurable Limits & Smart Negotiation

Dynamic limits and intelligent batch strategy.

### Server-Advertised Limits

When client sends `Graft-Capabilities`, server responds with limits:

```
Graft-Limits: max_batch=50000,max_payload=67108864,max_object=33554432
```

- Client respects these instead of guessing with hardcoded constants
- Different servers (self-hosted orchard, future mirrors) advertise different limits
- If server doesn't send limits, client falls back to current defaults

### Bisect-Style Negotiation

Current strategy: send up to 20K haves every round, hope for the best.

New strategy: binary search for the common ancestor.
1. Start with the N most recent commits on each wanted ref
2. If server says "already have," move deeper
3. If server says "don't have," we found the boundary
4. Fewer rounds, less data on the wire, faster time-to-first-object

Same `FetchConfig` struct, smarter default strategy.

### Ref Pagination

```
GET /refs?cursor=<token>&limit=1000
```

Server response:

```json
{"refs": {"heads/main": "abc123..."}, "cursor": "next_page_token"}
```

Empty cursor = last page. Handles repos with thousands of tags without buffering everything.

### Files

- Modified `pkg/remote/sync.go` — bisect negotiation logic, paginated ref fetching
- Modified `pkg/remote/client.go` — limit parsing, paginated `ListRefs`

---

## Composition

```
Layer 1: Transport        retry, timeouts, buffer limits, content-type checks
Layer 2: Protocol         hash validation, structured errors, capability header
Layer 3: Wire Format      pack transport, zstd compression, sideband framing
Layer 4: Negotiation      server limits, bisect strategy, ref pagination
```

Each layer builds on the previous. Layer 1 is independently useful. Layer 2 enables Layer 3 (capabilities gate pack/zstd/sideband). Layer 4 optimizes the negotiation that Layers 1-3 make reliable.

## Both Sides

All changes touch both graft (client) and orchard (server):

**graft (client):**
- `pkg/remote/client.go` — all layers
- `pkg/remote/protocol.go` — new, Layer 2
- `pkg/remote/pack_transport.go` — new, Layer 3
- `pkg/remote/sideband.go` — new, Layer 3
- `pkg/remote/compress.go` — new, Layer 3
- `pkg/remote/sync.go` — Layer 4

**orchard (server):**
- Handler endpoints need to support pack responses, zstd, sideband, capability headers, structured errors, paginated refs, limit advertisement
