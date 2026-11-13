// Prueba estructural de citas (quotes/replies) y reacciones con emojis en la GUI:
// - Renderizado de mensajes que citan a otros (.msg-quoted-reply con remitente y texto)
// - Acción de responder en el compositor (activa preview y envía reply_to_message_id)
// - Cancelación de respuesta activa
// - Renderizado de píldoras de reacciones (.msg-reactions, .reaction-pill con active si incluye al usuario)
// - Clic en reacción dispara sendReaction
// Ejecutar con: node test/reactions-quotes.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-quotes',
    display_name: 'Valeria',
    phone: '+5491122334455',
    channels: [
      { id: 'ch-wa', provider: 'PROVIDER_WHATSAPP', remote_id: '+5491122334455', handle: '+5491122334455' },
    ],
  },
];

const conversations = [
  {
    id: 'contact-quotes',
    contact_id: 'contact-quotes',
    title: 'Valeria',
    active_channel_id: 'ch-wa',
    is_group: false,
  },
];

(async () => {
  const app = createHarness({ contacts, conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // Abrir la conversación
  await app.sandbox.selectConversation('contact-quotes');

  // 1. Renderizar mensajes con citas y reacciones
  const messages = [
    {
      id: 'msg-1',
      conversation_id: 'contact-quotes',
      sender_type: 'contact',
      body: '¿Nos vemos mañana?',
      sent_at_unix: 1700000000,
      reactions: [
        { emoji: '👍', count: 1, includes_me: false, senders: ['contact'] },
      ],
    },
    {
      id: 'msg-2',
      conversation_id: 'contact-quotes',
      sender_type: 'me',
      body: '¡Dale, me parece perfecto!',
      sent_at_unix: 1700000060,
      reply_to_message_id: 'msg-1',
      reply_to_sender: 'Valeria',
      reply_to_body: '¿Nos vemos mañana?',
      reactions: [
        { emoji: '❤️', count: 1, includes_me: true, senders: ['me'] },
        { emoji: '🔥', count: 2, includes_me: false, senders: ['Valeria', 'Amigo'] },
      ],
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const msgRows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderizan 2 mensajes', msgRows.length === 2, `hay ${msgRows.length} elementos`);

  // 2. Inspeccionar la cita en msg-2 (segunda fila de mensaje)
  const row2 = msgRows[1];
  const row2HTML = row2 ? row2.innerHTML || '' : '';
  check('msg-2 renderiza el bloque de cita', row2HTML.includes('msg-quoted-reply'));
  check('la cita muestra el remitente original', row2HTML.includes('Valeria'));
  check('la cita muestra el texto original', row2HTML.includes('¿Nos vemos mañana?'));

  // 3. Inspeccionar las reacciones en msg-2
  check('msg-2 renderiza contenedor de reacciones', row2HTML.includes('msg-reactions'));
  check('msg-2 tiene píldora para ❤️', row2HTML.includes('❤️'));
  check('la píldora ❤️ tiene clase active porque includes_me=true', row2HTML.includes('reaction-pill active'));
  check('msg-2 tiene píldora para 🔥', row2HTML.includes('🔥'));

  // 4. Activar respuesta a msg-1
  app.sandbox.setReplyTarget(messages[0]);

  const preview = el.composerReplyPreview;
  check('la barra de respuesta deja de estar oculta', preview && !preview.classList.contains('hidden'));
  check('muestra el remitente citado', el.replyPreviewSender && el.replyPreviewSender.textContent === 'Valeria');
  check('muestra el extracto del mensaje', el.replyPreviewText && el.replyPreviewText.textContent === '¿Nos vemos mañana?');

  // 5. Enviar mensaje con la respuesta activa
  el.messageInput.value = 'Confirmado entonces';
  await app.sandbox.handleSendMessage();

  const lastSend = app.calls.sendMessage[app.calls.sendMessage.length - 1];
  check('sendMessage recibe reply_to_message_id = msg-1', lastSend && lastSend[3] === 'msg-1');
  check('la barra de respuesta vuelve a ocultarse tras enviar', preview.classList.contains('hidden'));

  // 6. Probar cancelación de respuesta
  app.sandbox.setReplyTarget(messages[0]);
  check('la barra de respuesta se abre de nuevo', !preview.classList.contains('hidden'));
  app.sandbox.clearReply();
  check('clearReply oculta la barra de respuesta', preview.classList.contains('hidden'));

  // 7. Enviar reacción
  await app.sandbox.toggleReaction('msg-1', '🎉');
  const lastReact = app.calls.sendReaction[app.calls.sendReaction.length - 1];
  check('sendReaction fue invocado con msg-1 y 🎉', lastReact && lastReact[0] === 'msg-1' && lastReact[1] === '🎉');

  finish();
})();
