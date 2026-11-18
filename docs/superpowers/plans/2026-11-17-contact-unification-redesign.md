# Contact Store & Automatic Unification Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the passive/manual contact system with a clean 1:1 common contact store synced from all provider accounts via `SyncContacts`, automatically unified by normalized E.164 phone number, and paired with a simplified GUI contact list that starts chats on any chosen provider/account.

**Architecture:** `sdk/proto/conduit.proto` gains `PluginService.SyncContacts`, `ConduitService.SyncContacts`, `ConduitService.StartChat`, `account_id` on `ProtoChannel`/`PluginContact`, and `ContactUpdatedEvent` in `EventEnvelope`. Core (`core/pkg/storage`, `core/pkg/identity`, `core/pkg/rpc`, `core/cmd/conduitd`) migrates `contact_channels` to `(provider, account_id, remote_id)` uniqueness, fixes provider phone normalization without `+`, purges legacy group/topic rows from `contacts`, and wires live contact events; `dist-public` plugins implement `SyncContacts` for 1:1 people only, and `gui/` removes all manual merge/split/identity UI in favor of a searchable list with per-account "Start Chat" chips.

**Tech Stack:** Go 1.23+, Protocol Buffers / gRPC (`protoc`), SQLite (`modernc.org/sqlite`), Electron / Vanilla JS (`node --test` in `gui/`).

**Spec:** `docs/superpowers/specs/2026-11-17-contact-unification-redesign-design.md`

## Global Constraints

- **1:1 people only:** Groups, channels, forum topics, broadcast lists, and servers must never be stored in `contacts` or returned by `SyncContacts`.
- **Multi-account support:** Must support multiple local accounts both across providers and within the same provider (`account_id` on `contact_channels`, `ProtoChannel`, `SyncContacts`, `StartChat`).
- **Graceful plugin degradation:** If a plugin returns `codes.Unimplemented` for `SyncContacts` (e.g., `plugins/discord` binary without source), Core logs a warning and continues syncing other plugins.
- **No manual merge/split UI:** All manual merge, split channel, and manual identity editing controls must be removed from the GUI.
- **Hexagonal boundaries:** Core must not depend on provider-specific internal databases (e.g., WhatsApp `session.db`); plugins resolve canonical identifiers before returning contacts.

## Review Focus

1. **Telegram phone without `+`:** `hints.Phone = "5491112345678"` must normalize to `"+5491112345678"` and auto-unify with WhatsApp `5491112345678@s.whatsapp.net` (pinned in Task 2).
2. **Multiple local accounts on the same provider:** Same remote contact reached by `wa-work` and `wa-personal` must attach two channels to one unified `Contact` without `UNIQUE` constraint failure (pinned in Task 2).
3. **Group/topic/broadcast exclusion:** `@g.us`, `@broadcast`, `status@broadcast`, and `_topic_` targets must be excluded during sync and cleaned up on startup without deleting group `conversations` (pinned in Tasks 2 & 3).
4. **Unimplemented plugin resilience:** `ConduitService.SyncContacts` must succeed and return synced counts from healthy plugins when one plugin returns `codes.Unimplemented` (pinned in Task 3).
5. **`StartChat` ownership validation:** `StartChat(contact_id, channel_id)` must reject a `channel_id` that does not belong to `contact_id` with `codes.InvalidArgument` (pinned in Task 3).

---

### Task 1: Protobuf Contract (`SyncContacts`, `StartChat`, `account_id`, `ContactUpdatedEvent`)

**Files:**
- Modify: `sdk/proto/conduit.proto:27-105,164-174,304-315`
- Regenerate: `sdk/conduitv1/conduit.pb.go`, `sdk/conduitv1/conduit_grpc.pb.go`

**Interfaces:**
- Consumes: existing `ProtoContact`, `ProtoChannel`, `ProtoConversation`, `EventEnvelope`, `PluginService`, `ConduitService`.
- Produces:
  - `ConduitService.SyncContacts(SyncContactsRequest) returns (SyncContactsResponse)`
  - `ConduitService.StartChat(StartChatRequest) returns (StartChatResponse)`
  - `PluginService.SyncContacts(SyncContactsRequest) returns (PluginSyncContactsResponse)`
  - `ProtoChannel.account_id = 10`
  - `PluginContact` (`account_id`, `remote_id`, `display_name`, `avatar_url`, `phone`, `email`, `handle`)
  - `ContactUpdatedEvent` in `EventEnvelope.payload` (`oneof`)

