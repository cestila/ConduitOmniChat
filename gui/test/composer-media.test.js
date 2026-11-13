// Prueba estructural del composer multimedios: adjuntar, previsualizar, quitar,
// clasificar por MIME al enviar, envío de solo texto sin adjuntos e inserción de
// emojis en la posición del cursor.
// Ejecutar con: node test/composer-media.test.js   (o npm run test:composer)

const { createHarness, createChecker } = require('./dom-harness');

(async () => {
  const app = createHarness({});
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;
  const state = app.state;

  // 1. Agregar un adjunto: queda en memoria, se previsualiza y se clasifica
  const photo = app.addPendingAttachment({ path: '/tmp/photo.png', name: 'photo.png', size: 2048 });
  check('agregar un archivo crea un adjunto pendiente', state.pendingAttachments.length === 1, `obtuvo ${state.pendingAttachments.length}`);
  check('el adjunto conserva ruta, nombre y tamaño', !!photo && photo.path === '/tmp/photo.png' && photo.name === 'photo.png' && photo.size === 2048);
  check('el MIME se deriva de la extensión al adjuntar', !!photo && photo.mime === 'image/png', photo && photo.mime);
  check('la tira de previews deja de estar oculta', !el.composerAttachments.classList.contains('hidden'));
  check('la tira de previews muestra un chip', el.composerAttachments.children.length === 1, `obtuvo ${el.composerAttachments.children.length}`);

  const chipHTML = el.composerAttachments.children[0] ? el.composerAttachments.children[0].innerHTML : '';
  check('el chip muestra el nombre del archivo', chipHTML.includes('photo.png'), chipHTML);
  check('el chip muestra el tamaño legible', chipHTML.includes('2.0 KB'), chipHTML);
  check('el chip muestra la miniatura por local-media', chipHTML.includes('local-media://'), chipHTML);

  // Adjunto que no es imagen: ícono genérico, sin miniatura
  const doc = app.addPendingAttachment({ path: '/tmp/report.pdf', name: 'report.pdf', size: 512 });
  check('el adjunto sin MIME explícito se detecta por extensión', !!doc && doc.mime === 'application/pdf', doc && doc.mime);
  const docChipHTML = el.composerAttachments.children[1] ? el.composerAttachments.children[1].innerHTML : '';
  check('un documento se previsualiza con ícono, no con miniatura', docChipHTML.includes('fa-file') && !docChipHTML.includes('<img'), docChipHTML);

  // 2. Quitar un adjunto de a uno
  app.removePendingAttachment(photo.id);
  check('quitar un adjunto lo saca de la lista', state.pendingAttachments.length === 1, `obtuvo ${state.pendingAttachments.length}`);
  check('quitar un adjunto re-renderiza la tira', el.composerAttachments.children.length === 1, `obtuvo ${el.composerAttachments.children.length}`);
  app.removePendingAttachment(doc.id);
  check('sin adjuntos la tira se oculta', state.pendingAttachments.length === 0 && el.composerAttachments.classList.contains('hidden'));

  // 3. Clasificación por MIME (image | audio | video | document)
  check('image/* se clasifica como imagen', app.classifyAttachmentType('image/gif') === 'image');
  check('animated webp se clasifica como imagen', app.classifyAttachmentType('image/webp') === 'image');
  check('audio/* se clasifica como audio', app.classifyAttachmentType('audio/ogg') === 'audio');
  check('video/* se clasifica como video', app.classifyAttachmentType('video/mp4') === 'video');
  check('cualquier otro tipo se clasifica como documento', app.classifyAttachmentType('application/pdf') === 'document');
  check('extensión desconocida cae a application/octet-stream', app.detectMimeType('backup.unknown') === 'application/octet-stream', app.detectMimeType('backup.unknown'));
  check('la extensión se detecta sin importar mayúsculas', app.detectMimeType('SCAN.JPG') === 'image/jpeg', app.detectMimeType('SCAN.JPG'));

  // 4. Envío con adjuntos: propaga los campos del contrato proto
  state.activeConversationId = 'conv-1';
  el.channelSelect.value = 'channel-9';
  app.addPendingAttachment({ path: '/tmp/photo.png', name: 'photo.png', size: 10 });
  app.addPendingAttachment({ path: '/tmp/voice.ogg', name: 'voice.ogg', size: 20 });
  el.messageInput.value = 'hello there';
  await app.handleSendMessage();

  const withAttachments = app.calls.sendMessage[0] || [];
  check('el envío propaga conversación, cuerpo y canal', withAttachments[0] === 'conv-1' && withAttachments[1] === 'hello there' && withAttachments[2] === 'channel-9', JSON.stringify(withAttachments));
  check('el envío propaga los dos adjuntos', Array.isArray(withAttachments[4]) && withAttachments[4].length === 2, JSON.stringify(withAttachments[4]));
  const firstPayload = (withAttachments[4] || [])[0] || {};
  check('el adjunto enviado lleva path, name, mime y size',
    firstPayload.path === '/tmp/photo.png' && firstPayload.name === 'photo.png' && firstPayload.mime === 'image/png' && firstPayload.size === 10,
    JSON.stringify(firstPayload));
  check('el audio enviado se clasifica con su MIME', (withAttachments[4] || [])[1] && withAttachments[4][1].mime === 'audio/ogg', JSON.stringify((withAttachments[4] || [])[1]));
  check('los adjuntos se limpian después de enviar', state.pendingAttachments.length === 0 && el.composerAttachments.classList.contains('hidden'));

  // 5. Envío de solo texto: sin adjuntos, comportamiento previo intacto
  el.messageInput.value = 'just text';
  await app.handleSendMessage();
  const textOnly = app.calls.sendMessage[1] || [];
  check('el envío de solo texto sigue enviando el cuerpo', textOnly[1] === 'just text', JSON.stringify(textOnly));
  check('el envío de solo texto no manda adjuntos', Array.isArray(textOnly[4]) && textOnly[4].length === 0, JSON.stringify(textOnly[4]));

  // 6. Composer vacío no envía nada; un mensaje sin texto pero con adjuntos sí
  el.messageInput.value = '   ';
  await app.handleSendMessage();
  check('un composer vacío sin adjuntos no envía', app.calls.sendMessage.length === 2, `obtuvo ${app.calls.sendMessage.length}`);

  app.addPendingAttachment({ path: '/tmp/note.mp4', name: 'note.mp4', size: 30 });
  el.messageInput.value = '';
  await app.handleSendMessage();
  const attachmentOnly = app.calls.sendMessage[2] || [];
  check('un mensaje sin texto pero con adjuntos es válido', attachmentOnly[1] === '' && Array.isArray(attachmentOnly[4]) && attachmentOnly[4].length === 1, JSON.stringify(attachmentOnly));

  // 7. Pegar desde el portapapeles: con ruta real no se copia a temporal
  const tempCallsBefore = app.calls.saveTemp.length;
  await app.addFilesFromFileList([{ path: '/tmp/dropped.jpg', name: 'dropped.jpg', size: 40, type: 'image/jpeg' }]);
  check('un archivo con ruta (drag & drop) se adjunta directo', state.pendingAttachments.length === 1 && state.pendingAttachments[0].path === '/tmp/dropped.jpg', JSON.stringify(state.pendingAttachments));
  check('un archivo con ruta no pasa por el guardado temporal', app.calls.saveTemp.length === tempCallsBefore, `obtuvo ${app.calls.saveTemp.length}`);

  // Bitmap pegado sin ruta (clipboardData sin File.path en Electron 33)
  app.clearPendingAttachments();
  await app.addFilesFromFileList([{ name: 'clipboard.png', size: 4, type: 'image/png', arrayBuffer: async () => new ArrayBuffer(4) }]);
  check('un archivo sin ruta se guarda en temporal', app.calls.saveTemp.length === tempCallsBefore + 1, `obtuvo ${app.calls.saveTemp.length}`);
  check('el archivo pegado queda adjunto con la ruta temporal', state.pendingAttachments.length === 1 && state.pendingAttachments[0].path.indexOf('/tmp/conduit-harness/') === 0, JSON.stringify(state.pendingAttachments));
  app.clearPendingAttachments();

  // 8. Emoji picker: inserta en la posición del cursor, no al final
  el.messageInput.value = 'hi there';
  el.messageInput.selectionStart = 3;
  el.messageInput.selectionEnd = 3;
  app.insertEmojiAtCursor('😀');
  check('el emoji se inserta en la posición del cursor', el.messageInput.value === 'hi 😀there', el.messageInput.value);

  el.messageInput.value = 'abc';
  el.messageInput.selectionStart = 1;
  el.messageInput.selectionEnd = 2;
  app.insertEmojiAtCursor('🎉');
  check('el emoji reemplaza la selección activa', el.messageInput.value === 'a🎉c', el.messageInput.value);

  // El panel de emojis arranca oculto y se abre con el botón
  check('el panel de emojis arranca oculto', el.emojiPanel.classList.contains('hidden'));
  app.toggleEmojiPanel();
  check('el botón de emojis abre el panel', !el.emojiPanel.classList.contains('hidden'));
  app.toggleEmojiPanel(false);
  check('Esc / clic afuera cierra el panel', el.emojiPanel.classList.contains('hidden'));

  finish();
})();
