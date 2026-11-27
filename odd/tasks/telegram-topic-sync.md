# Feature: Telegram topic continuous sync & cleanup

Branch: `fix/telegram-topic-sync` (in both repos)
Scope: `conduit-plugin-telegram/` (separate git repo) and `ConduitOmniChat/core` + `gui` (this repo)

## Specs

- **S1**: "no hay una sincronizacion continua de los topicos de telegram, se borran en telegram pero en conduit no se sincronizan"
  - The Telegram plugin keeps topics in sync after connect, without restart: it reacts to forum topic updates (`UpdateChannel`, `UpdateChannelPinnedTopic(s)`, topic create/edit service messages) and also re-syncs every connected forum periodically. After each sync it publishes the live topic list of that forum as a `channel.reconcile` event (`ServerId` = forum remote id, `Body` = newline-separated live topic remote ids, always including General `_topic_1`). It never emits it on an API error or an empty list. A deleted topic must also stop resolving in `lookupPeer` and in catch-up.
- **S2**: "se borran" (answer to: deleted topics are deleted or archived)
  - On `channel.reconcile` the core deletes every conversation of that server whose remote id contains `_topic_` and is absent from the list, together with its messages, attachments and channel rows, and tells the GUI to refresh. It never touches DMs, other servers, or an empty/invalid list.
- **S3**: "se estan creando topicos nuevos con el nombre del grupo"
  - In a forum, a message whose topic cannot be resolved goes to General (topic 1), never to a channel named after the group. The forum parent is not published as a channel when topic sync fails or returns nothing. `GetDialogs` is paginated so forums beyond the first 100 dialogs get their topics. Existing group-named forum conversations are re-homed into General (messages moved) and removed.

## Tasks

- [x] **T1** (S3): Plugin: unresolved forum messages go to General; no parent `channel.sync` for forums; paginate `GetDialogs` | Route: writer (plugin repo) | Risk: none | Commit: `55e0879` (plugin repo; 423 lines, slightly over the 400-line slice; go test/vet clean, RED observed)
- [ ] **T2** (S2): Core: handle `channel.reconcile`, delete vanished topic conversations + messages, notify GUI | Route: writer (core) | Risk: item 1 (deletes stored data) | Commit: pending
- [ ] **T3** (S1): Plugin: topic update handlers + periodic resync + emit `channel.reconcile`; deleted topics stop resolving | Route: writer (plugin repo) | Risk: none | Commit: pending
- [ ] **T4** (S3): Core: re-home messages of group-named forum conversations into General and remove them | Route: writer (core) | Risk: item 1 (moves/deletes stored data) | Commit: pending
- [ ] **T5** (S2): GUI: refresh/navigate away when a topic conversation is removed | Route: writer (gui) | Risk: none | Commit: pending

Order: T1 -> T2 -> T3 -> T4 -> T5 (T2 defines the contract T3 emits; T2 and T4 each get their own commit for native review).

## Log

- **L1**: User request:
  > me esta pasando como que no hay una sincronizacion continua de los topicos de telegram, se borran en telegram pero en conduit no se sincronizan, ademas se estan creando topicos nuevos con el nombre del grupo
- **L2**: User answer: "se borran, armame el plan y activa rdd para esta session o repocitorio"
- **L3**: RDD enabled with `--scope global` by the user's choice ("Sí, activar global"); a clone-local setting can only disable it.
