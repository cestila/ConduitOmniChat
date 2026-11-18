# Contact Store & Automatic Unification Redesign

**Date:** 2026-11-17
**Status:** Ready for user approval

## 1. Intent & Why

ConduitOmniChat needs contacts to work as a clean, reliable address book across all messaging providers:

1. **Common contact base:** Extract individual contacts from every connected provider into a shared store—not just people who already have an open chat.
2. **Automatic deduplication:** Automatically unify contacts that represent the same person across providers using their phone number, without requiring the user to manually merge, split, or edit identities.
3. **Provider-first chat initiation:** Let the user browse their unified contact list and pick *which* provider/account on a contact they want to start a conversation with, even if no conversation exists yet.
4. **Plugin-extensible contract:** Provide a clean, first-class plugin contract so current and future providers can supply 1:1 contacts without leaking groups, topics, channels, or servers into the contact store.

Today, contact syncing is passive (`contact.sync` events pushed on connect), overloaded with groups and forum topics, drops Telegram phone numbers that lack a leading `+`, lacks live UI updates or an on-demand `SyncContacts` RPC, and exposes complex manual merge/split UI that the user wants removed.

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
- Extract 1:1 person contacts from all connected provider plugins into a common `contacts` + `contact_channels` database via an explicit `SyncContacts` contract (automatic on connect + on-demand from the UI).
- Strictly exclude groups, channels, forum topics, broadcast lists, and servers from `contacts`.
- Automatically unify contacts across providers by normalized E.164 phone number (fixing Telegram `user.Phone` normalization and preserving WhatsApp `@lid` → `@s.whatsapp.net` resolution).
- Remove all manual contact merging, channel splitting, and manual identity editing from the GUI.
- Allow starting a chat from any contact by selecting the desired provider/account directly on that contact, creating/ensuring the 1:1 conversation with that channel set as active.
- Push real-time contact updates to clients via `EventEnvelope` so the contact list never goes stale.

### Non-Goals
- Manual merge, split, or identity-override UI.
- Unifying Discord contacts with WhatsApp/Telegram (Discord does not expose third-party phone numbers; Discord contacts still appear in the common list with their Discord channel).
- Unifying group chats, Discord servers/channels, or Telegram forum topics.
- Multi-device or cloud contact sync outside local SQLite.

### Anti-Goals
- Storing groups, topics, or system broadcasts as rows in `contacts` (or creating `contact_channels` with empty `contact_id` for 1:1 flows).
- Requiring the user to manually fix phone formatting or merge duplicate entries created by missing `+` prefixes.
- Duplicating contact storage in a separate staging table (`provider_contacts`) when `contact_channels` already represents per-provider identities.

---

## 3. Constraints

