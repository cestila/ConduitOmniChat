// Prueba los eventos de stream `conversation.deleted`: un tema de foro eliminado
// en el proveedor debe desaparecer de la GUI sin reiniciar.
// Ejecutar con: node test/conversation-deleted.test.js

const { createHarness, createChecker } = require('./dom-harness');

const SERVER_ID = '-1001987654321';
const TOPIC_1 = 'telegram:-1001987654321_topic_1';
const TOPIC_2 = 'telegram:-1001987654321_topic_2';
const TOPIC_3 = 'telegram:-1001987654321_topic_3';
const DM_ID = 'whatsapp:wa-1';

const conversations = [
  {
    id: TOPIC_1,
    title: 'General',
    server_id: SERVER_ID,
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: SERVER_ID,
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1500,
    last_message_preview: 'Bienvenidos a la comunidad'
  },
  {
    id: TOPIC_2,
    title: 'Ofertas Laborales',
    server_id: SERVER_ID,
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: SERVER_ID,
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1400,
    last_message_preview: 'Búsqueda Ssr Backend Go'
  },
  {
    id: TOPIC_3,
    title: 'Dudas y Consultas',
    server_id: SERVER_ID,
    server_name: 'Comunidad Golang Argentina',
    parent_channel_id: SERVER_ID,
    channel_type: 'forum',
    is_group: true,
    last_message_at_unix: 1300,
    last_message_preview: '¿Cómo compilan con CGO_ENABLED=0?'
  },
  {
    id: DM_ID,
    title: 'Lucas',
    is_group: false,
    last_message_at_unix: 1200,
    last_message_preview: '¿Nos vemos mañana?'
  }
];

const messagesByConv = {
  [TOPIC_2]: [
    { id: 'm-t2', conversation_id: TOPIC_2, sender_type: 'INCOMING', body: 'Oferta obsoleta del tema borrado', sent_at_unix: 1400 }
  ],
  [TOPIC_3]: [
    { id: 'm-t3', conversation_id: TOPIC_3, sender_type: 'INCOMING', body: 'Mensaje vivo del tema tres', sent_at_unix: 1300 }
  ],
  [DM_ID]: [
    { id: 'm-dm', conversation_id: DM_ID, sender_type: 'INCOMING', body: 'Mensaje vivo del chat directo', sent_at_unix: 1200 }
  ]
};

const deletedEnvelope = (conversationId, serverId = SERVER_ID) => ({
  type: 'conversation.deleted',
  channel_status_update: JSON.stringify({ conversation_id: conversationId, server_id: serverId })
});

const wait = ms => new Promise(r => setTimeout(r, ms));

