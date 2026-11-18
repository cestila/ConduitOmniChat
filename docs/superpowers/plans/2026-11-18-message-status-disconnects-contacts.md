# Message Status, Disconnections & Legible Contacts Implementation Plan

> **For agentic workers:** Executing inline in Native mode (`superpowers:executing-plans`).

**Goal:** Surface real-time outgoing message statuses (`pending`/`sent`/`delivered`/`read`/`failed`), detect/surface plugin/account disconnections, prefer legible contact names over symbol-only PushNames, and allow manual contact renaming protected from resync overwrites.

**Spec:** [2026-11-18-message-status-disconnects-contacts-design.md](file:///home/cestila/workspace/Personales/ConduitOmniChat/docs/superpowers/specs/2026-11-18-message-status-disconnects-contacts-design.md)

## Tasks
- [ ] **Task 1 (Proto):** Add `MessageStatusEvent` (`message_status_updated = 9` in `EventEnvelope`), `RenameContact` RPC, `ProtoContact.name_locked = 8`, `ProtoConversation.contact_phone = 16`, and run `make proto`.
- [ ] **Task 2 (Storage & Identity):** Add `name_locked` column migration to `contacts`, `RenameContact`, `UpdateMessageStatusByRemoteID`, `UpdateAccountsStatusByProvider`, `IsLegibleName`, and `ResolveOrLinkChannel` update/protection logic + unit tests.
- [ ] **Task 3 (RPC & Daemon):** Subscribe `StreamEvents` to `EventMessageStatus` and `EventChannelConnected`, mark `StatusFailed` when plugin missing in `SendMessage`, implement `RenameContact` RPC, handle `channel.disconnected` + `StreamIncomingEvents` stream errors/EOF + outgoing status events in `conduitd/main.go` + unit tests.
- [ ] **Task 4 (GUI):** Expose `renameContact` in `conduit-client.js`/`main.js`/`preload.js`, render real status check icons + live `message_status_updated` + `channel_status_update` disconnection banner + non-legible phone fallback/search + inline contact rename in `gui/src/` + tests in `gui/test/`.
