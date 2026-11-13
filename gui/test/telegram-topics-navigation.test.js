// Prueba de navegación de supergrupos de Telegram con múltiples temas de foro (drill-down).
// Ejecutar con: node test/telegram-topics-navigation.test.js

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  // Supergrupo de Telegram con 3 temas de foro
  {
    id: 'telegram:-1001987654321_topic_1',
    title: 'General',
    server_id: '-1001987654321',
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: '-1001987654321',
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1500,
    unread_count: 1,
    last_message_preview: 'Bienvenidos a la comunidad'
  },
  {
    id: 'telegram:-1001987654321_topic_2',
    title: 'Ofertas Laborales',
    server_id: '-1001987654321',
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: '-1001987654321',
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1400,
    unread_count: 0,
    last_message_preview: 'Búsqueda Ssr Backend Go'
  },
  {
    id: 'telegram:-1001987654321_topic_3',
    title: 'Dudas y Consultas',
    server_id: '-1001987654321',
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: '-1001987654321',
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1300,
    unread_count: 0,
    last_message_preview: '¿Cómo compilan con CGO_ENABLED=0?'
  },
  // Chat directo independiente sin servidor
  {
    id: 'whatsapp:wa-1',
    title: 'Lucas',
    is_group: false,
    last_message_at_unix: 1200,
    unread_count: 0,
    last_message_preview: '¿Nos vemos mañana?'
  }
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

  // 1. En la lista raíz debe aparecer el contenedor del supergrupo de Telegram
  const root = el.conversationList.children;
  check('la lista raíz tiene 2 filas (1 supergrupo Telegram + 1 chat directo)', root.length === 2, `obtuvo ${root.length}`);
  check('el supergrupo con múltiples temas se agrupa como servidor', root.filter(isServerRow).length === 1, `obtuvo ${root.filter(isServerRow).length}`);

  const tgServerRow = root.find(isServerRow);
  check('la fila del supergrupo muestra su nombre', tgServerRow && tgServerRow.innerHTML.includes('Comunidad Golang Argentina'));
  check('la fila del supergrupo muestra 3 canales', tgServerRow && tgServerRow.innerHTML.includes('3 canales'));

  // 2. Entrar al supergrupo de Telegram (drill-down de temas)
  app.openServer('-1001987654321');
  await new Promise(r => setTimeout(r, 20));

  check('cabecera de servidor visible tras entrar al supergrupo', !el.serverNavHeader.classList.contains('hidden'));
  check('cabecera muestra el nombre del supergrupo', el.serverNavTitle.textContent === 'Comunidad Golang Argentina', el.serverNavTitle.textContent);
  check('cabecera muestra el contador de 3 canales', el.serverNavCount.textContent === '3 canales', el.serverNavCount.textContent);

  const inner = el.conversationList.children;
  const channels = inner.filter(isChannelRow);
  check('se listan los 3 temas de foro dentro del supergrupo', channels.length === 3, `obtuvo ${channels.length}`);
  check('no hay cabeceras de categoría intermedias para temas sin categoría', inner.filter(isCategoryHeader).length === 0, `obtuvo ${inner.filter(isCategoryHeader).length}`);

  // Verificar que cada tema muestra el prefijo de foro '💬 '
  const htmls = channels.map(c => c.innerHTML || '');
  check('el primer tema es General con prefijo de foro', htmls[0].includes('💬') && htmls[0].includes('General'), htmls[0]);
  check('el segundo tema es Ofertas Laborales', htmls[1].includes('💬') && htmls[1].includes('Ofertas Laborales'), htmls[1]);
  check('el tercer tema es Dudas y Consultas', htmls[2].includes('💬') && htmls[2].includes('Dudas y Consultas'), htmls[2]);

  // 3. Volver a la lista unificada
  app.closeServer();
  await new Promise(r => setTimeout(r, 20));

  check('volver restaura la lista raíz con 2 elementos', el.conversationList.children.length === 2, `obtuvo ${el.conversationList.children.length}`);
  check('cabecera de navegación oculta tras volver', el.serverNavHeader.classList.contains('hidden'));
  check('activeServerId limpio tras volver', state.activeServerId === null);

  finish();
})();