- [ ] **Step 1: Add RPCs and messages to `sdk/proto/conduit.proto`**

Add to `ConduitService`:
```protobuf
  rpc SyncContacts(SyncContactsRequest) returns (SyncContactsResponse);
  rpc StartChat(StartChatRequest) returns (StartChatResponse);
```
Add to `PluginService`:
```protobuf
  rpc SyncContacts(SyncContactsRequest) returns (PluginSyncContactsResponse);
```
Add `string account_id = 10;` to `ProtoChannel`, `ContactUpdatedEvent contact_updated = 15;` (next free field number in `EventEnvelope.payload`), and the request/response messages (`SyncContactsRequest` with `provider` & `account_id`, `SyncContactsResponse` with `synced_count` & `unified_count`, `PluginSyncContactsResponse` with `repeated PluginContact contacts` & `error`, `StartChatRequest` with `contact_id` & `channel_id`, `StartChatResponse` with `ProtoConversation conversation`).

- [ ] **Step 2: Regenerate Go protobuf bindings**

Run: `protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative sdk/proto/conduit.proto` (or repo's existing Makefile/script if present; verify generated files compile).
Expected: `sdk/conduitv1/conduit.pb.go` and `sdk/conduitv1/conduit_grpc.pb.go` updated cleanly.

- [ ] **Step 3: Verify `core` compiles against updated SDK**

Run: `cd core && go test ./pkg/domain/... ./pkg/identity/...`
Expected: PASS (no broken references).

- [ ] **Step 4: Commit**

```bash
git add sdk/proto/conduit.proto sdk/conduitv1/
git commit -m "feat(proto): add SyncContacts, StartChat, account_id, and ContactUpdatedEvent"
```

---

### Task 2: Phone Normalization & Multi-Account Identity Graph

**Files:**
- Modify: `core/pkg/domain/models.go:30-42`
- Modify: `core/pkg/identity/normalizer.go:65-104`
- Modify: `core/pkg/identity/graph.go:32-150`
- Modify: `core/pkg/storage/sqlite.go:76-85,165-220`
- Modify: `core/pkg/storage/contacts.go:1-150`
- Test: `core/pkg/identity/graph_test.go`, `core/pkg/identity/normalizer_test.go`, `core/pkg/storage/contacts_test.go`

**Interfaces:**
- Consumes: `domain.Contact`, `domain.ContactChannel`.
- Produces:
  - `domain.ContactChannel.AccountID string`
  - `identity.NormalizePhone(raw string) string`
  - `identity.Graph.ResolveOrLinkChannel(ctx context.Context, provider domain.Provider, accountID, rawRemoteID, handle, displayName, avatarURL string, hints Hints) (*domain.Contact, *domain.ContactChannel, error)`
  - `storage.DB.GetChannelByProviderAccountAndRemoteID(ctx context.Context, provider, accountID, remoteID string) (*domain.ContactChannel, error)`
  - `storage.DB.CleanupNonPersonContacts(ctx context.Context) (int64, error)`

- [ ] **Step 1: Write failing tests for phone normalization, multi-account channels, and legacy cleanup**

In `core/pkg/identity/normalizer_test.go`:
```go
func TestNormalizePhoneAcceptsProviderDigitsWithoutPlus(t *testing.T) {
    if got := NormalizePhone("5491112345678"); got != "+5491112345678" {
        t.Fatalf("got %q, want +5491112345678", got)
    }
    if got := PhoneFromIdentifier("5491112345678"); got != "" {
        t.Fatalf("PhoneFromIdentifier must still reject raw non-+ IDs, got %q", got)
    }
}
```
In `core/pkg/identity/graph_test.go`:
```go
func TestResolveOrLinkChannelUnifiesTelegramNoPlusWithWhatsApp(t *testing.T) {
    // WhatsApp contact "5491112345678@s.whatsapp.net" on account "wa-1"
    // followed by Telegram contact "u999" on account "tg-1" with Hints{Phone: "5491112345678"}
    // Assert both channels share the same Contact.ID and Contact.Phone == "+5491112345678".
}

func TestResolveOrLinkChannelMultipleLocalAccountsSameProvider(t *testing.T) {
    // WhatsApp contact "5491112345678@s.whatsapp.net" on account "wa-work"
    // followed by same remoteID on account "wa-personal"
    // Assert 1 Contact with 2 ContactChannels (accountIDs "wa-work" and "wa-personal").
}
```
In `core/pkg/storage/contacts_test.go`:
```go
func TestCleanupLegacyNonPersonContacts(t *testing.T) {
    // Insert a legacy contact whose only channel is "12345@g.us" and a group conversation (contact_id = NULL)
    // Run CleanupNonPersonContacts(ctx)
    // Assert the group contact is deleted, while the group conversation remains intact.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/identity/... ./pkg/storage/... -run "TestNormalizePhone|TestResolveOrLinkChannelUnifiesTelegramNoPlus|TestResolveOrLinkChannelMultipleLocalAccounts|TestCleanupLegacyNonPersonContacts" -v`
Expected: FAIL (compile error: `NormalizePhone` undefined, `AccountID` / `accountID` signature mismatch).

- [ ] **Step 3: Implement schema migration, `NormalizePhone`, multi-account `Graph`, and batch `ListContacts`**

1. Add `AccountID string` to `domain.ContactChannel` (`core/pkg/domain/models.go`).
2. In `core/pkg/storage/sqlite.go`: migrate `contact_channels` to include `account_id TEXT NOT NULL DEFAULT ''` and rebuild table/unique index if needed to enforce `UNIQUE(provider, account_id, remote_id)` without data loss; call `CleanupNonPersonContacts` on startup.
3. In `core/pkg/storage/contacts.go`: add `GetChannelByProviderAccountAndRemoteID`, update `SaveChannel` to persist `account_id`, add `CleanupNonPersonContacts`, and batch-load `contact_channels` in `ListContacts` (single query instead of N+1).
4. In `core/pkg/identity/normalizer.go`: add `NormalizePhone(raw string) string` (delegating to `PhoneFromIdentifier` first, then `canonicalPhoneDigits` for raw digit strings).
5. In `core/pkg/identity/graph.go`: update `ResolveOrLinkChannel` signature to take `accountID`, use `NormalizePhone(hints.Phone)`, and update all internal callers in `core/` (including `conduitd/main.go` and existing tests).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test -count=1 ./pkg/domain/... ./pkg/identity/... ./pkg/storage/...`
Expected: PASS (all new and existing tests pass).

- [ ] **Step 5: Commit**

```bash
git add core/pkg/domain/ core/pkg/identity/ core/pkg/storage/ core/cmd/conduitd/
git commit -m "feat(identity,storage): support multi-account channels, Telegram phone normalization, and non-person cleanup"
```

---

### Task 3: Core RPC & Daemon Wiring (`SyncContacts`, `StartChat`, `ContactUpdatedEvent`, 1:1 Guard)

**Files:**
- Modify: `core/pkg/eventbus/bus.go` (payload type for `EventContactUpdated`)
- Modify: `core/pkg/rpc/server.go:650-764`
- Modify: `core/cmd/conduitd/main.go:139-215,439-464`
- Test: `core/pkg/rpc/contacts_sync_test.go`, `core/cmd/conduitd/contacts_test.go`

**Interfaces:**
- Consumes: `identity.Graph.ResolveOrLinkChannel`, `storage.DB.EnsureConversation`, `storage.DB.GetChannelByID`, `conduitv1.PluginServiceClient.SyncContacts`.
- Produces:
  - `rpc.Server.SyncContacts(ctx, *conduitv1.SyncContactsRequest) (*conduitv1.SyncContactsResponse, error)`
  - `rpc.Server.StartChat(ctx, *conduitv1.StartChatRequest) (*conduitv1.StartChatResponse, error)`
  - Live `EventEnvelope{Payload: &EventEnvelope_ContactUpdated{...}}` streamed on `StreamEvents`.

- [ ] **Step 1: Write failing tests for `SyncContacts`, `StartChat`, unimplemented plugin skip, and group exclusion**

In `core/pkg/rpc/contacts_sync_test.go`:
```go
func TestSyncContactsSkipsUnimplementedPlugin(t *testing.T) {
    // Mock 2 plugins: WhatsApp returning 2 contacts, Discord returning codes.Unimplemented
    // Call Server.SyncContacts -> assert err == nil, synced_count == 2
}

func TestSyncContactsExcludesGroupsAndTopics(t *testing.T) {
    // Mock plugin returning 1 person + 1 "@g.us" + 1 "c123_topic_4"
    // Assert only 1 contact is saved in DB.
}

func TestStartChatRejectsForeignChannel(t *testing.T) {
    // Create contactA + channelA, contactB + channelB
    // Call StartChat(contactA.ID, channelB.ID) -> assert status.Code(err) == codes.InvalidArgument
}

func TestStartChatCreatesConversationWithSelectedChannel(t *testing.T) {
    // Create contactA with channelA (no conversation yet)
    // Call StartChat(contactA.ID, channelA.ID) -> assert Conversation.Id == contactA.ID and ActiveChannelId == channelA.ID
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd core && go test ./pkg/rpc/... -run "TestSyncContacts|TestStartChat" -v`
Expected: FAIL (`SyncContacts` and `StartChat` not implemented on `Server`).

- [ ] **Step 3: Implement `SyncContacts`, `StartChat`, `EventContactUpdated` streaming, and `conduitd` 1:1 guard**

1. In `core/pkg/rpc/server.go`: implement `SyncContacts` (iterates connected plugin accounts/instances, skips `codes.Unimplemented`, filters out group/topic/broadcast `remote_id`s, calls `graph.ResolveOrLinkChannel`, publishes `EventContactUpdated`, returns counts) and `StartChat` (validates `channel.ContactID == req.ContactId`, calls `EnsureConversation` with `active_channel_id = req.ChannelId`, returns `ProtoConversation`).
2. Wire `EventContactUpdated` in `core/pkg/rpc/server.go` (`StreamEvents` switch) and publish it from `conduitd/main.go` on `"contact.sync"` / new 1:1 contact creation.
3. In `core/cmd/conduitd/main.go`: ensure `"contact.sync"` and message handlers pass `account_id` and never insert group/topic/broadcast targets into `contacts`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd core && go test -count=1 ./...`
Expected: PASS across all `core` packages.

- [ ] **Step 5: Commit**

```bash
git add core/pkg/eventbus/ core/pkg/rpc/ core/cmd/conduitd/
git commit -m "feat(core): implement SyncContacts, StartChat, live contact events, and 1:1 guard"
```

---

### Task 4: Provider Plugins (`SyncContacts` in WhatsApp & Telegram)

**Files:**
- Modify: `dist-public/conduit-plugin-whatsapp/go.mod`, `dist-public/conduit-plugin-whatsapp/plugin.go`
- Modify: `dist-public/conduit-plugin-telegram/go.mod`, `dist-public/conduit-plugin-telegram/plugin.go`, `dist-public/conduit-plugin-telegram/sync.go`
- Rebuild binaries: `plugins/whatsapp/conduit-plugin-whatsapp`, `plugins/telegram/conduit-plugin-telegram`
- Test: `dist-public/conduit-plugin-whatsapp/plugin_test.go`, `dist-public/conduit-plugin-telegram/sync_test.go`

**Interfaces:**
- Consumes: `conduitv1.PluginServiceServer.SyncContacts`.
- Produces: `PluginSyncContactsResponse` containing strictly 1:1 person `PluginContact`s with `account_id`, `remote_id`, `display_name`, `avatar_url`, `phone`, `handle`.

- [ ] **Step 1: Write failing tests in WhatsApp & Telegram plugins**

In `dist-public/conduit-plugin-whatsapp/plugin_test.go`:
- `TestSyncContactsExcludesGroupsAndResolvesLID`: verify `@g.us`, `@broadcast`, `status@broadcast` are filtered out and `@lid` resolves to canonical `@s.whatsapp.net` with E.164 `phone`.

In `dist-public/conduit-plugin-telegram/sync_test.go`:
- `TestSyncContactsExcludesBotsChannelsAndIncludesPhone`: verify bots/channels/groups are excluded and `user.Phone` is populated on `PluginContact.Phone`.

- [ ] **Step 2: Run plugin tests to verify they fail**

Run: `cd dist-public/conduit-plugin-whatsapp && go test ./... -run TestSyncContactsExcludesGroupsAndResolvesLID` and `cd dist-public/conduit-plugin-telegram && go test ./... -run TestSyncContactsExcludesBotsChannelsAndIncludesPhone`
Expected: FAIL (`SyncContacts` not implemented).

- [ ] **Step 3: Point plugin `go.mod` to local `sdk` (temporary `replace`), implement `SyncContacts`, and rebuild binaries**

1. Add `replace github.com/cestila/ConduitOmniChat/sdk => ../../sdk` in both `dist-public/conduit-plugin-{whatsapp,telegram}/go.mod`.
2. Implement `SyncContacts(ctx, req) (*conduitv1.PluginSyncContactsResponse, error)` in both plugins (WhatsApp: `Store.Contacts.GetAllContacts` + `resolveCanonicalJID`; Telegram: `contacts.getContacts` + cached users).
3. Rebuild `plugins/whatsapp/conduit-plugin-whatsapp` and `plugins/telegram/conduit-plugin-telegram`.

- [ ] **Step 4: Run plugin tests to verify they pass**

Run: `cd dist-public/conduit-plugin-whatsapp && go test -count=1 ./...` and `cd dist-public/conduit-plugin-telegram && go test -count=1 ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add plugins/whatsapp/conduit-plugin-whatsapp plugins/telegram/conduit-plugin-telegram
git commit -m "feat(plugins): rebuild whatsapp and telegram binaries with SyncContacts RPC"
```
*(Note: `dist-public/conduit-plugin-whatsapp` and `dist-public/conduit-plugin-telegram` are separate Git repos; commit inside them separately if desired, keeping local `replace` out of tagged releases.)*

---

### Task 5: GUI Contacts Redesign (Clean List, Sync Button, Provider/Account Chat Initiation, Remove Manual Merge/Split)

**Files:**
- Modify: `gui/conduit-client.js:287-340`
- Modify: `gui/main.js:360-386`
- Modify: `gui/preload.js:34-38`
- Modify: `gui/src/index.html` (contacts view section)
- Modify: `gui/src/styles.css` (contact list & provider/account chips)
- Modify: `gui/src/app.js:2590-2820` (and stream event handler)
- Test: `gui/test/identity.test.js`

**Interfaces:**
- Consumes: `window.conduit.syncContacts`, `window.conduit.startChat`, `window.conduit.listContacts`, `contact_updated` stream event.
- Produces: Searchable contact list with "Sync Contacts" button and per-channel provider/account chips that call `startChat(contactId, channelId)` and open the conversation with `channelId` selected in the composer.

- [ ] **Step 1: Write failing GUI tests for clean contact list, `syncContacts`, `startChat`, and removal of manual merge/split**

In `gui/test/identity.test.js`:
- Assert manual merge/split/edit-identity UI elements and functions (`populateMergeSelects`, `searchContactsByIdentity`, `mergeIdentityMatch`, `splitContactChannel`, `editContactIdentity`) are removed.
- Assert `renderContactsList` renders each contact's linked channels as clickable provider/account chips (showing account label when >1 account of the same provider exists).
- Assert clicking a provider chip calls `startChat(contactId, channelId)`, switches to the chat view, and selects `channelId` in the composer.
- Assert clicking "Sync Contacts" calls `syncContacts` and refreshes `state.contacts`.

- [ ] **Step 2: Run GUI tests to verify they fail**

Run: `cd gui && npm test`
Expected: FAIL on the new `identity.test.js` assertions.

- [ ] **Step 3: Implement IPC bridge + clean Contacts UI**

1. Update `gui/conduit-client.js`, `gui/main.js`, `gui/preload.js`: expose `syncContacts` and `startChat`, forward `contact_updated` events, remove unused manual merge/split/identity IPCs.
2. Update `gui/src/index.html` & `gui/src/styles.css`: remove merge/identity panels; add Contacts header (search input + "Sync Contacts" button) and clean contact list container.
3. Update `gui/src/app.js`: replace `renderContactsGrid` & manual merge/split handlers with `renderContactsList`, `syncAllContacts`, `startChatWithChannel(contactId, channelId)`, and `contact_updated` listener.

- [ ] **Step 4: Run all GUI tests to verify they pass**

Run: `cd gui && npm test`
Expected: PASS (100% of test suites pass).

- [ ] **Step 5: Commit**

```bash
git add gui/
git commit -m "feat(gui): replace manual contact merge UI with clean synced contact list and per-account chat launch"
```
