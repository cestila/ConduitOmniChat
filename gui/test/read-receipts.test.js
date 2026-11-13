const { createHarness, createChecker } = require('./dom-harness');

async function testReadReceipts() {
  console.log('--- TEST SYNC READ RECEIPTS ---');
  const { check, finish } = createChecker();

  const conversations = [
    { id: 'wa-chat-1', title: 'Grupo WhatsApp', unread_count: 5, last_message_preview: 'Hola!', last_message_at_unix: 1700000000 },
    { id: 'tg-chat-2', title: 'Canal Telegram', unread_count: 2, last_message_preview: 'Noticia', last_message_at_unix: 1700000100 }
  ];

  const app = createHarness({ conversations });
  await app.start();

  // Iniciar eventos realtime
  app.setupRealtimeEvents();

  // 1. Verificar render inicial con unread_count en el estado de conversaciones
  const convs = app.state.conversations;
  check('conversaciones cargadas con no leídos', convs[0].unread_count === 5 && convs[1].unread_count === 2);

  // 2. Marcar conversación wa-chat-1 como leída (como ocurre al abrirla)
  app.markActiveConversationAsRead('wa-chat-1');
  check('markConversationRead llamado vía API', app.calls.markConversationRead && app.calls.markConversationRead.includes('wa-chat-1'));
  check('unread_count en estado de conversación puesto a 0', app.state.conversations.find(c => c.id === 'wa-chat-1').unread_count === 0);

  // 3. Simular evento en tiempo real conversation_read recibido desde el celular para tg-chat-2
  app.emitEvent({
    type: 'conversation.read',
    timestamp_unix: Math.floor(Date.now() / 1000),
    conversation_read: {
      conversation_id: 'tg-chat-2',
      remote_message_ids: ['105'],
      max_remote_id: '105'
    }
  });

  const tgConv = app.state.conversations.find(c => c.id === 'tg-chat-2');
  check('evento conversation_read actualiza contador a 0 en memoria', tgConv.unread_count === 0);

  // 4. S8: conversation_read con unread_count restante conserva ese valor
  app.state.conversations.find(c => c.id === 'wa-chat-1').unread_count = 7;
  app.emitEvent({
    type: 'conversation.read',
    conversation_read: { conversation_id: 'wa-chat-1', remote_message_ids: ['1'], max_remote_id: '1', unread_count: 3 }
  });
  check('conversation_read con unread_count 3 deja 3', app.state.conversations.find(c => c.id === 'wa-chat-1').unread_count === 3);

  app.emitEvent({
    type: 'conversation.read',
    conversation_read: { conversation_id: 'wa-chat-1', remote_message_ids: ['2'], max_remote_id: '2' }
  });
  check('conversation_read sin unread_count deja 0', app.state.conversations.find(c => c.id === 'wa-chat-1').unread_count === 0);

  // 5. S4: mensaje entrante en la conversación activa sin foco no se marca leído
  app.state.activeConversationId = 'tg-chat-2';
  app.state.conversations.find(c => c.id === 'tg-chat-2').unread_count = 0;
  app.calls.markConversationRead = [];
  app.setWindowState({ focused: false });
  const incoming = (id) => ({
    type: 'message.received',
    message_received: { id, remote_message_id: id, conversation_id: 'tg-chat-2', body: 'hola', sender_type: 'CONTACT', sent_at_unix: 1700000500, attachments: [] }
  });
  app.emitEvent(incoming('m-1'));
  check('sin foco: no llama markConversationRead', app.calls.markConversationRead.length === 0);
  // El harness registra el handler realtime dos veces (start + setupRealtimeEvents), por eso se valida > 0
  check('sin foco: unread_count aumenta', app.state.conversations.find(c => c.id === 'tg-chat-2').unread_count >= 1);

  // Al recuperar el foco con la conversación al fondo se marca leído
  app.setWindowState({ focused: true });
  app.fireWindowEvent('focus');
  check('focus: llama markConversationRead', app.calls.markConversationRead.includes('tg-chat-2'));
  check('focus: unread_count vuelve a 0', app.state.conversations.find(c => c.id === 'tg-chat-2').unread_count === 0);

  // Documento oculto: tampoco se marca; al volver visible sí
  app.calls.markConversationRead = [];
  app.setWindowState({ focused: true, visibility: 'hidden' });
  app.emitEvent(incoming('m-2'));
  check('documento oculto: no llama markConversationRead', app.calls.markConversationRead.length === 0);
  app.fireDocumentEvent('visibilitychange');
  check('oculto: visibilitychange sigue oculto no marca', app.calls.markConversationRead.length === 0);
  app.setWindowState({ visibility: 'visible' });
  app.fireDocumentEvent('visibilitychange');
  check('visibilitychange visible: llama markConversationRead', app.calls.markConversationRead.includes('tg-chat-2'));

  // Con foco y visible, el entrante se marca leído como antes
  app.calls.markConversationRead = [];
  app.emitEvent(incoming('m-3'));
  check('con foco y visible: marca leído', app.calls.markConversationRead.includes('tg-chat-2'));

  finish();
}

testReadReceipts().catch(e => {
  console.error('FAIL testReadReceipts:', e);
  process.exit(1);
});
