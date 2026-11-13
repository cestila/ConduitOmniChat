// Prueba estructural del orden de canales: se respeta el orden en que el servidor
// publica canales y categorías, no el alfabético ni el de actividad.
// Ejecutar con: node test/navigation-order.test.js   (o npm run test:order)

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  // El servidor publica primero la categoría ZETA y después ALPHA, aunque alfabéticamente
  // ALPHA iría antes. La actividad reciente tampoco debe alterar el orden.
  { id: 'discord:zeta-1', title: '#zeta-uno', server_id: 'guild-1', server_name: 'Srv', parent_channel_id: 'cat-zeta', channel_type: 'text', is_group: true, position: 1, last_message_at_unix: 900 },
  { id: 'discord:zeta-2', title: '#zeta-dos', server_id: 'guild-1', server_name: 'Srv', parent_channel_id: 'cat-zeta', channel_type: 'text', is_group: true, position: 2, last_message_at_unix: 100 },
  { id: 'discord:alpha-1', title: '#alpha-uno', server_id: 'guild-1', server_name: 'Srv', parent_channel_id: 'cat-alpha', channel_type: 'text', is_group: true, position: 3, last_message_at_unix: 5000 },
  { id: 'discord:alpha-2', title: '#alpha-dos', server_id: 'guild-1', server_name: 'Srv', parent_channel_id: 'cat-alpha', channel_type: 'text', is_group: true, position: 4, last_message_at_unix: 10 },
];

(async () => {
  const app = createHarness({ conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  app.openServer('guild-1');
  await new Promise(r => setTimeout(r, 20));

  const nodes = el.conversationList.children;
  const headers = nodes.filter(n => (n.className || '') === 'category-header');
  check('se muestran las 2 categorías', headers.length === 2, `obtuve ${headers.length}`);
  check('la categoría del primer canal del servidor va primero (ZETA antes que ALPHA)',
    !!headers[0] && /ZETA/.test(headers[0].innerHTML),
    headers.map(h => h.innerHTML).join(' | '));

  const channelTitles = nodes
    .filter(n => (n.className || '').includes('conversation-item'))
    .map(n => n.innerHTML);
  const order = ['#zeta-uno', '#zeta-dos', '#alpha-uno', '#alpha-dos'];
  let cursor = 0;
  let inOrder = true;
  channelTitles.forEach(html => {
    const idx = order.findIndex(title => html.includes(title));
    if (idx < cursor) inOrder = false;
    else cursor = idx;
  });
  check('los canales respetan el orden del servidor aunque otro tenga actividad más reciente',
    inOrder && channelTitles.length === 4,
    channelTitles.map(h => (h.match(/#[a-z-]+/) || ['?'])[0]).join(' '));

  finish();
})();