(async () => {
  const { check, finish } = createChecker();
  const app = createHarness({ conversations, messages: messagesByConv });
  await app.start();

  const state = app.state;
  const el = app.el;
  const convIds = () => state.conversations.map(c => c.id);
  const rows = () => el.conversationList.children;
  const channelRows = () => rows().filter(n => (n.className || '').includes('conversation-item') && !(n.className || '').includes('is-group-parent'));
  const rowIds = () => channelRows().map(n => n.dataset && n.dataset.convId);

  // Preparación: entrar al supergrupo y abrir el tema que luego se borra.
  app.openServer(SERVER_ID);
  await wait(20);
  check('la vista del supergrupo muestra los 3 temas antes del borrado', channelRows().length === 3, `obtuvo ${channelRows().length}`);

  await app.selectConversation(TOPIC_2);
  check('el tema 2 queda activo para la prueba de borrado', state.activeConversationId === TOPIC_2, state.activeConversationId);
  check('el panel muestra los mensajes del tema 2 antes del borrado', el.messagesContainer.innerHTML.includes('Oferta obsoleta del tema borrado'));

  // (a) + (b) El tema activo se elimina en el proveedor.
  app.emitEvent(deletedEnvelope(TOPIC_2));
  await wait(20);

  check('(a) la conversación borrada sale del estado en memoria', !convIds().includes(TOPIC_2), convIds().join(','));
  check('(a) las otras conversaciones siguen en memoria', convIds().length === 3, `obtuvo ${convIds().length}`);
  check('(a) la lista se re-renderiza sin la fila borrada', !rowIds().includes(TOPIC_2), rowIds().join(','));
  check('(a) quedan las filas de los otros dos temas', rowIds().length === 2, `obtuvo ${rowIds().length}`);
  check('(a) el tema 3 sigue listado', rowIds().includes(TOPIC_3), rowIds().join(','));
  check('(a) el tema 1 sigue listado', rowIds().includes(TOPIC_1), rowIds().join(','));
  check('(b) el chat activo se limpia al borrarse', state.activeConversationId === null, String(state.activeConversationId));
  check('(b) el panel muestra un estado vacío', el.messagesContainer.innerHTML.includes('empty-conversation-state'), el.messagesContainer.innerHTML);
  check('(b) el panel no conserva los mensajes obsoletos', !el.messagesContainer.innerHTML.includes('Oferta obsoleta del tema borrado'), el.messagesContainer.innerHTML);
  check('(b) no queda ninguna burbuja de mensaje en el panel', el.messagesContainer.children.length === 0, `obtuvo ${el.messagesContainer.children.length}`);
  check('(b) el título del chat vuelve al estado inicial', el.activeChatTitle.textContent === 'Selecciona un chat', el.activeChatTitle.textContent);

  // (b) Una selección posterior sigue funcionando.
  await app.selectConversation(TOPIC_3);
  check('(b) se puede seleccionar otro tema después del borrado', state.activeConversationId === TOPIC_3, String(state.activeConversationId));
  check('(b) el tema seleccionado después muestra sus mensajes', el.messagesContainer.innerHTML.includes('Mensaje vivo del tema tres'), el.messagesContainer.innerHTML);

  // (d) Un id desconocido/no anunciado no toca nada.
  app.emitEvent(deletedEnvelope('telegram:-1001987654321_topic_9'));
  await wait(20);
  check('(d) un id no anunciado no borra conversaciones', convIds().length === 3, `obtuvo ${convIds().length}`);
  check('(d) un id no anunciado no cambia el chat activo', state.activeConversationId === TOPIC_3, String(state.activeConversationId));
  check('(d) un id no anunciado no limpia el panel de mensajes', el.messagesContainer.innerHTML.includes('Mensaje vivo del tema tres'), el.messagesContainer.innerHTML);
  check('(d) el chat directo sigue en memoria', convIds().includes(DM_ID), convIds().join(','));

  // (c) Payloads inválidos: se ignoran sin lanzar excepción ni tocar el estado.
  const malformed = [
    undefined,
    '',
    'no-es-json',
    '{}',
    'null',
    '[]',
    JSON.stringify({ conversation_id: 42 }),
    JSON.stringify({ conversation_id: '' }),
    JSON.stringify({ server_id: SERVER_ID })
  ];
  let threw = null;
  for (const raw of malformed) {
    try {
      app.emitEvent({ type: 'conversation.deleted', channel_status_update: raw });
    } catch (err) {
      threw = err;
      break;
    }
    try {
      app.emitEvent({ type: 'conversation.deleted' });
    } catch (err) {
      threw = err;
      break;
    }
  }
  await wait(20);
  check('(c) los payloads inválidos no lanzan excepción', threw === null, threw && threw.message);
  check('(c) los payloads inválidos no borran conversaciones', convIds().length === 3, `obtuvo ${convIds().length}`);
  check('(c) los payloads inválidos no cambian el chat activo', state.activeConversationId === TOPIC_3, String(state.activeConversationId));
  check('(c) los payloads inválidos no limpian el panel de mensajes', el.messagesContainer.innerHTML.includes('Mensaje vivo del tema tres'));

  // (e) Los eventos normales de estado de canal siguen aplicándose.
  const banner = app.documentStub.getElementById('connection-status-banner');
  app.emitEvent({ type: 'channel.connected', channel_status_update: 'telegram:disconnected:eof' });
  check('(e) una desconexión de canal sigue mostrando el banner', Boolean(banner && !banner.classList.contains('hidden')));
  check('(e) el banner sigue mostrando el motivo', app.documentStub.getElementById('connection-status-text').textContent.includes('telegram desconectado'));

  app.emitEvent({ type: 'channel.connected', channel_status_update: 'telegram:connected' });
  check('(e) una conexión de canal sigue ocultando el banner', Boolean(banner && banner.classList.contains('hidden')));
  check('(e) el estado de canal no borra conversaciones', convIds().length === 3, `obtuvo ${convIds().length}`);

  finish();
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
