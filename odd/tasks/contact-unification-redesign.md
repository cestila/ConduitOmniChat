# OmniChat — Contact Store & Automatic Unification Redesign

Branch: `feat/contact-unification-redesign`. Spec: `docs/superpowers/specs/2026-11-17-contact-unification-redesign-design.md`. Plan: `docs/superpowers/plans/2026-11-17-contact-unification-redesign.md`.

## Specs

**S1 — Common 1:1 Contact Extraction (`SyncContacts`)**
> "deberiamos extraer de todos los proveedores los contactos que tienen y guardarlo en una base en comun"
> "si tiene que ser solo contacto dejando de lado grupos, topicos, etc... solo contacto y tenlo en cuenta para que se integre con los plugins para que otro proveedor pueda hacer uso de este feature"

**S2 — Automatic Phone Unification & Multi-Account Support**
> "luego diseñamos la unificacion de contacto para que no se repitan lo puede hace por numero telefonico"
> "si si todas las cuentas de provedores distintos o mismos proveedores para todas las cuentas"

**S3 — Clean Contact List UI & Provider-Selected Chat Launch**
> "quiero mejorar la interface que solo funcione como una lista de contacto y si esta en mas de un proveedor y lo puede unir que los una y sea una forma facil de iniciar un chat con algun contacto, quita todo lo que existia de union manual y todo eso"
> "la idea es elegir la cuenta en el contacto elegir con que proveedor queire iniciar la conversacion"

## Tasks

- [x] **T1** (S1, S2, S3) | Route: delegated (subagent-driven) | Protobuf Contract (`SyncContacts`, `StartChat`, `account_id`, `ContactUpdatedEvent`) | Commit: `a881b66`
- [x] **T2** (S1, S2) | Route: delegated (subagent-driven) | Phone Normalization & Multi-Account Identity Graph | Commit: `aa1606b`
- [x] **T3** (S1, S2, S3) | Route: delegated (subagent-driven) | Core RPC & Daemon Wiring | Commit: `2771ec4`
- [x] **T4** (S1, S2) | Route: delegated (subagent-driven) | Provider Plugins (`SyncContacts` in WhatsApp & Telegram) | Commit: `dist-public` working trees + rebuilt `plugins/` binaries (gitignored)
- [x] **T5** (S3) | Route: delegated (subagent-driven) | GUI Contacts Redesign | Commit: `cdeef07`, `6ff076a`

## Log

- **L1** — Initial user request: "ok quiero que trabajemos en los contactos y reformulemos todo lo de contacto nuevamente, deberiamos extraer de todos los proveedores los contactos que tienen y guardarlo en una base en comun y luego diseñamos la unificacion de contacto para que no se repitan lo puede hace por numero telefonico"
- **L2** — Clarifications: remove all manual merge/split UI, choose provider/account directly on contact to start chat, 1:1 people only (no groups/topics), support all accounts (both different providers and multiple accounts of same provider), explicit `SyncContacts` contract for plugins.
- **L3** — Design & plan approved (`docs/superpowers/specs/2026-11-17-contact-unification-redesign-design.md`, `docs/superpowers/plans/2026-11-17-contact-unification-redesign.md`). Execution mode: `subagent-driven`.
- **L4** — Final whole-branch review & fix wave completed (`a9ee0f5`, `69ed06c`). `Risk: item 1 (contact_channels migration + non-person cleanup), item 3 (additive SyncContacts/StartChat/account_id proto), item 4 (Graph mutex + async channel.ready sync)`.
