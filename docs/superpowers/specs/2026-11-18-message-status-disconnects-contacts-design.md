# Design: Real-Time Message Statuses, Disconnection Surfacing & Legible Contact Names

Date: 2026-11-18
Status: Ready for User Approval

## 1. Intent & Why

Local testing exposed three interconnected reliability and UX gaps:
1. **Silent send failures & hardcoded checkmarks:** `Server.SendMessage` saves outgoing messages as `pending`, dispatches `SendOutgoingMessage` asynchronously, and immediately returns success. If the plugin is not running (`GetInstance` returns `false`), the message stays `pending` forever; if the async call fails, `EventMessageStatus` is published to `eventbus` but `Server.StreamEvents` never subscribes to it. Meanwhile, `gui/src/app.js` hardcodes `fa-check-double` on all outgoing bubbles regardless of `msg.status`.
2. **Silent plugin/account disconnections:** When `StreamIncomingEvents` in `core/cmd/conduitd/main.go` fails or closes, the goroutine exits silently without updating account status in SQLite, notifying the GUI, or attempting recovery. Neither `channel.disconnected` nor `EventChannelConnected` reaches `StreamEvents`.
3. **Unreadable & locked contact names:** Contacts whose initial `display_name` came from a non-legible PushName (e.g. `"."`, `"🕷️"`, `"‼️"`) cannot be overwritten by subsequent `contact.sync` events because `identity.Graph.ResolveOrLinkChannel` locks any non-ID `DisplayName`. Users also have no way to manually rename a contact without risking future syncs overwriting it.

## 2. Goals, Non-Goals & Anti-Goals

### Goals
- Surface accurate outgoing message lifecycle statuses (`pending`, `sent`, `delivered`, `read`, `failed`) in real time via gRPC `StreamEvents` and distinct GUI check/status icons (including clear failure feedback when a message fails to send).
- Detect plugin stream drops and `channel.disconnected` events in `conduitd`, update account status to `disconnected`, forward `channel_status_update` over `StreamEvents`, and show clear disconnection status/banners in the GUI.
- Add `name_locked` protection on `contacts`, prefer legible names (containing at least one Unicode letter or digit) over symbol/punctuation-only names during auto-sync, add `RenameContact` RPC + GUI inline rename, and display/search phone/handle when a contact has no legible name.

### Non-Goals
- Merging `feat/contact-unification-redesign` into this branch (this branch targets `main` cleanly).
- Modifying external plugin binaries (`conduit-plugin-whatsapp`, `conduit-plugin-telegram`), though `conduitd` will ingest outgoing status events (`message.status_updated`, `message.delivered`, `message.read`) whenever plugins emit them.

### Anti-Goals
- Polling `GetMessages` / `GetAccounts` from the GUI instead of using `StreamEvents`.
- Allowing `contact.sync` or incoming messages to overwrite a user-edited (`name_locked = true`) contact name.

## 3. Architecture & Detailed Design

### 3.1. Protobuf Contract (`sdk/proto/conduit.proto`)
- Add `MessageStatusEvent` (`message_id`, `conversation_id`, `status`, `remote_message_id`, `error`) to `EventEnvelope.payload` (`message_status_updated = 9`).
- Reuse existing `EventEnvelope.channel_status_update = 5` for real-time connection/disconnection notices (structured prefix e.g. `provider:disconnected[:reason]`, `provider:connected`, `provider:auth_error[:reason]`).
- Add `RenameContact(RenameContactRequest) returns (RenameContactResponse)` (`contact_id`, `display_name`) to `ConduitService`.

### 3.2. Outgoing Message Status & Delivery/Read Ingestion (`core`)
- **`Server.SendMessage` (`core/pkg/rpc/server.go`):**
  - If `s.plugins == nil` or `!s.plugins.GetInstance(pluginName)`: mark `domainMsg.Status = domain.StatusFailed`, persist it, publish `EventMessageStatus` (with error `"plugin not running"`), and still return the saved `ProtoMessage` (`status: "failed"`).
  - On async `SendOutgoingMessage` failure or success: persist `StatusFailed` / `StatusSent` + `RemoteMessageID` and publish `EventMessageStatus` (`MessageStatusEvent` payload with error message on failure).
