// Prueba estructural y de UX para emojis y reacciones en la GUI:
// - Detección de mensajes con solo emojis (1 a 3 emojis) aplicando clase 'emoji-jumbo'
// - No aplicar 'emoji-jumbo' a mensajes con texto mezclado o texto estándar
// - Renderizado del popup de reacciones rápidas (.quick-reaction-popup) con emojis aumentados y 🔥
// - Estructura de píldoras de reacciones (.reaction-pill) con .reaction-emoji y .reaction-count
// - Toggle de reacciones desde la GUI
// Ejecutar con: node test/emoji-reactions-ux.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-emoji-ux',
    display_name: 'Matias',
    phone: '+5491199887766',
    channels: [
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: 'u12345', handle: '@matias' },
    ],
  },
];

const conversations = [
  {
    id: 'conv-emoji-ux',
    contact_id: 'contact-emoji-ux',
    title: 'Matias',
    active_channel_id: 'ch-tg',
    is_group: false,
  },
];

(async () => {
  const app = createHarness({ contacts, conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // 1. Verificar la función pura isOnlyEmojis
  const isOnlyEmojis = app.isOnlyEmojis;
  check('isOnlyEmojis está exportada', typeof isOnlyEmojis === 'function');
  check('isOnlyEmojis detecta emoji único 👍', isOnlyEmojis('👍') === true);
  check('isOnlyEmojis detecta emoji con corazón ❤️', isOnlyEmojis('❤️') === true);
  check('isOnlyEmojis detecta dos emojis 😂🔥', isOnlyEmojis('😂🔥') === true);
  check('isOnlyEmojis detecta tres emojis 👍❤️🎉', isOnlyEmojis('👍❤️🎉') === true);
  check('isOnlyEmojis rechaza más de tres emojis 👍❤️🎉🔥', isOnlyEmojis('👍❤️🎉🔥') === false);
  check('isOnlyEmojis rechaza texto con emoji "Hola 👍"', isOnlyEmojis('Hola 👍') === false);
  check('isOnlyEmojis rechaza texto simple "Hola"', isOnlyEmojis('Hola') === false);
  check('isOnlyEmojis rechaza números "1"', isOnlyEmojis('1') === false);
  check('isOnlyEmojis rechaza string vacío o nulo', isOnlyEmojis('') === false && isOnlyEmojis(null) === false);

  // Abrir la conversación
  await app.sandbox.selectConversation('conv-emoji-ux');

  // 2. Renderizar mensajes normales y con emojis jumbo
  const messages = [
    {
      id: 'msg-text',
      conversation_id: 'conv-emoji-ux',
      sender_type: 'contact',
      body: 'Che, ¿cómo viene el proyecto?',
      sent_at_unix: 1700000000,
    },
    {
      id: 'msg-emoji-single',
      conversation_id: 'conv-emoji-ux',
      sender_type: 'me',
      body: '🚀',
      sent_at_unix: 1700000010,
    },
    {
      id: 'msg-emoji-double',
      conversation_id: 'conv-emoji-ux',
      sender_type: 'contact',
      body: '🔥👏',
      sent_at_unix: 1700000020,
      reactions: [
        { emoji: '❤️', count: 1, includes_me: true, senders: ['me'] },
        { emoji: '👍', count: 3, includes_me: false, senders: ['Matias', 'Lucas', 'Sofia'] },
      ],
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const msgRows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderizan 3 mensajes', msgRows.length === 3, `hay ${msgRows.length}`);

  // 3. Inspeccionar el mensaje de texto estándar (no debe tener emoji-jumbo)
  const rowTextHTML = msgRows[0] ? (msgRows[0].innerHTML || '') : '';
  check('mensaje de texto no contiene clase emoji-jumbo', !rowTextHTML.includes('emoji-jumbo'));
  check('mensaje de texto conserva msg-body estándar', rowTextHTML.includes('msg-body') && rowTextHTML.includes('Che, ¿cómo viene el proyecto?'));

  // 4. Inspeccionar el mensaje con un solo emoji (debe tener emoji-jumbo)
  const rowSingleHTML = msgRows[1] ? (msgRows[1].innerHTML || '') : '';
  check('mensaje de 1 emoji aplica clase emoji-jumbo', rowSingleHTML.includes('msg-body emoji-jumbo'));
  check('mensaje de 1 emoji contiene 🚀', rowSingleHTML.includes('🚀'));

  // 5. Inspeccionar el mensaje con 2 emojis (debe tener emoji-jumbo y reacciones)
  const rowDoubleHTML = msgRows[2] ? (msgRows[2].innerHTML || '') : '';
  check('mensaje de 2 emojis aplica clase emoji-jumbo', rowDoubleHTML.includes('msg-body emoji-jumbo'));
  check('mensaje contiene reactions container', rowDoubleHTML.includes('msg-reactions'));
  check('reacción ❤️ incluye active', rowDoubleHTML.includes('reaction-pill active'));
  check('reacción 👍 muestra contador 3', rowDoubleHTML.includes('reaction-count') && rowDoubleHTML.includes('3'));
  check('reacción contiene span con clase reaction-emoji', rowDoubleHTML.includes('reaction-emoji'));

  // 6. Verificar el popup de reacciones rápidas
  check('toolbar incluye quick-reaction-popup', rowSingleHTML.includes('quick-reaction-popup'));
  check('popup incluye emoji 👍', rowSingleHTML.includes('data-emoji="👍"'));
  check('popup incluye emoji ❤️', rowSingleHTML.includes('data-emoji="❤️"'));
  check('popup incluye emoji 🔥', rowSingleHTML.includes('data-emoji="🔥"'));

  // 7. Toggle de reacción
  await app.sandbox.toggleReaction('msg-emoji-single', '🎉');
  const lastReact = app.calls.sendReaction[app.calls.sendReaction.length - 1];
  check('toggleReaction envía la reacción solicitada', lastReact && lastReact[0] === 'msg-emoji-single' && lastReact[1] === '🎉');

  finish();
})();
