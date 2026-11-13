// Prueba de Reenvío de Mensajes (S4, S5, S6):
// Botón en la burbuja, modal de selección con filtro rápido, y reenvío de texto y adjuntos.
// Ejecutar con: node test/forward-message.test.js

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  { id: 'conv-1', title: 'Grupo Devs', provider: 'telegram', is_group: true, last_message_at_unix: 3000, unread_count: 0 },
  { id: 'conv-2', title: 'Carla Gómez', provider: 'whatsapp', is_group: false, last_message_at_unix: 2000, unread_count: 0 },
  { id: 'conv-3', title: 'Lucía Pérez', provider: 'whatsapp', is_group: false, last_message_at_unix: 1000, unread_count: 0 },
];

(async () => {
  const sampleMessages = [
    {
      id: 'msg-text-1',
      conversation_id: 'conv-1',
      sender_type: 'contact',
      sender_name: 'Pablo',
      body: 'Revisen el deploy de staging',
      sent_at_unix: 1700000100,
      attachments: [],
    },
    {
      id: 'msg-att-cached',
      conversation_id: 'conv-1',
      sender_type: 'contact',
      sender_name: 'Pablo',
      body: 'Adjunto el reporte',
      sent_at_unix: 1700000200,
      attachments: [
        { id: 'att-1', file_name: 'reporte.pdf', mime_type: 'application/pdf', file_size: 1024, download_status: 'cached', local_path: '/tmp/reporte.pdf' }
      ],
    },
    {
      id: 'msg-att-remote',
      conversation_id: 'conv-1',
      sender_type: 'contact',
      sender_name: 'Pablo',
      body: 'Foto del error',
      sent_at_unix: 1700000300,
      attachments: [
        { id: 'att-2', file_name: 'error.png', mime_type: 'image/png', file_size: 2048, download_status: 'remote_only' }
      ],
    }
  ];

  const app = createHarness({
    conversations,
  });

  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;
  const calls = app.calls;

  // Renderizar mensajes en la conversación activa
  app.state.activeConversationId = 'conv-1';
  app.renderMessages(sampleMessages);

  // S4: Botón de reenviar en toolbar de acciones
  const msgRows = el.messagesContainer.children.filter(c => c.classList && c.classList.contains('message-row'));
  check('se renderizaron 3 mensajes', msgRows.length === 3);

  const firstMsgHtml = msgRows[0] ? msgRows[0].innerHTML : '';
  check(
    'mensaje contiene botón de reenviar btn-action-forward',
    firstMsgHtml.includes('btn-action-forward'),
    firstMsgHtml
  );
  check(
    'botón de reenviar usa ícono fa-share',
    firstMsgHtml.includes('fa-share'),
    firstMsgHtml
  );

  // S5: Abrir modal de reenvío
  check('función openForwardModal existe', typeof app.openForwardModal === 'function');
  app.openForwardModal(sampleMessages[0]);

  check('modal de reenvío visible', !el.forwardModal.classList.contains('hidden'));
  check('preview muestra el texto del mensaje', el.forwardPreview.innerHTML.includes('Revisen el deploy de staging'));
  
  // Lista de chats en el modal
  const convItems = el.forwardConvList.children;
  check('modal lista las conversaciones disponibles', convItems.length >= 2);
  const convListHtml = el.forwardConvList.innerHTML;
  check('modal incluye Carla Gómez', convListHtml.includes('Carla Gómez'));
  check('modal incluye Lucía Pérez', convListHtml.includes('Lucía Pérez'));

  // Filtro rápido de búsqueda dentro del modal
  app.filterForwardConversations('Lucía');
  const filteredHtml = el.forwardConvList.innerHTML;
  check('filtro rápido muestra a Lucía', filteredHtml.includes('Lucía Pérez'));
  check('filtro rápido oculta a Carla', !filteredHtml.includes('Carla Gómez'));

  // Cerrar modal
  app.closeForwardModal();
  check('cerrar modal lo vuelve a ocultar', el.forwardModal.classList.contains('hidden'));

  // S6: Despacho de reenvío de texto simple
  app.openForwardModal(sampleMessages[0]);
  calls.sendMessage.length = 0;
  await app.forwardMessageToConversation('conv-2');

  check('sendMessage invocado para conv-2', calls.sendMessage.length === 1);
  const sentArgs = calls.sendMessage[0];
  check('sendMessage recibe conversation_id conv-2', sentArgs[0] === 'conv-2');
  check('sendMessage recibe el texto original', sentArgs[1] === 'Revisen el deploy de staging');
  check('modal queda cerrado tras enviar', el.forwardModal.classList.contains('hidden'));
  check('navega a la conversación destino conv-2', app.state.activeConversationId === 'conv-2');

  // S6: Despacho con adjunto cached
  app.openForwardModal(sampleMessages[1]);
  calls.sendMessage.length = 0;
  await app.forwardMessageToConversation('conv-3');

  check('sendMessage invocado para conv-3', calls.sendMessage.length === 1);
  const sentAttArgs = calls.sendMessage[0];
  const outAtts = sentAttArgs[4];
  check('se envían adjuntos en la llamada', Array.isArray(outAtts) && outAtts.length === 1);
  check('adjunto lleva la propiedad path con /tmp/reporte.pdf', outAtts && outAtts[0].path === '/tmp/reporte.pdf');
  check('adjunto lleva la propiedad file_path con /tmp/reporte.pdf', outAtts && outAtts[0].file_path === '/tmp/reporte.pdf');

  // S6: Despacho con adjunto remote_only (descarga a demanda previa con wait=true)
  app.openForwardModal(sampleMessages[2]);
  calls.sendMessage.length = 0;
  calls.downloadAttachment.length = 0;
  await app.forwardMessageToConversation('conv-2');

  check('downloadAttachment invocado para att-2 con wait=true', calls.downloadAttachment.some(a => a[0] === 'att-2' && a[1] === true));
  check('sendMessage invocado tras descarga', calls.sendMessage.length === 1);

  finish();
})();