- **`Server.StreamEvents` (`core/pkg/rpc/server.go`):**
  - Subscribe to `eventbus.EventMessageStatus` and `eventbus.EventChannelConnected`, forwarding `message_status_updated` and `channel_status_update` envelopes.
- **Incoming outgoing-receipt events (`core/cmd/conduitd/main.go`):**
  - Handle plugin events `message.status_updated` / `message.delivered` / `message.read` (when referencing an outgoing message by `remote_message_id`) to update message status (`sent` -> `delivered` -> `read`, respecting monotonic non-downgrade in SQLite) and publish `EventMessageStatus`.

### 3.3. Disconnection Detection & Surfacing (`core` + `gui`)
- **`core/cmd/conduitd/main.go`:**
  - Add `channel.disconnected` case in `StreamIncomingEvents` switch: update matching provider accounts in SQLite to `disconnected` and publish `EventChannelConnected` (`"<provider>:disconnected:<reason>"`).
  - When `stream.Recv()` returns an error or EOF: update matching provider accounts to `disconnected`, publish `EventChannelConnected` (`"<provider>:disconnected:<err>"`), and log clearly.
- **`gui/src/app.js`:**
  - Handle `channel_status_update` in the event stream listener: refresh accounts list and show a clear non-intrusive connection alert/banner when a provider disconnects (and clear/update when reconnected).

### 3.4. Outgoing Message Status UI (`gui`)
- Replace hardcoded `fa-check-double` in `appendMessageBubble` (`gui/src/app.js`) with `renderMessageStatusIcon(status, error)`:
  - `pending`: `fa-clock` (muted)
  - `sent`: `fa-check` (muted)
  - `delivered`: `fa-check-double` (muted)
  - `read`: `fa-check-double` with `.msg-status-read` (accent/blue)
  - `failed`: `fa-circle-exclamation` (red, with clear `"No enviado"` / error tooltip)
- Handle `message_status_updated` events in `gui/src/app.js` to update the existing bubble's status icon in place without re-rendering the whole chat (and show an error toast/banner when a message transitions to `failed`).

### 3.5. Legible Contact Names, Manual Rename & Resync Lock (`core` + `gui`)
- **Schema (`core/pkg/storage/sqlite.go`, `core/pkg/domain/models.go`):**
  - Add `name_locked INTEGER NOT NULL DEFAULT 0` to `contacts` (via `ALTER TABLE contacts ADD COLUMN name_locked INTEGER NOT NULL DEFAULT 0` migration).
  - Add `NameLocked bool` to `domain.Contact` and `Storage.RenameContact(ctx, contactID, displayName string) (*domain.Contact, error)` (sets `display_name`, `name_locked = 1`, and updates `conversations.title` for conversations linked to that contact).
- **Legibility Helper & Sync Protection (`core/pkg/identity/graph.go`):**
  - `IsLegibleName(s string) bool`: returns `true` iff `s` contains at least one Unicode letter (`unicode.IsLetter`) or digit (`unicode.IsDigit`).
  - In `ResolveOrLinkChannel`:
    - If `contact.NameLocked` is `true`, **never overwrite** `contact.DisplayName`.
    - Otherwise, allow updating `contact.DisplayName` when current `DisplayName` is empty, ID-like, OR `!IsLegibleName(current)`, AND incoming `displayName` satisfies `IsLegibleName(incoming)`.
    - When `DisplayName` is updated, also update `conversations.title` if the conversation's title was empty, ID-like, or non-legible.
- **RPC & GUI (`core/pkg/rpc/server.go`, `gui/`):**
  - Implement `RenameContact` RPC in `core/pkg/rpc/server.go` (validates non-empty trimmed name, updates DB with `name_locked = 1`).
  - In `gui/conduit-client.js`, `gui/main.js`, `gui/preload.js`: expose `renameContact(contactId, displayName)`.
  - In `gui/src/app.js`: when a conversation/contact title is non-legible (no letters/digits), display `phone` / `handle` alongside it so it is identifiable; allow chat search to match `phone`/`handle`; add an inline "Renombrar contacto" button in the chat header when `contact_id` is available.

## 4. Risk
`Risk: item 1 (data/migration — adds name_locked column to contacts via ALTER TABLE with DEFAULT 0, safe and additive for existing conduit.db)`
