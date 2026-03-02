# Graft Wire Protocol Specification

**Version:** 1
**Status:** Normative
**Date:** 2026-03-02

This document specifies the Graft remote protocol used between a Graft client
and an Orchard-compatible server. A conforming server implementation can be
built entirely from this specification.

---

## Table of Contents

1. [Protocol Overview](#1-protocol-overview)
2. [Headers](#2-headers)
3. [Capability Negotiation](#3-capability-negotiation)
4. [Authentication](#4-authentication)
5. [Repository Endpoints](#5-repository-endpoints)
6. [Management API Endpoints](#6-management-api-endpoints)
7. [Pack Format](#7-pack-format)
8. [Entity Trailer Format](#8-entity-trailer-format)
9. [Object Serialization](#9-object-serialization)
10. [Error Handling](#10-error-handling)
11. [Transport Modes](#11-transport-modes)
12. [Server Limits](#12-server-limits)
13. [Retry Semantics](#13-retry-semantics)
14. [Sideband Framing](#14-sideband-framing)
15. [Shallow Clones and Object Filters](#15-shallow-clones-and-object-filters)

---

## 1. Protocol Overview

The Graft protocol operates over HTTP/HTTPS using JSON request and response
bodies for control messages, and a Git-compatible binary pack format for bulk
object transfer.

### 1.1 Protocol Version

The current protocol version is **`1`**. The version is transmitted in the
`Graft-Protocol` header on every request.

### 1.2 Transport

All communication uses HTTP/1.1 or later over TLS (HTTPS). Servers MAY accept
plain HTTP for development, but production deployments MUST use HTTPS.

### 1.3 Base URL Format

Repository endpoints are scoped under a base URL with the following canonical
form:

```
https://{host}/graft/{owner}/{repo}
```

The client normalizes all remote URLs to this form. The following inputs are
equivalent:

| Input | Normalized |
|-------|-----------|
| `https://orchard.dev/graft/alice/myrepo` | `https://orchard.dev/graft/alice/myrepo` |
| `https://orchard.dev/alice/myrepo` | `https://orchard.dev/graft/alice/myrepo` |
| `https://orchard.dev/api/v1/graft/alice/myrepo` | `https://orchard.dev/api/v1/graft/alice/myrepo` |

The base URL never has a trailing slash. All repository endpoints described in
[Section 5](#5-repository-endpoints) are relative to this base URL.

### 1.4 Hash Function

Graft uses **SHA-256** throughout. All hashes are represented as 64-character
lowercase hexadecimal strings. Objects are hashed using the envelope format
described in [Section 9.1](#91-object-hashing).

### 1.5 Default Orchard Host

When no host is configured, the client defaults to `https://orchard.dev`.

---

## 2. Headers

Every request from a Graft client to a Graft-aware server includes the
following custom headers:

### 2.1 Graft-Protocol

```
Graft-Protocol: 1
```

Specifies the protocol version. The server MUST reject requests with an
unsupported version with HTTP 400.

### 2.2 Graft-Capabilities

```
Graft-Capabilities: pack,sideband,zstd
```

A comma-separated list of capabilities the client supports. See
[Section 3](#3-capability-negotiation) for the full list.

### 2.3 Graft-Limits

Sent by the **server** in response headers to advertise operational limits:

```
Graft-Limits: max_batch=50000,max_payload=67108864,max_object=33554432
```

Format: comma-separated `key=value` pairs. Keys are:

| Key | Type | Description |
|-----|------|-------------|
| `max_batch` | integer | Maximum number of objects in a single batch response |
| `max_payload` | integer | Maximum request/response payload size in bytes |
| `max_object` | integer | Maximum single object size in bytes |

Unknown keys MUST be ignored. Invalid or non-positive values MUST be ignored
(the field defaults to 0, meaning "use client default").

The client caches these limits from the first response that includes them and
uses them to constrain subsequent requests.

---

## 3. Capability Negotiation

### 3.1 Defined Capabilities

| Capability | Description |
|-----------|-------------|
| `pack` | Client supports Git-compatible pack binary transport |
| `zstd` | Client supports zstd compression for pack payloads |
| `sideband` | Client supports sideband multiplexed streams |
| `shallow` | Client supports shallow clone boundaries |
| `filter` | Client supports partial clone object filters |
| `include-tag` | Client requests tag objects be included when fetching tagged commits |

### 3.2 Negotiation Process

1. The client sends its full capability set in the `Graft-Capabilities` header.
2. The server inspects the client capabilities and intersects them with its own
   supported set.
3. The server selects the most efficient transport mode from the intersection
   (e.g., pack+zstd if both are present).
4. The server MAY include a `Graft-Capabilities` response header indicating the
   agreed capabilities.

The standard client advertises: `pack,zstd,sideband`.

### 3.3 Intersection

The effective capability set is the intersection of client and server
capabilities. If the server does not support `pack`, the protocol falls back
to JSON object transport.

---

## 4. Authentication

### 4.1 Credential Sources

The client resolves credentials in the following priority order:

| Priority | Source | Auth Method |
|----------|--------|-------------|
| 1 | `GRAFT_TOKEN` environment variable | Bearer token |
| 2 | `~/.graftconfig` `token` field | Bearer token |
| 3 | `GRAFT_USERNAME` + `GRAFT_PASSWORD` environment variables | HTTP Basic |
| 4 | URL userinfo (e.g., `https://user:pass@host/...`) | HTTP Basic |

### 4.2 Bearer Token

When a token is available, the client sends:

```http
Authorization: Bearer <token>
```

### 4.3 HTTP Basic Auth

When username/password credentials are available and no Bearer token exists:

```http
Authorization: Basic <base64(username:password)>
```

### 4.4 Header Placement

Authentication headers are sent on **every** request alongside the protocol
headers (`Graft-Protocol`, `Graft-Capabilities`).

### 4.5 User Configuration File

The `~/.graftconfig` file is a JSON file with mode `0600`:

```json
{
  "version": 1,
  "orchard_url": "https://orchard.dev",
  "token": "graft_pat_...",
  "username": "alice",
  "owner": "alice",
  "signing_key_path": "/home/alice/.graft/signing_key",
  "auto_sign": true
}
```

---

## 5. Repository Endpoints

All repository endpoints use the base URL `{base}` as defined in
[Section 1.3](#13-base-url-format).

### 5.1 List Refs

Retrieves all references (branches, tags) in the remote repository.

```
GET {base}/refs
```

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `cursor` | string | No | Opaque pagination cursor from a previous response |
| `limit` | integer | No | Maximum refs per page (client default: 1000) |

#### Request

```http
GET /graft/alice/myrepo/refs?limit=1000 HTTP/1.1
Host: orchard.dev
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123
```

#### Response (Paginated Format)

```http
HTTP/1.1 200 OK
Content-Type: application/json
Graft-Limits: max_batch=50000,max_payload=67108864

{
  "refs": {
    "heads/main": "a1b2c3d4e5f6...64 hex chars...",
    "tags/v1.0": "f6e5d4c3b2a1...64 hex chars..."
  },
  "cursor": "next_page_token"
}
```

When `cursor` is empty or the `refs` key is absent, there are no more pages.

#### Response (Legacy Flat Format)

Older servers may return a flat map without pagination:

```json
{
  "heads/main": "a1b2c3d4e5f6...64 hex chars...",
  "tags/v1.0": "f6e5d4c3b2a1...64 hex chars..."
}
```

The client detects this by checking whether the top-level JSON has a `refs`
key. If not, the entire object is treated as the ref map and pagination
terminates.

#### Pagination Loop

```
cursor = ""
loop:
    GET {base}/refs?cursor={cursor}&limit=1000
    parse response
    merge refs into local map
    if cursor == "" or refs key absent:
        break
    cursor = response.cursor
```

#### Response Limit

The client reads at most **8 MB** from the response body.

#### Error Response

On failure, the server returns a non-200 status with a JSON error body (see
[Section 10](#10-error-handling)).

---

### 5.2 Update Refs (CAS)

Atomically updates one or more references using compare-and-swap semantics.

```
POST {base}/refs
```

#### Request

```http
POST /graft/alice/myrepo/refs HTTP/1.1
Host: orchard.dev
Content-Type: application/json
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123

{
  "updates": [
    {
      "name": "heads/main",
      "old": "a1b2c3d4...old hash...",
      "new": "f6e5d4c3...new hash..."
    },
    {
      "name": "heads/feature",
      "new": "abcdef01...new hash..."
    }
  ]
}
```

#### Update Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Reference name (e.g., `heads/main`, `tags/v1.0`) |
| `old` | string | No | Expected current hash (CAS guard). Omit for unconditional create. |
| `new` | string | Yes | New hash value. Empty string `""` deletes the ref. |

If `old` is provided and the server's current value does not match, the entire
batch MUST be rejected atomically.

#### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "updated": {
    "heads/main": "f6e5d4c3...new hash...",
    "heads/feature": "abcdef01...new hash..."
  }
}
```

The `updated` map contains each successfully updated ref and its new hash.

#### Response Limit

The client reads at most **1 MB** from the response body.

---

### 5.3 Batch Object Negotiation

Fetches objects reachable from `wants` that are not reachable from `haves`.

```
POST {base}/objects/batch
```

#### Request

```http
POST /graft/alice/myrepo/objects/batch HTTP/1.1
Host: orchard.dev
Content-Type: application/json
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123

{
  "wants": [
    "a1b2c3d4e5f6..."
  ],
  "haves": [
    "f6e5d4c3b2a1..."
  ],
  "max_objects": 50000
}
```

#### Request Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `wants` | string[] | Yes | Hashes of objects the client needs (at least one) |
| `haves` | string[] | No | Hashes the client already has (for graph pruning) |
| `max_objects` | integer | No | Maximum objects to return in this response |

#### Accept Header for Transport Mode

The client signals its preferred response format via the `Accept` header:

| Accept Header | Meaning |
|--------------|---------|
| `application/x-graft-pack` | Prefer binary pack transport |
| (absent or `application/json`) | JSON object transport |

When requesting pack transport, the client also sends:

```http
Accept-Encoding: zstd
```

The server MUST respect the `Accept` header. If the server cannot provide the
requested format, it falls back to `application/json`.

#### JSON Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "objects": [
    {
      "hash": "a1b2c3d4e5f6...",
      "type": "commit",
      "data": "<base64-encoded bytes>"
    },
    {
      "hash": "b2c3d4e5f6a1...",
      "type": "blob",
      "data": "<base64-encoded bytes>"
    }
  ],
  "truncated": false
}
```

| Field | Type | Description |
|-------|------|-------------|
| `objects` | array | Array of object records |
| `objects[].hash` | string | SHA-256 hash (64 hex chars) |
| `objects[].type` | string | Object type: `blob`, `commit`, `tree`, `tag`, `entity`, `entitylist` |
| `objects[].data` | bytes | Base64-encoded raw object data (Go `[]byte` JSON encoding) |
| `truncated` | boolean | `true` if more objects exist beyond `max_objects` |

#### Pack Response

```http
HTTP/1.1 200 OK
Content-Type: application/x-graft-pack
Content-Encoding: zstd
X-Truncated: true

<zstd-compressed pack stream bytes>
```

When `Content-Type` is `application/x-graft-pack`:

1. If `Content-Encoding` contains `zstd`, decompress the body first.
2. Decode the body as a pack stream (see [Section 7](#7-pack-format)).
3. Check the `X-Truncated` header: if `"true"`, more rounds are needed.

#### Response Limit

The client reads at most **64 MB** from the response body.

#### Batch Negotiation Loop

The client performs iterative negotiation when responses are truncated:

```
known_haves = initial_haves
for round in 1..max_rounds:
    response = POST /objects/batch { wants, haves=known_haves[tail], max_objects }
    store response objects
    known_haves += response object hashes
    if not response.truncated:
        break
    if no new objects in this round:
        break  // avoid infinite loops
```

After negotiation completes, the client performs a **graph closure walk**:
starting from each want, it recursively walks the object graph locally and
fetches any missing object individually via `GET /objects/{hash}`.

#### Negotiation Defaults

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_objects` | 50,000 | Maximum objects per batch request |
| `max_haves` | 20,000 | Maximum have hashes sent per request |
| `max_rounds` | 1,024 | Maximum negotiation rounds before failing |

---

### 5.4 Get Single Object

Fetches a single object by its hash.

```
GET {base}/objects/{hash}
```

#### Request

```http
GET /graft/alice/myrepo/objects/a1b2c3d4e5f6... HTTP/1.1
Host: orchard.dev
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123
```

#### Response

```http
HTTP/1.1 200 OK
X-Object-Type: commit

<raw object bytes>
```

The response body contains the raw serialized object data (not base64, not
JSON). The object type is conveyed in the `X-Object-Type` response header.

| Header | Values |
|--------|--------|
| `X-Object-Type` | `blob`, `commit`, `tree`, `tag`, `entity`, `entitylist` |

#### Response Limit

The client reads at most **32 MB** from the response body.

---

### 5.5 Push Objects (JSON Mode)

Uploads objects using newline-delimited JSON (NDJSON).

```
POST {base}/objects
```

#### Request

```http
POST /graft/alice/myrepo/objects HTTP/1.1
Host: orchard.dev
Content-Type: application/x-ndjson
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123

{"hash":"a1b2c3d4...","type":"commit","data":"<base64>"}
{"hash":"b2c3d4e5...","type":"blob","data":"<base64>"}
{"hash":"c3d4e5f6...","type":"tree","data":"<base64>"}
```

Each line is a JSON object with:

| Field | Type | Description |
|-------|------|-------------|
| `hash` | string | SHA-256 hash (64 hex chars) |
| `type` | string | Object type |
| `data` | bytes | Base64-encoded raw object data |

The client MUST verify that the computed hash matches the provided hash before
sending. If the hash field is non-empty and does not match the computed hash,
the client MUST reject the object locally.

#### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json
```

On success, the server returns HTTP 200. The response body may be empty or
contain a JSON acknowledgment.

#### Response Limit

The client reads at most **1 MB** from the response body.

---

### 5.6 Push Objects (Pack Mode)

Uploads objects using zstd-compressed pack transport.

```
POST {base}/objects
```

#### Request

```http
POST /graft/alice/myrepo/objects HTTP/1.1
Host: orchard.dev
Content-Type: application/x-graft-pack
Content-Encoding: zstd
Graft-Protocol: 1
Graft-Capabilities: pack,zstd,sideband
Authorization: Bearer graft_pat_abc123

<zstd-compressed pack stream bytes>
```

The body is a pack stream (see [Section 7](#7-pack-format)) compressed with
zstd (see [Section 11.2](#112-zstd-compression)).

The server differentiates between JSON and pack push based on the
`Content-Type` header:

| Content-Type | Mode |
|-------------|------|
| `application/x-ndjson` | JSON NDJSON push |
| `application/x-graft-pack` | Binary pack push |

#### Response

```http
HTTP/1.1 200 OK
```

On success, the server returns HTTP 200.

#### Response Limit

The client reads at most **1 MB** from the response body.

---

## 6. Management API Endpoints

Management endpoints are rooted at `{host}/api/v1/` and are not scoped to a
specific repository.

### 6.1 Create Repository

```
POST {host}/api/v1/repos
```

#### Request

```http
POST /api/v1/repos HTTP/1.1
Host: orchard.dev
Content-Type: application/json
Authorization: Bearer graft_pat_abc123

{
  "name": "myrepo",
  "description": "A new repository",
  "private": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Repository name |
| `description` | string | No | Repository description |
| `private` | boolean | No | Whether the repository is private (default: false) |

#### Response

```http
HTTP/1.1 201 Created
Content-Type: application/json
```

On success, the server returns **HTTP 201 Created**.

If the repository already exists, the server returns HTTP 400 with an error
message containing "already" and "exist" (case-insensitive), which the client
treats as a non-fatal condition.

---

### 6.2 Magic Link Authentication

#### 6.2.1 Request Magic Link

```
POST {host}/api/v1/auth/magic/request
```

```json
{
  "email": "alice@example.com"
}
```

**Response:**

```json
{
  "sent": true,
  "token": "magic_token_value",
  "expires_at": "2026-03-02T12:00:00Z"
}
```

The `token` field MAY be present for development/test servers that return the
token inline. Production servers typically send it via email, in which case
`token` may be empty.

#### 6.2.2 Verify Magic Token

```
POST {host}/api/v1/auth/magic/verify
```

```json
{
  "token": "magic_token_value"
}
```

**Response:**

```json
{
  "token": "graft_pat_abc123...",
  "user": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com"
  }
}
```

The `token` field is the long-lived auth token to be stored in
`~/.graftconfig`.

---

### 6.3 SSH Challenge/Response Authentication

#### 6.3.1 Begin SSH Challenge

```
POST {host}/api/v1/auth/ssh/challenge
```

```json
{
  "username": "alice",
  "fingerprint": "SHA256:abc123..."
}
```

The `fingerprint` field is optional; when provided, the server selects the
matching registered key.

**Response:**

```json
{
  "challenge_id": "uuid-string",
  "challenge": "random-challenge-bytes",
  "fingerprint": "SHA256:abc123..."
}
```

#### 6.3.2 Verify SSH Challenge

```
POST {host}/api/v1/auth/ssh/verify
```

```json
{
  "challenge_id": "uuid-string",
  "signature": "<base64-encoded SSH signature blob>",
  "signature_format": "ssh-ed25519"
}
```

The `signature` is the base64 encoding of the SSH signature blob produced by
signing the challenge string with the private key. The `signature_format` is
the SSH signature algorithm (e.g., `ssh-ed25519`, `ecdsa-sha2-nistp256`,
`rsa-sha2-512`).

**Response:**

```json
{
  "token": "graft_pat_abc123...",
  "user": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com"
  }
}
```

---

### 6.4 SSH Key Bootstrap

For first-time SSH key registration using a bootstrap token.

#### 6.4.1 Mint Bootstrap Token

```
POST {host}/api/v1/auth/ssh/bootstrap/token
```

Requires an existing Bearer auth token.

```json
{
  "ttl_seconds": 300
}
```

**Response:**

```json
{
  "bootstrap_token": "bootstrap_token_value",
  "expires_at": "2026-03-02T12:05:00Z"
}
```

#### 6.4.2 Bootstrap SSH Registration

```
POST {host}/api/v1/auth/ssh/bootstrap
```

```json
{
  "username": "alice",
  "name": "my-laptop",
  "public_key": "ssh-ed25519 AAAA... alice@laptop",
  "bootstrap_token": "bootstrap_token_value"
}
```

**Response:**

```json
{
  "token": "graft_pat_abc123...",
  "user": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com"
  }
}
```

---

### 6.5 Register SSH Public Key

Registers an SSH public key for the authenticated user.

```
POST {host}/api/v1/user/ssh-keys
```

Requires Bearer token authentication.

```json
{
  "name": "my-laptop",
  "public_key": "ssh-ed25519 AAAA... alice@laptop"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Human-readable key name |
| `public_key` | string | Yes | Full SSH authorized_keys format public key |

**Response:**

```http
HTTP/1.1 200 OK
```

---

## 7. Pack Format

Graft uses a Git-compatible pack format with SHA-256 checksums and an optional
Graft-specific entity trailer extension.

### 7.1 Pack Stream Layout

```
+----------------------------------------------+
| Pack Header (12 bytes)                       |
+----------------------------------------------+
| Object Entry 1                               |
| Object Entry 2                               |
| ...                                          |
| Object Entry N                               |
+----------------------------------------------+
| SHA-256 Checksum (32 bytes)                  |
+----------------------------------------------+
| Entity Trailer (optional, variable length)   |
+----------------------------------------------+
```

### 7.2 Pack Header

The pack header is exactly 12 bytes:

| Offset | Size | Field | Value |
|--------|------|-------|-------|
| 0 | 4 bytes | Magic | `PACK` (0x5041434b) |
| 4 | 4 bytes | Version | 2 (big-endian uint32) |
| 8 | 4 bytes | Object Count | N (big-endian uint32) |

### 7.3 Object Entry

Each object entry consists of a variable-length header followed by zlib-compressed
data.

#### 7.3.1 Entry Header

The entry header encodes the object type and uncompressed data size using a
variable-length encoding:

```
Byte 0:
  Bits 7:    continuation flag (1 = more size bytes follow)
  Bits 6-4:  object type (3 bits)
  Bits 3-0:  size bits [3:0]

Subsequent bytes (while continuation flag set):
  Bit 7:     continuation flag
  Bits 6-0:  size bits [shift+6:shift], shift starts at 4, increments by 7
```

#### 7.3.2 Object Types

| Type Value | Name | Description |
|-----------|------|-------------|
| 1 | `commit` | Commit object |
| 2 | `tree` | Tree object |
| 3 | `blob` | Blob, entity, or entitylist object |
| 4 | `tag` | Annotated tag object |
| 6 | `ofs_delta` | Offset-based delta |
| 7 | `ref_delta` | Hash-based delta |

**Important:** Entity and entitylist objects are encoded as pack type 3 (blob).
The entity trailer ([Section 8](#8-entity-trailer-format)) provides type
override information to recover the correct Graft type on decode.

#### 7.3.3 Compressed Data

Following the entry header is a zlib-compressed stream. The decompressed size
MUST match the size declared in the entry header.

### 7.4 Delta Entries

#### 7.4.1 OFS_DELTA (Type 6)

After the entry header comes an encoded backward distance to the base entry,
followed by zlib-compressed delta instructions.

The distance encoding:

```
Read bytes from the stream. Each byte contributes 7 bits.
Bit 7 is the continuation flag.
On continuation, the accumulator is: ((accumulator + 1) << 7) | (next & 0x7f)
```

#### 7.4.2 REF_DELTA (Type 7)

After the entry header comes a 32-byte raw SHA-256 hash of the base object,
followed by zlib-compressed delta instructions.

#### 7.4.3 Delta Instruction Format

The delta stream begins with two varints: base object size and target object
size. Then a sequence of instructions:

**Insert instruction** (bit 7 = 0):
- Command byte: bits 6-0 = length (1-127)
- Followed by `length` literal bytes to insert

**Copy instruction** (bit 7 = 1):
- Bits 0-3 select which offset bytes follow (0-4 bytes, little-endian)
- Bits 4-6 select which size bytes follow (0-3 bytes, little-endian)
- If size is 0 after decoding, it defaults to 0x10000 (65536)
- Copies `size` bytes from `offset` in the base object

**Command byte 0x00** is invalid.

### 7.5 Pack Checksum

After the last object entry, a 32-byte SHA-256 checksum is written. The
checksum covers all bytes from the pack header through the last object entry
(i.e., everything before the checksum itself).

### 7.6 Pack Index (IDX v2)

For on-disk storage, Graft uses a Git IDX v2 compatible index format with
SHA-256 hashes:

```
+-------------------------------------------+
| Header: 0xff744f63 + version 2 (8 bytes) |
+-------------------------------------------+
| Fanout table (256 x 4 bytes)             |
+-------------------------------------------+
| SHA-256 hashes (N x 32 bytes)           |
+-------------------------------------------+
| CRC-32 values (N x 4 bytes)             |
+-------------------------------------------+
| 4-byte offsets (N x 4 bytes)            |
+-------------------------------------------+
| 8-byte large offsets (variable)          |
+-------------------------------------------+
| Pack checksum (32 bytes)                 |
+-------------------------------------------+
| Index checksum (32 bytes)                |
+-------------------------------------------+
```

Entries in the index are sorted by hash. Offsets >= 2^31 use the large offset
table, indicated by setting bit 31 in the 4-byte offset entry.

---

## 8. Entity Trailer Format

The entity trailer is a Graft-specific extension appended **after** the
standard pack checksum. It maps pack blob entries to their true Graft object
types (entity, entitylist) via stable identifiers.

### 8.1 Layout

```
+----------------------------------------------+
| Magic: "GENT" (4 bytes)                     |
+----------------------------------------------+
| Version: 1 (big-endian uint16)              |
+----------------------------------------------+
| Entry Count (big-endian uint32)             |
+----------------------------------------------+
| Entry 1                                     |
| Entry 2                                     |
| ...                                          |
| Entry N                                     |
+----------------------------------------------+
| SHA-256 Checksum (32 bytes)                 |
+----------------------------------------------+
```

### 8.2 Header

| Offset | Size | Field | Value |
|--------|------|-------|-------|
| 0 | 4 bytes | Magic | `GENT` (0x47454e54) |
| 4 | 2 bytes | Version | 1 (big-endian uint16) |
| 6 | 4 bytes | Entry Count | N (big-endian uint32) |

Total header: 10 bytes.

### 8.3 Entry Format

Each entry is variable-length:

| Size | Field | Description |
|------|-------|-------------|
| 32 bytes | Object Hash | Raw SHA-256 bytes of the object |
| 2 bytes | Stable ID Length | Big-endian uint16, max 65535 |
| variable | Stable ID | UTF-8 string identifying the entity type |

### 8.4 Stable ID Convention

For type overrides, the stable ID uses the format `type:<graft_type>`:

| Stable ID | Meaning |
|-----------|---------|
| `type:entity` | Object is a Graft entity |
| `type:entitylist` | Object is a Graft entitylist |

### 8.5 Checksum

The trailer checksum is a SHA-256 hash over all trailer bytes **excluding** the
checksum itself (i.e., from the magic bytes through the last entry).

### 8.6 Normalization

Entries MUST be sorted by (object hash, stable ID) in ascending lexicographic
order before serialization. Duplicate entries are not permitted.

### 8.7 Decoding

When decoding a pack response:

1. Parse all pack entries normally (entities and entitylists appear as blob type 3).
2. If an entity trailer is present, build a type override map from entries
   whose stable ID starts with `type:`.
3. For each resolved pack entry, compute its hash as a blob. If the hash
   appears in the override map, re-assign the object type and recompute the
   hash with the correct type prefix.

---

## 9. Object Serialization

All objects are content-addressed using the envelope hash described below.

### 9.1 Object Hashing

Objects are hashed using the Git-style envelope:

```
SHA-256("{type} {length}\x00{data}")
```

Where:
- `{type}` is the object type string (`blob`, `commit`, `tree`, `tag`, `entity`, `entitylist`)
- `{length}` is the decimal byte length of `{data}`
- `\x00` is a null byte
- `{data}` is the raw serialized object bytes

The hash is the lowercase hex encoding of the resulting 32-byte SHA-256 digest
(64 hex characters).

### 9.2 Object Types

Graft defines six object types:

| Type | String | Description |
|------|--------|-------------|
| Blob | `blob` | Raw file data |
| Entity | `entity` | A single code entity (function, type, method) |
| EntityList | `entitylist` | Ordered list of entity references for a file |
| Tree | `tree` | Directory listing with entries |
| Commit | `commit` | Snapshot pointing to a tree with metadata |
| Tag | `tag` | Annotated tag pointing to another object |

### 9.3 Blob

A blob's serialized form is its raw byte content (identity serialization):

```
<raw file bytes>
```

### 9.4 Entity

The entity serialization format uses a text header/body structure:

```
version 1
kind <kind>
name <name>
declkind <declkind>
receiver <receiver>
bodyhash <hash>

<body bytes>
```

| Header | Description |
|--------|-------------|
| `version` | Serialization version (currently `1`) |
| `kind` | Entity kind: `function`, `type`, `method`, etc. |
| `name` | Entity name |
| `declkind` | Language-specific declaration kind |
| `receiver` | Method receiver (empty for non-methods) |
| `bodyhash` | SHA-256 hash of the body bytes |

The header and body are separated by a blank line (`\n\n`). The body contains
the raw source code of the entity.

### 9.5 EntityList

```
version 1
language <language>
path <file_path>

<hash1>
<hash2>
...
```

| Header | Description |
|--------|-------------|
| `version` | Serialization version (currently `1`) |
| `language` | Programming language (e.g., `go`, `python`) |
| `path` | File path relative to the repository root |

The body (after the blank line separator) contains one entity hash per line,
in the order entities appear in the source file.

### 9.6 Tree

```
version 1
<name> <mode> <blobhash> <entitylisthash> <subtreehash>
<name> <mode> <blobhash> <entitylisthash> <subtreehash>
...
```

Entries are sorted lexicographically by name. Each entry is a single line with
space-separated fields:

| Field | Description |
|-------|-------------|
| `name` | Entry name (file or directory name) |
| `mode` | Git-compatible mode string |
| `blobhash` | Blob hash, or `-` if none |
| `entitylisthash` | EntityList hash, or `-` if none |
| `subtreehash` | Subtree hash (for directories), or `-` if none |

#### Mode Values

| Mode | Type | Description |
|------|------|-------------|
| `40000` | Directory | Subdirectory entry |
| `100644` | File | Regular file |
| `100755` | File | Executable file |

A `-` represents an empty/absent hash for that field.

**Example:**

```
version 1
cmd 40000 - - a1b2c3d4...
go.mod 100644 f6e5d4c3... - -
main.go 100644 b2c3d4e5... c3d4e5f6... -
```

### 9.7 Commit

```
version 1
tree <tree_hash>
parent <parent_hash>
parent <parent_hash>
author <author_identity>
timestamp <unix_epoch_seconds>
author_tz <timezone>
committer <committer_identity>
committer_timestamp <unix_epoch_seconds>
committer_tz <timezone>

<commit message>
```

| Header | Required | Description |
|--------|----------|-------------|
| `version` | Yes | Serialization version (currently `1`) |
| `tree` | Yes | Hash of the root tree object |
| `parent` | No | Hash of a parent commit (zero or more) |
| `author` | Yes | Author identity string |
| `timestamp` | Yes | Author timestamp as Unix epoch seconds |
| `author_tz` | No | Author timezone offset (e.g., `+0000`, `-0700`) |
| `committer` | No | Committer identity string |
| `committer_timestamp` | No | Committer timestamp as Unix epoch seconds |
| `committer_tz` | No | Committer timezone offset |
| `signature` | No | Cryptographic signature string |

The header and message are separated by a blank line (`\n\n`).

**Example:**

```
version 1
tree a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
parent f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3b2a1f6e5
author Alice <alice@example.com>
timestamp 1709337600
author_tz +0000
committer Alice <alice@example.com>
committer_timestamp 1709337600
committer_tz +0000

Initial commit
```

### 9.8 Tag

```
version 1
target <target_hash>

<tag annotation bytes>
```

| Header | Description |
|--------|-------------|
| `version` | Serialization version (currently `1`) |
| `target` | Hash of the tagged object |

The tag body (after the blank line) contains the canonical tag annotation data.
The `target` hash points to the Graft object (not the Git hash), so graph
traversal stays within Graft object space.

---

## 10. Error Handling

### 10.1 Structured Error Response

When an endpoint returns a non-success HTTP status, the response body SHOULD
be a JSON object:

```json
{
  "code": "ref_conflict",
  "error": "reference update conflict",
  "detail": "heads/main expected a1b2c3d4 but was f6e5d4c3"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `code` | string | No | Machine-readable error code |
| `error` | string | Yes | Human-readable error message |
| `detail` | string | No | Additional context |

The client formats the error as: `{error} ({code}): {detail}` or
`{error} ({code})` if detail is absent.

### 10.2 Fallback

If the response body is not valid JSON or does not contain `error` or `code`,
the client uses the raw response body text (trimmed) as the error message. If
the body is empty, the HTTP status text is used.

### 10.3 Management API Errors

Management API endpoints use a simpler error format:

```json
{
  "error": "repository name already exists"
}
```

### 10.4 HTTP Status Codes

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 201 | Created (repository creation) |
| 400 | Bad request (malformed input, validation failure) |
| 401 | Unauthorized (missing or invalid credentials) |
| 403 | Forbidden (insufficient permissions) |
| 404 | Not found |
| 409 | Conflict (CAS failure on ref update) |
| 429 | Too many requests (rate limited) |
| 500+ | Server error |

---

## 11. Transport Modes

### 11.1 JSON Transport

The default fallback transport uses JSON encoding for all object data. Objects
are serialized as JSON arrays with base64-encoded `data` fields. This mode
works with any HTTP infrastructure but is less efficient for large transfers.

- **Fetch:** `POST /objects/batch` with `Accept: application/json` (or no
  Accept header). Response is `Content-Type: application/json`.
- **Push:** `POST /objects` with `Content-Type: application/x-ndjson`.

### 11.2 Pack Transport with Zstd Compression

The preferred transport uses the binary pack format with zstd compression.

- **Fetch:** `POST /objects/batch` with `Accept: application/x-graft-pack` and
  `Accept-Encoding: zstd`. Response is `Content-Type: application/x-graft-pack`
  with optional `Content-Encoding: zstd`.
- **Push:** `POST /objects` with `Content-Type: application/x-graft-pack` and
  `Content-Encoding: zstd`.

#### Zstd Compression

Zstd compression uses default compression level. The client detects zstd
encoding by checking if the `Content-Encoding` header contains the string
`zstd`.

### 11.3 Content Type Negotiation

| Client Sends | Server Response |
|-------------|-----------------|
| `Accept: application/x-graft-pack` | `Content-Type: application/x-graft-pack` (preferred) or `application/json` (fallback) |
| `Accept: application/json` | `Content-Type: application/json` |
| No Accept header | `Content-Type: application/json` |

The client MUST handle both response types regardless of what was requested,
since the server may fall back to JSON even when pack was requested.

---

## 12. Server Limits

### 12.1 Limit Advertisement

The server advertises its limits via the `Graft-Limits` response header on
any repository endpoint response. The client caches these limits from the first
response that includes them.

### 12.2 Client Response Limits

The client enforces per-endpoint response body read limits to protect against
excessive memory usage:

| Endpoint | Max Response Size |
|----------|------------------|
| `GET /refs` | 8 MB |
| `POST /objects/batch` | 64 MB |
| `GET /objects/{hash}` | 32 MB |
| `POST /refs` | 1 MB |
| `POST /objects` (response) | 1 MB |
| Default (other endpoints) | 2 MB |

### 12.3 Client Behavior

When server limits are received, the client SHOULD:

1. Limit batch request sizes to `max_batch` objects.
2. Keep total request payload under `max_payload` bytes.
3. Avoid sending individual objects larger than `max_object` bytes.

If the server does not advertise limits (header absent), the client uses its
own defaults.

---

## 13. Retry Semantics

### 13.1 Retry Policy

The client retries failed HTTP requests with exponential backoff:

| Parameter | Value |
|-----------|-------|
| Max attempts | 3 (configurable) |
| Initial backoff | 1 second |
| Backoff multiplier | 2x |
| Max backoff | 4 seconds (1s, 2s, 4s) |

### 13.2 Retryable Conditions

| Condition | Retried? |
|-----------|----------|
| Network error | Yes |
| HTTP 429 (Too Many Requests) | Yes |
| HTTP 5xx (Server Error) | Yes |
| HTTP 4xx (except 429) | No |
| HTTP 2xx (Success) | No (returns immediately) |

### 13.3 Body Replay

For requests with a body, the client buffers the entire request body before
the first attempt. On retry, the body is replayed from the buffer. The
`Content-Length` header is set explicitly for each attempt.

---

## 14. Sideband Framing

The sideband protocol multiplexes data, progress messages, and error messages
over a single byte stream using length-prefixed frames.

### 14.1 Frame Format

```
+----------------------------------+
| Frame Length (4 bytes, big-endian)|
+----------------------------------+
| Channel (1 byte)                 |
+----------------------------------+
| Payload (Frame Length - 1 bytes) |
+----------------------------------+
```

The frame length is a big-endian uint32 that includes the channel byte but not
itself. Minimum frame length is 1 (channel byte only, empty payload).

### 14.2 Channels

| Channel | Value | Description |
|---------|-------|-------------|
| Data | `0x01` | Object/pack data payload |
| Progress | `0x02` | Human-readable progress message (UTF-8) |
| Error | `0x03` | Error message (UTF-8); terminates the stream |

### 14.3 Reading

The `SidebandDataReader` presents data frames as a sequential `io.Reader`,
discarding progress frames (optionally forwarding them to a callback). On
receiving an error frame, it returns the error message. On EOF, the stream
is complete.

---

## 15. Shallow Clones and Object Filters

### 15.1 Shallow State

A shallow clone has commit boundary markers stored in a `shallow` file within
the `.graft` directory. The file contains one SHA-256 hash per line (sorted),
each identifying a commit at which graph traversal stops.

### 15.2 Object Filters

Object filters support partial clone. The `filter` capability must be present
for the server to honor filter specs.

| Filter Spec | Description |
|-------------|-------------|
| `blob:none` | Exclude all blobs |
| `blob:limit=<n>` | Exclude blobs larger than `n` bytes |
| `tree:<depth>` | Limit tree traversal depth |

### 15.3 Filter Evaluation

- `blob:none`: `AllowsBlob()` returns false for all sizes.
- `blob:limit=N`: `AllowsBlob(size)` returns true only if `size < N`.
- `tree:<depth>`: Does not restrict blobs; limits tree recursion depth.

---

## Appendix A: Complete Push Flow

1. Client calls `CollectObjectsForPush(store, roots, stopRoots)` to determine
   which objects to send (reachable from roots, excluding reachable from
   stopRoots).
2. Client encodes objects into a pack stream via `EncodePackTransport`.
3. Client compresses the pack with zstd.
4. Client sends `POST {base}/objects` with `Content-Type: application/x-graft-pack`
   and `Content-Encoding: zstd`.
5. On success, client sends `POST {base}/refs` with CAS updates to advance
   remote refs.

## Appendix B: Complete Fetch Flow

1. Client calls `GET {base}/refs` to discover remote refs (paginated).
2. Client determines want hashes (remote refs) and have hashes (local objects).
3. Client calls `POST {base}/objects/batch` with `Accept: application/x-graft-pack`
   and `Accept-Encoding: zstd`.
4. Client decodes the pack response (decompress zstd if needed, decode pack,
   resolve deltas, apply entity trailer type overrides).
5. Client verifies each object's hash matches `HashObject(type, data)`.
6. Client stores verified objects locally.
7. If `truncated` is true, client adds received object hashes to `haves` and
   repeats step 3.
8. After negotiation completes, client walks the object graph from wants,
   fetching any missing objects individually via `GET {base}/objects/{hash}`.
9. Client updates local refs.

## Appendix C: Object Graph References

The following table describes which other objects each type references, used
for graph traversal (closure walk):

| Object Type | References |
|-------------|-----------|
| `blob` | (none) |
| `entity` | (none) |
| `entitylist` | Entity hashes (from body lines) |
| `tree` | Per entry: `BlobHash`, `EntityListHash` (if present), `SubtreeHash` (if directory) |
| `commit` | `TreeHash`, all `Parents` |
| `tag` | `TargetHash` |
