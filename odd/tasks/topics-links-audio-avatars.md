# Feature: Topics Purge, URL Previews, Audio Player & Profile Avatars

Branch: `fix/topics-links-audio-avatars`
Scope: `core/` (SQLite storage, sync, identity) and `gui/` (Electron main, renderer, styles, tests)

## Specs

- **S1**: "Suelen aparecer contactos que en realidad son topic o tema que se llaman por ejemplo Tema 780 se pueden purgar ya que deberian solo aparecer en su respectivo grupo/topic"
  - Purge existing misclassified `_topic_` contacts/channels from SQLite and prevent `contact.sync` / `message.received` from creating 1-on-1 contacts for `_topic_` IDs; ensure forum groups with 1+ topic channels render inside their group instead of standalone.
- **S2**: "Podemos identificar las url y generar el link y una vista previa."
  - Linkify URLs in message text (`<a>` opening safely via external browser) and fetch/cache OpenGraph metadata to render a link preview card below messages containing URLs.
- **S3**: "No me gusta como muestra los audios, se puede mejorar la vista de medios de audio"
  - Replace the native `<audio controls>` element in `.media-audio-card` with a custom, styled audio player (play/pause, interactive progress/waveform bar, elapsed/total duration, playback speed toggle).
- **S4**: "y en las cuenta de telelgram y whatsapp no veo las foto de perfil mias, y de algunos contactos"
  - Improve self-account avatar matching/resolution and ensure `avatar_url` updates when newly received or refreshed, with local avatar caching/proxy fallback in `core`/`gui` where needed.

## Tasks

- [x] **T1** (S1): Purge misclassified `_topic_` contacts & fix topic grouping | Route: inline (high-risk item 1: SQLite migration/purge) | Commit: `55c822d`
- [x] **T2** (S2): Linkify URLs and render OpenGraph link preview cards | Route: inline | Commit: `6f98cd2`
- [x] **T3** (S3): Custom audio media player UI | Route: inline | Commit: `9862ad4`
- [x] **T4** (S4): Fix self & contact profile avatars for Telegram & WhatsApp | Route: inline | Commit: `d314aa6`

## Log

- **L1**: User request:
  > necesito realizar algunos cambios de algunos errores,
  >
  >   Suelen aparecer contactos que en realidad son topic o tema que se llaman por ejemplo Tema 780 se pueden purgar ya que deberian solo aparecer en su respectivo grupo/topic
  >   Podemos identificar las url y generar el link y una vista previa.
  >   No me gusta como muestra los audios, se puede mejorar la vista de medios de audio
  >   y en las cuenta de telelgram y whatsapp no veo las foto de perfil mias, y de algunos contactos
- **L2**: User confirmed proceeding with all 4 tasks in order (`ok`).
