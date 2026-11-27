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
- [x] **T2** (S2): Core: handle `channel.reconcile`, delete vanished topic conversations + messages, notify GUI | Route: writer (core) | Risk: item 1 (deletes stored data) | Commit: `58dc054` (independent verifier found shared-channel-row defect, fixed; native review approved, lineage `review-c1b338bb3fbc66f4`; 572 lines across docs+feat commits, over the 400-line slice)
- [x] **T3** (S1): Plugin: topic update handlers + periodic resync + emit `channel.reconcile`; deleted topics stop resolving | Route: writer (plugin repo), finished inline after the writer hit a provider quota mid-refactor | Risk: none | Commit: `b9e8ce1` (progress: paginated complete lists with Count check, reconcile only on complete lists, `ForumTopicDeleted`/absent-topic forgetting, `OnChannel` + pinned-topic wrapper + service-message triggers, 3 s debounce, 5 min sweep; go test/vet clean; native review found 2 CRITICAL completeness findings, corrected within the 170-line plan (two-pass equality when deleted stubs exist, empty-but-ended list reconciles), re-validated, lineage `review-0dbe0d51519be5f2` approved)
- [x] **T4** (S3): Core: re-home messages of group-named forum conversations into General and remove them | Route: writer (core) | Risk: item 1 (moves/deletes stored data) | Commit: `c0b64b5` + `71cba10` (independent verifier: met, no blockers; native review approved, lineage `review-6f9f2a085cbf9f40`; its two advisories, General unreadable with unparseable conversation or message timestamps, fixed in the follow-up commit, assess: medium, no review due)
- [x] **T5** (S2): GUI: refresh/navigate away when a topic conversation is removed | Route: writer (gui) | Risk: none | Commit: `6046d8f` (npm test: 32/32 files pass; RED observed with 12 failures before the change)

- [x] **T6** (S2): Core hardening from review findings: snapshot freshness guard (`SentAtUnix` of `channel.reconcile` = time the live list was fetched; never delete a topic conversation with `last_message_at`/creation after it), non-ASCII plugin-name prefix match, dispatch test for the `channel.reconcile` case, StreamEvents `conversation.deleted` test, downloaded media files of deleted attachments | Route: writer (core) | Risk: item 1 (deletion rules) | Commit: `cb01d85` + `56364e7` (symlink-escape fix after independent verifier; native review approved, lineage `review-648e22bbd5f656c8`; loop `continue` finding checked: no code after the switch). Open non-blocking: stream test uses a 50 ms sleep (flaky risk), sub-second snapshot not tested, unreadable `last_message_at` retention not in CHANGELOG

- [x] **T7** (S2): Core: retry the reconcile transaction on SQLite BUSY/BUSY_SNAPSHOT (517), 5 attempts — found in the live run, `busy_timeout` does not cover the deferred-to-write upgrade | Risk: item 1 (deletion robustness) | Commit: `ee80d2c`
- [x] **T8** (S3): Core: also re-home a `group`-typed bare conversation when the server is provably a forum | Risk: item 1 | Commit: `5ad5b18`
- [ ] **T9** (S3): Cosmetic: repair `parent_channel_id` = `<server>_topic_<n>` contamination on topic conversations | Risk: none | Commit: pending

Order: T1 -> T2 -> T6 -> T3 -> T4 -> T5 (T2 defines the contract T3 emits; T2 and T4 each get their own commit for native review).

## Log

- **L1**: User request:
  > me esta pasando como que no hay una sincronizacion continua de los topicos de telegram, se borran en telegram pero en conduit no se sincronizan, ademas se estan creando topicos nuevos con el nombre del grupo
- **L2**: User answer: "se borran, armame el plan y activa rdd para esta session o repocitorio"
- **L7**: Live run on the installed data (backup `backup-prueba/conduit.db.20261127-160313`): 12 topics deleted for `c4308592267` (206 messages), 205 stranded messages re-homed to General (16 -> 221), `c1613217947` 392 messages re-homed (32 -> 424), totals 36726 -> 36552 messages; arithmetic closes exactly. Launcher on NixOS: `CONDUITD_PATH=../bin/conduitd nix run nixpkgs#electron -- .` (`npm start` fails: npm Electron needs system libglib).
- **L6**: Delivery shape: 9 commits on `fix/telegram-topic-sync` in both repos, well past the 400-line review slice; push, PR and merge remain the user's decision.
- **L5**: Deleted topics must be deleted and existing group-named forum conversations re-homed into General (T4/T5 added). Native review consent for the first T4 target was declined at the host.
- **L4**: Native review of T2 returned non-blocking findings R3-001..R3-006 and R4-reconcile-snapshot-delete; the actionable ones became T6.
- **L3**: RDD enabled with `--scope global` by the user's choice ("Sí, activar global"); a clone-local setting can only disable it.
