# WeChat iLink Bot API Spec

> Undocumented private HTTP API provided by Tencent's iLink Bot service (`https://ilinkai.weixin.qq.com`). Reverse-engineered from the openclaw-weixin plugin.

---

## Authentication

All API requests require the following headers:

```
Authorization: Bearer {bot_token}
AuthorizationType: ilink_bot_token
Content-Type: application/json
```

`bot_token` is issued by the server upon QR login completion and remains valid until session expiry (errcode=-14).

---

## API Endpoints

Base URL: `https://ilinkai.weixin.qq.com` (or the `baseurl` returned at login for IDC routing)

### 1. Fetch Login QR Code

```
GET /ilink/bot/get_bot_qrcode?bot_type=3
```

No auth headers required.

**Response:**
```json
{
  "qrcode": "<hex session token — used to poll scan status>",
  "qrcode_img_content": "<scannable URL — encode this as the QR image>"
}
```

- `qrcode_img_content` is what the user scans — use this as QR image content
- `qrcode` is the session key for status polling, not the QR content itself

---

### 2. Poll QR Scan Status

```
GET /ilink/bot/get_qrcode_status?qrcode={qrcode}
```

Server-side long-poll; each request holds ~35s. Client must loop until terminal status.

**Response:**
```json
{
  "status": "wait | scaned | confirmed | expired | scaned_but_redirect",
  "bot_token": "<auth token, present on confirmed>",
  "ilink_bot_id": "<bot account ID>",
  "baseurl": "<alternate base URL for IDC routing>",
  "ilink_user_id": "<user's ID>",
  "redirect_host": "<new base URL when status is scaned_but_redirect>"
}
```

**Status transitions:**
```
wait → scaned → confirmed
              → scaned_but_redirect → (switch baseURL, continue polling) → confirmed
     → expired
```

- On `confirmed`: extract `bot_token` + `ilink_user_id` + `baseurl` and save as credentials
- On `scaned_but_redirect`: switch `baseURL` to `redirect_host` and continue polling

---

### 3. Long-Poll for Inbound Messages

```
POST /ilink/bot/getupdates
```

**Request:**
```json
{
  "get_updates_buf": "<cursor from previous response; empty string for fresh session>",
  "base_info": { "channel_version": "jcode/1.0.0" }
}
```

**Response:**
```json
{
  "ret": 0,
  "errcode": 0,
  "errmsg": "",
  "msgs": [ <WeixinMessage> ],
  "get_updates_buf": "<new cursor — must be persisted>",
  "longpolling_timeout_ms": 35000
}
```

**Key behaviors:**
- Server holds the connection for ~35s; returns early only if messages arrive
- **The first `getupdates` call implicitly activates the session.** Calling `sendmessage` before the first `getupdates` completes returns `ret=-2`
- `get_updates_buf` must be persisted to disk after every response; used to resume after restart
- `get_updates_buf=""` signals a fresh session with no prior state

**Error codes:**

| ret / errcode | Meaning |
|---|---|
| 0 | Success |
| -2 | Session not yet activated (first `getupdates` has not completed) |
| -14 | Session expired — re-login required |

---

### 4. Send Message

```
POST /ilink/bot/sendmessage
```

**Request:**
```json
{
  "msg": {
    "from_user_id": "",
    "to_user_id": "<ilink_user_id>",
    "client_id": "<client-generated unique ID for deduplication>",
    "message_type": 2,
    "message_state": 2,
    "item_list": [
      {
        "type": 1,
        "text_item": { "text": "<message content>" }
      }
    ],
    "context_token": "<optional; echo verbatim from inbound message>"
  },
  "base_info": { "channel_version": "jcode/1.0.0" }
}
```

**Response:** Empty JSON `{}` on success; body contains `ret`/`errcode`/`errmsg` on failure.

**Field notes:**
- `message_type: 2` = BOT
- `message_state: 2` = FINISH (complete message)
- `client_id` is client-generated; use a timestamp or UUID
- `context_token` is extracted from inbound messages and should be echoed back to maintain session context

---

## Message Structure

### WeixinMessage (inbound)

```json
{
  "seq": 1,
  "message_id": 123456,
  "from_user_id": "<sender user_id>",
  "to_user_id": "<bot user_id>",
  "client_id": "<sender client ID>",
  "create_time_ms": 1713312000000,
  "message_type": 1,
  "message_state": 2,
  "item_list": [ <MessageItem> ],
  "context_token": "<session context token — echo on outbound>"
}
```

### MessageItem Types

| type | Kind | Fields |
|---|---|---|
| 1 | TEXT | `text_item.text` |
| 2 | IMAGE | `media` (CDN-encrypted) |
| 3 | VOICE | `media`, `encode_type`, `sample_rate`, `playtime` |
| 4 | FILE | `media`, `file_name`, `md5`, `len` |
| 5 | VIDEO | `media`, `thumb_media`, `video_size`, `play_length` |

---

## Session Lifecycle

```
QR Login
  ├─ GET /get_bot_qrcode → obtain qrcode + qrcode_img_content
  ├─ Display qrcode_img_content as QR image for user to scan
  └─ Poll /get_qrcode_status → status=confirmed → save bot_token + user_id + baseurl

Enable
  └─ Start pollLoop (goroutine)
       ├─ POST /getupdates (long-poll, ~35s) ← implicitly activates session
       ├─ Persist get_updates_buf
       └─ Dispatch inbound messages

Send Message
  └─ POST /sendmessage
       ├─ If ret=-2: session not yet active; retry (first getupdates may take ~35s)
       └─ If ret=-14: session expired; re-login required
```

---

## Caveats

1. **`sendmessage` requires `getupdates` to have completed first.** The long-poll is the implicit session activation handshake. Calls before the first poll completes return `ret=-2` and must be retried.
2. **`baseurl` is mutable.** The `scaned_but_redirect` status and the login `baseurl` field may differ from the default. Always use the URL saved in credentials.
3. **`get_updates_buf` must be persisted.** It is the server-side cursor; losing it means either replaying old messages or missing new ones after a restart.
4. **Session expiry (errcode=-14).** Clear credentials and restart the QR login flow.
5. **WeChat client has limited Markdown support.** Bold (`**`), code fences, and tables render correctly. H5/H6 headings and inline images do not.
