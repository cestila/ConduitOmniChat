// Prueba del filtro por proveedor del sidebar: los chats directos se guardan
// con el id del contacto (UUID), así que el filtro no puede depender del prefijo
// del id o desaparecen de su pestaña.
// Ejecutar con: node test/filter-provider.test.js

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  { id: 'telegram:c1212630063', title: 'Lemon - Comunidad', provider: 'telegram', is_group: true, last_message_at_unix: 3000, unread_count: 0, last_message_preview: 'hola' },
  { id: '01476c7d-34b2-485a-b34f-1a5f6f990569', title: 'Fulano Ejemplo', provider: 'telegram', is_group: false, last_message_at_unix: 2000, unread_count: 1, last_message_preview: '¿todo bien?' },
  { id: '5491100000001@s.whatsapp.net', title: 'Fulano Prueba', provider: 'whatsapp', is_group: false, last_message_at_unix: 1000, unread_count: 0, last_message_preview: 'ok' },
  { id: 'discord:chan-general-101', title: '#general', provider: 'discord', server_id: 'guild-1', server_name: 'Conduit HQ', is_group: true, last_message_at_unix: 500, unread_count: 0, last_message_preview: '' },
];

(async () => {
  const app = createHarness({ conversations });
  await app.start();

  const { check, finish } = createChecker();
  const state = app.state;
  const el = app.el;

  const titles = () => el.conversationList.children.map(n => n.innerHTML);

  state.activeFilter = 'telegram';
  app.renderConversations();
  await new Promise(r => setTimeout(r, 10));
  const telegram = titles();
  check('el filtro Telegram muestra el grupo', telegram.some(h => h.includes('Lemon')), telegram.join(' | '));
  check('el filtro Telegram muestra el chat directo del contacto', telegram.some(h => h.includes('Fulano Ejemplo')), telegram.join(' | '));
  check('el chat directo muestra el badge de su proveedor', telegram.some(h => h.includes('Fulano Ejemplo') && h.includes('badge-telegram')), telegram.join(' | '));
  check('el filtro Telegram no muestra WhatsApp', !telegram.some(h => h.includes('Fulano Prueba')), telegram.join(' | '));
  check('el filtro Telegram no muestra Discord', !telegram.some(h => h.includes('general')), telegram.join(' | '));

  state.activeFilter = 'whatsapp';
  app.renderConversations();
  await new Promise(r => setTimeout(r, 10));
  const whatsapp = titles();
  check('el filtro WhatsApp muestra solo su contacto', whatsapp.length === 1 && whatsapp[0].includes('Fulano Prueba'), whatsapp.join(' | '));

  finish();
})();
