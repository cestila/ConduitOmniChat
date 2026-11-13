// Prueba estructural de la navegación drill-down del sidebar (servidor -> canales -> volver).
// Ejecutar con: node test/navigation.test.js   (o npm run test:navigation)

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  // Servidor Discord con tres categorías
  { id: 'discord:chan-general-101', title: '#general', server_id: 'guild-7788990011', server_name: 'Conduit HQ (Discord)', parent_channel_id: 'cat-text-channels', channel_type: 'text', is_group: true, last_message_at_unix: 1000, unread_count: 2, last_message_preview: 'hola' },
  { id: 'discord:chan-voice-103', title: '🔊 Sala de Reuniones', server_id: 'guild-7788990011', server_name: 'Conduit HQ (Discord)', parent_channel_id: 'cat-voice', channel_type: 'voice', is_group: true, last_message_at_unix: 900, unread_count: 0, last_message_preview: '' },
  { id: 'discord:chan-dev-102', title: '#dev-announcements', server_id: 'guild-7788990011', server_name: 'Conduit HQ (Discord)', parent_channel_id: 'cat-dev', channel_type: 'text', is_group: true, last_message_at_unix: 950, unread_count: 0, last_message_preview: 'PR' },
  // Supergrupo Telegram: parent_channel_id idéntico al servidor (sin categoría propia)
  { id: 'telegram:-1001234567890_topic_1', title: '💬 Debate General', server_id: '-1001234567890', server_name: 'Comunidad Telegram Devs', parent_channel_id: '-1001234567890', channel_type: 'forum', is_group: true, last_message_at_unix: 800, unread_count: 0, last_message_preview: 'tema' },
  // Chat directo WhatsApp, sin servidor
  { id: 'contact-uuid-1', title: 'Juan Pérez', is_group: false, last_message_at_unix: 700, unread_count: 0, last_message_preview: 'ok' },
];

(async () => {
  const app = createHarness({ conversations });
  await app.start();

  const { check, finish } = createChecker();
  const state = app.state;
  const el = app.el;

  const isServerRow = node => (node.className || '').includes('is-group-parent');
  const isChannelRow = node => (node.className || '').includes('conversation-item');
  const isCategoryHeader = node => (node.className || '') === 'category-header';

  // 1. Lista unificada
  const root = el.conversationList.children;
  check('lista raíz tiene 3 filas (1 servidor + 2 chats directos)', root.length === 3, `obtuvo ${root.length}`);
  check('solo el servidor con varios canales es contenedor', root.filter(isServerRow).length === 1, `obtuvo ${root.filter(isServerRow).length}`);
  check('cabecera de servidor oculta en la lista raíz', el.serverNavHeader.classList.contains('hidden'));
  check('filtros visibles en la lista raíz', !el.chatFiltersContainer.classList.contains('hidden'));

  // 2. Entrar al servidor de Discord
  app.openServer('guild-7788990011');
  await new Promise(r => setTimeout(r, 20));
  check('cabecera de servidor visible tras entrar', !el.serverNavHeader.classList.contains('hidden'));
  check('cabecera muestra el nombre del servidor', el.serverNavTitle.textContent === 'Conduit HQ (Discord)', el.serverNavTitle.textContent);
  check('filtros ocultos dentro del servidor', el.chatFiltersContainer.classList.contains('hidden'));

  const inner = el.conversationList.children;
  check('3 canales listados dentro del servidor', inner.filter(isChannelRow).length === 3, `obtuvo ${inner.filter(isChannelRow).length}`);
  check('3 cabeceras de categoría (dev, text, voice)', inner.filter(isCategoryHeader).length === 3, `obtuvo ${inner.filter(isCategoryHeader).length}`);
  const firstHeader = inner.find(isCategoryHeader);
  // Sin posición publicada por el servidor (fixtures sin `position`) el orden cae al alfabético.
  check('sin orden del servidor, las categorías caen al alfabético: DEV primero', !!firstHeader && firstHeader.innerHTML.includes('DEV'), firstHeader && firstHeader.innerHTML);
  check('contador de canales en cabecera', el.serverNavCount.textContent === '3 canales', el.serverNavCount.textContent);

  // 3. Volver a la lista unificada
  app.closeServer();
  await new Promise(r => setTimeout(r, 20));
  check('volver restaura la lista raíz', el.conversationList.children.length === 3, `obtuvo ${el.conversationList.children.length}`);
  check('cabecera oculta tras volver', el.serverNavHeader.classList.contains('hidden'));
  check('estado de servidor limpio', state.activeServerId === null, String(state.activeServerId));

  // 4. Grupo de Telegram con un único canal: se muestra como chat directo, sin segunda ventana
  app.openServer('-1001234567890');
  await new Promise(r => setTimeout(r, 20));
  check('grupo de un solo canal no entra al drill-down', state.activeServerId === null, String(state.activeServerId));
  check('grupo de un solo canal queda en la lista raíz como chat', el.conversationList.children.filter(n => isChannelRow(n) && !isServerRow(n)).length === 2, `obtuvo ${el.conversationList.children.filter(n => isChannelRow(n) && !isServerRow(n)).length}`);
  check('sin cabecera de categoría para el grupo suelto', el.conversationList.children.filter(isCategoryHeader).length === 0);

  // 5. Servidor inexistente no rompe la navegación
  app.closeServer();
  app.openServer('servidor-que-no-existe');
  await new Promise(r => setTimeout(r, 20));
  check('servidor inexistente devuelve a la lista raíz', state.activeServerId === null && el.conversationList.children.length > 0);

  finish();
})();