- **Hexagonal / Clean boundaries:** Core (`core/pkg/identity`, `core/pkg/storage`) must not depend on provider-specific internals (e.g., Core should not need to know WhatsApp's `session.db` schema; plugins resolve their own canonical identifiers).
- **Backward compatibility with existing chats:** Existing `conversations` and `messages` in SQLite must remain intact. 1:1 conversations continue to map 1:1 to a unified `contact_id` (`conversation.id == contact.id`).
- **Graceful plugin degradation:** Not all plugins may implement `SyncContacts` immediately (for example, `plugins/discord` is currently a compiled binary without Go source in `dist-public/`). Core must treat `Unimplemented` from `SyncContacts` as a non-fatal skip for that provider.
- **Proto additive evolution:** Changes to `sdk/proto/conduit.proto` must regenerate `sdk/conduitv1/*.pb.go` cleanly and keep existing RPCs compiling.
- **Multi-repo plugin SDK consumption *(architect call)*:** `dist-public/conduit-plugin-whatsapp` and `dist-public/conduit-plugin-telegram` are separate Git repos pinning `sdk v0.1.0`. During implementation, use a temporary local `go.mod` `replace` (or `go.work`) to build updated `plugins/` binaries cleanly; bump and tag `sdk` when releasing.

---

## 4. Approaches Considered

### Approach A — Explicit Plugin `SyncContacts` Contract + Evolution of `contacts` / `contact_channels` *(Selected)*
- **How it works:** Add `SyncContacts` to `PluginService` and `ConduitService`, plus `StartChat(contact_id, channel_id)` and a live `ContactUpdatedEvent` in `EventEnvelope`. Keep `contacts` + `contact_channels` as the common store, fix E.164 phone normalization (accepting digits without `+`), purge any non-person rows from `contacts`, and simplify the GUI to a searchable contact list with per-provider "Start Chat" buttons and a "Sync Contacts" button.
- **Tradeoffs:** Reuses existing schema and preserves all message history and unified 1:1 threads; requires a startup cleanup migration for any legacy group/topic contamination in `contacts` and proto regeneration.

### Approach B — Separate `provider_contacts` Staging Table + Projection Engine
- **How it works:** Plugins dump raw contacts into a new `provider_contacts` table, and a separate projection job groups them by phone into `contacts`.
- **Tradeoffs:** Allows re-running matching rules from raw snapshots without touching `contacts`, but duplicates `contact_channels`, complicates consistency with `conversations`, and requires migrating existing channel/conversation foreign keys.

**Decision:** The user selected **Approach A** with an explicit contact sync mechanism.

---

## 5. Architecture & Design

### 5.1 Protobuf & Plugin Contract (`sdk/proto/conduit.proto`)
1. **`PluginService.SyncContacts(SyncContactsRequest) returns (SyncContactsResponse)`**
   - `SyncContactsRequest`: optional `account_id` (empty = all accounts managed by the plugin).
   - `SyncContactsResponse`: `repeated PluginContact contacts` (containing `account_id`, `remote_id`, `display_name`, `avatar_url`, `phone`, `email`, `handle`), `int32 synced_count`, `string error`.
   - **Contract rule for all plugins:** `SyncContacts` must **only** return/emit individual 1:1 person contacts. Groups, channels, topics, and bots/broadcasts must never be emitted as contacts.
2. **`ProtoChannel` multi-account support:**
   - Add `account_id` to `ProtoChannel` so callers and the GUI can distinguish multiple local accounts on the same provider (e.g., two WhatsApp accounts).
3. **`ConduitService.SyncContacts(SyncContactsRequest) returns (SyncContactsResponse)`**
   - Triggers contact extraction across all connected accounts/plugins (or a filtered `provider` / `account_id`), runs each contact through `identity.Graph.ResolveOrLinkChannel`, emits live contact update events, and returns summary counts (`synced_count`, `unified_count`).
4. **`ConduitService.StartChat(StartChatRequest) returns (StartChatResponse)`**
   - Request: `contact_id`, `channel_id`.
   - Behavior: Validates that `channel_id` belongs to `contact_id`, ensures the 1:1 `Conversation` (`id = contact_id`) exists in SQLite, sets `active_channel_id = channel_id` (which binds both the remote contact channel and its local `account_id`), and returns `ProtoConversation`.
5. **`EventEnvelope` live contact event:**
   - Add `ContactUpdatedEvent` (containing `ProtoContact contact`) to `EventEnvelope.payload` so the GUI updates `state.contacts` in real time after syncs or incoming messages.

### 5.2 Common Store & Automatic Unification (`core/pkg/storage`, `core/pkg/identity`)
1. **Multi-account channel schema (`core/pkg/storage/sqlite.go`):**
   - Add `account_id TEXT NOT NULL DEFAULT ''` to `contact_channels` and migrate the uniqueness constraint from `UNIQUE(provider, remote_id)` to `UNIQUE(provider, account_id, remote_id)` so the same remote contact reached from two different local accounts (e.g., two WhatsApp accounts) is stored as two selectable channels on the same unified `Contact`.
   - Update `identity.Graph.ResolveOrLinkChannel` to accept `accountID` and key channel lookups by `(provider, account_id, normRemoteID)`.
2. **Phone normalization fix (`core/pkg/identity/normalizer.go`):**
   - Update `PhoneFromIdentifier` / add `NormalizePhone(raw string) string` so raw numeric phone strings from providers (e.g., Telegram's `5491112345678` without `+`) are normalized to E.164 (`+5491112345678`) when they contain 8–15 digits.
   - Ensure `ResolveOrLinkChannel` normalizes `hints.Phone` through this function before matching or storing `contacts.phone`.
2. **Automatic unification flow:**
   - When `SyncContacts` or an incoming 1:1 message provides a contact with a valid E.164 phone:
     - If a `ContactChannel` for `(provider, remote_id)` already exists, `linkLateIdentity` attaches the phone and merges into any existing owner of that phone (consolidating 1:1 conversations).
     - If the channel is new and a `Contact` with that `phone` already exists, the new `ContactChannel` is attached to that existing `Contact` (no duplicate contact created).
     - Otherwise, a new `Contact` + `ContactChannel` is created.
3. **1:1 person-only invariant & cleanup:**
   - `conduitd` must reject or route to conversation-only sync any group/topic/server target (`@g.us`, `@broadcast`, `status@broadcast`, `_topic_`, server/channel IDs) so they never enter `contacts`.
   - Add an idempotent startup cleanup in `core/pkg/storage` to delete any legacy `contacts` rows whose channels are exclusively group/topic/broadcast targets (without deleting group `conversations`).
4. **Performance fix:**
   - Replace the N+1 query in `storage.DB.ListContacts` (`core/pkg/storage/contacts.go`) with a single `contact_channels` batch query or `JOIN` mapped by `contact_id`.

### 5.3 Provider Plugins
- **WhatsApp (`dist-public/conduit-plugin-whatsapp`):**
  - Implement `SyncContacts` using `Store.Contacts.GetAllContacts`, filtering out `@g.us`, `@broadcast`, and `status@broadcast`.
  - Resolve `@lid` JIDs to `@s.whatsapp.net` via `resolveCanonicalJID` and populate `phone` from the canonical JID.
- **Telegram (`dist-public/conduit-plugin-telegram`):**
  - Implement `SyncContacts` using Telegram's address book (`contacts.getContacts`) and cached users, excluding bots, channels, and groups, and passing `user.Phone` (so Core normalizes it to E.164 and unifies with WhatsApp).
- **Discord & Future Plugins:**
  - Any plugin implementing `PluginService.SyncContacts` automatically participates in common contact extraction and phone unification.
  - If a plugin (such as the current `plugins/discord` binary) returns `codes.Unimplemented` for `SyncContacts`, Core logs a warning and continues syncing other providers.

### 5.4 GUI Redesign (`gui/`)
1. **Remove manual identity UI:**
   - Remove the merge dropdowns/form, identity search-and-merge panel, `splitContactChannel` button, and `editContactIdentity` button from `gui/src/index.html` and `gui/src/app.js`.
   - Remove unused manual identity IPC handlers if no longer needed by any UI path (`mergeContacts`, `splitChannel`, `setContactIdentity`, `findContactsByIdentity` — **builder call** whether to keep or remove from `preload.js`/`main.js`).
2. **Clean Contact List UI:**
   - Header: Search bar (filters by name, phone, handle, or provider) + **"Sync Contacts"** button with loading/result feedback.
   - Contact item: Avatar, Display Name, Phone (if present), and **provider action buttons/chips** for each linked `ContactChannel` (e.g., `WhatsApp`, `Telegram`, `Discord` with account/handle tooltip).
   - Clicking a provider button calls `StartChat(contact.id, channel.id)`, switches to the Chats view, selects that conversation, and pre-selects `channel.id` in the composer's account selector.
   - If a contact has only 1 channel, clicking the contact row (or its single provider button) starts the chat directly with that channel.
   - Subscribe to `ContactUpdatedEvent` in the GUI event stream handler to refresh `state.contacts` live.

---

## 6. Left to the Builder

The following decisions are intentionally left to the implementation plan / builder:
1. Whether deprecated `MergeContacts` / `SplitChannel` / `SetContactIdentity` RPCs remain in `conduit.proto` as deprecated server methods or are removed, and whether their IPC bridges are removed from `gui/preload.js`.
2. Handling edge cases in phone normalization (e.g., regional mobile prefixes like Argentina `+549...` vs `+54...` if encountered, or storing multiple phones per contact in a `contact_identities` table vs single normalized `contacts.phone`).
3. How legacy `contact_channels` rows with `account_id = ''` are backfilled during migration when matching existing `accounts` rows.
4. Exact visual layout (list rows vs compact cards) and styling of provider/account chips in `gui/src/index.html` / `gui/src/styles.css` (making sure multiple local accounts on the same provider show a clear account label on each chip).
