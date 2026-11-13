// Prueba estructural de edición, borrado dinámico y mensajes enriquecidos (ubicación, contactos):
// - Renderizado de tarjetas de ubicación (.msg-location-card con coordenadas y enlace)
// - Renderizado de tarjetas de contacto (.msg-contact-card con nombre y teléfono)
// - Badge de editado (.msg-edited-badge)
// - Flujo de edición en el compositor (startEditMessage y envío con editMessage)
// - Flujo de borrado de mensaje (handleDeleteMessage invoca deleteMessage y remueve del DOM)
// Ejecutar con: node test/rich-messages.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-rich',
    display_name: 'Ana',
    channels: [
      { id: 'ch-wa', provider: 'PROVIDER_WHATSAPP', remote_id: '+5491144332211', handle: '+5491144332211' },
    ],
  },
];

const conversations = [
  {
    id: 'contact-rich',
    title: 'Ana',
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
  app.state.activeConversationId = 'contact-rich';

  // 1. Mensajes con rich media y edición
  const messages = [
    {
      id: 'msg-loc',
      conversation_id: 'contact-rich',
      sender_type: 'contact',
      content_type: 'location',
      body: 'Punto de encuentro',
      sent_at_unix: 1700000000,
      location: {
        latitude: -34.6037,
        longitude: -58.3816,
        title: 'Obelisco',
        address: 'Av. Corrientes 1066',
      },
    },
    {
      id: 'msg-contact',
      conversation_id: 'contact-rich',
      sender_type: 'contact',
      content_type: 'contact',
      body: 'Contacto recomendado',
      sent_at_unix: 1700000030,
      shared_contact: {
        display_name: 'Dr. Roberto López',
        phone_number: '+5491188776655',
        vcard: 'BEGIN:VCARD...',
      },
    },
    {
      id: 'msg-edited',
      conversation_id: 'contact-rich',
      sender_type: 'me',
      body: 'Llego en 10 minutos',
      sent_at_unix: 1700000060,
      is_edited: true,
      edited_at_unix: 1700000090,
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const rows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderizan 3 mensajes', rows.length === 3);

  // 2. Inspeccionar ubicación
  const locHTML = rows[0] ? rows[0].innerHTML || '' : '';
  check('renderiza la tarjeta de ubicación', locHTML.includes('msg-location-card'));
  check('muestra el título de la ubicación', locHTML.includes('Obelisco'));
  check('muestra la dirección', locHTML.includes('Av. Corrientes 1066'));
  check('incluye enlace a OpenStreetMap', locHTML.includes('openstreetmap.org'));

  // 3. Inspeccionar contacto compartido
  const contHTML = rows[1] ? rows[1].innerHTML || '' : '';
  check('renderiza la tarjeta de contacto', contHTML.includes('msg-contact-card'));
  check('muestra el nombre del contacto', contHTML.includes('Dr. Roberto López'));
  check('muestra el teléfono del contacto', contHTML.includes('+5491188776655'));

  // 4. Inspeccionar badge de mensaje editado
  const editHTML = rows[2] ? rows[2].innerHTML || '' : '';
  check('muestra el badge (editado)', editHTML.includes('msg-edited-badge') && editHTML.includes('(editado)'));

  // 5. Flujo de edición
  app.sandbox.startEditMessage(messages[2]);
  const editPreview = el.composerEditPreview;
  check('activa la barra de edición en el compositor', editPreview && !editPreview.classList.contains('hidden'));
  check('el input carga el texto original', el.messageInput.value === 'Llego en 10 minutos');

  // Enviar edición
  el.messageInput.value = 'Llego en 5 minutos en realidad';
  await app.sandbox.handleSendMessage();

  const lastEdit = app.calls.editMessage[app.calls.editMessage.length - 1];
  check('editMessage fue invocado con el ID y nuevo texto', lastEdit && lastEdit[0] === 'msg-edited' && lastEdit[1] === 'Llego en 5 minutos en realidad');
  check('la barra de edición vuelve a ocultarse', editPreview && editPreview.classList.contains('hidden'));

  // 6. Flujo de borrado
  await app.sandbox.handleDeleteMessage('msg-edited');
  const lastDelete = app.calls.deleteMessage[app.calls.deleteMessage.length - 1];
  check('deleteMessage fue invocado con msg-edited', lastDelete && lastDelete[0] === 'msg-edited');

  // 7. Mensajes multilínea con saltos de línea (enters)
  const multilineMsg = {
    id: 'msg-multiline',
    conversation_id: 'contact-rich',
    sender_type: 'contact',
    body: 'Línea 1\nLínea 2\nLínea 3',
    sent_at_unix: 1700000100,
  };
  app.sandbox.appendMessageBubble(multilineMsg);
  const multilineRow = el.messagesContainer.querySelector('.message-row[data-msg-id="msg-multiline"]');
  check('mensaje multilínea renderizado en el DOM', multilineRow !== null);
  const rowHTML = multilineRow ? multilineRow.innerHTML || '' : '';
  check('msg-body conserva los caracteres de salto de línea intactos', rowHTML.includes('msg-body') && rowHTML.includes('Línea 1\nLínea 2\nLínea 3'));

  const cssContent = require('fs').readFileSync(require('path').join(__dirname, '../src/styles.css'), 'utf8');
  check('.msg-body en styles.css define white-space: pre-wrap', cssContent.includes('.msg-body') && cssContent.includes('white-space: pre-wrap;'));
  check('.msg-body en styles.css define word-break / overflow-wrap', cssContent.includes('word-break: break-word;') && cssContent.includes('overflow-wrap: break-word;'));

  // 8. Evento gRPC en tiempo real: message_updated
  const liveMsg = {
    id: 'msg-live-edit',
    remote_message_id: 'rem-edit-live-1',
    conversation_id: 'contact-rich',
    sender_type: 'contact',
    body: 'Texto antes de la edición remota',
    sent_at_unix: 1700000150,
  };
  app.sandbox.appendMessageBubble(liveMsg);
  check('mensaje antes de editar existe en el DOM', el.messagesContainer.querySelector('.message-row[data-msg-id="msg-live-edit"]') !== null);

  app.emitEvent({
    message_updated: {
      id: 'msg-live-edit',
      remote_message_id: 'rem-edit-live-1',
      conversation_id: 'contact-rich',
      sender_type: 'contact',
      body: 'Texto actualizado en tiempo real',
      is_edited: true,
      edited_at_unix: 1700000180,
    },
  });

  const updatedLiveRow = el.messagesContainer.querySelector('.message-row[data-msg-id="msg-live-edit"]');
  check('fila de mensaje editado en tiempo real encontrada', updatedLiveRow !== null);
  const rowHTMLAfter = updatedLiveRow ? (updatedLiveRow.innerHTML || updatedLiveRow._html || '') : '';
  check('cuerpo de mensaje actualizado en el DOM', rowHTMLAfter.includes('Texto actualizado en tiempo real'));
  check('badge (editado) visible tras evento en tiempo real', rowHTMLAfter.includes('msg-edited-badge') && rowHTMLAfter.includes('(editado)'));

  // 9. Evento gRPC en tiempo real: message_deleted (por ID local y por remote_id)
  check('mensaje msg-loc existe antes de borrar', el.messagesContainer.querySelector('.message-row[data-msg-id="msg-loc"]') !== null);
  app.emitEvent({
    message_deleted: 'msg-loc',
  });
  check('mensaje msg-loc fue removido del DOM vía message_deleted', el.messagesContainer.querySelector('.message-row[data-msg-id="msg-loc"]') === null);

  // Borrado por remote_id
  check('mensaje live existe antes de borrar por remote_id', el.messagesContainer.querySelector('.message-row[data-remote-id="rem-edit-live-1"]') !== null);
  app.emitEvent({
    message_deleted: 'rem-edit-live-1',
  });
  check('mensaje fue removido del DOM buscando por remote_id', el.messagesContainer.querySelector('.message-row[data-remote-id="rem-edit-live-1"]') === null);

  finish();
})();
