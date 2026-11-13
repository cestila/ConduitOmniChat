const { createHarness, createChecker } = require('./dom-harness');

function msgWith(attachments, extra = {}) {
  return {
    id: 'msg-1', conversation_id: 'conv-1', sender_type: 'contact', sender_name: 'Ana',
    body: '', sent_at_unix: 1700000000, attachments, ...extra,
  };
}

function rowHTML(app) {
  const row = app.el.messagesContainer.children.find(c => c.dataset && c.dataset.msgId === 'msg-1');
  return row ? row.innerHTML : '';
}

async function run() {
  console.log('--- TEST DESCARGA A DEMANDA DE ADJUNTOS (S8/S9) ---');
  const { check, finish } = createChecker();
  const app = createHarness({ conversations: [{ id: 'conv-1', title: 'Chat', unread_count: 0 }] });
  await app.start();
  app.setupRealtimeEvents();
  app.state.activeConversationId = 'conv-1';

  // 1. remote_only: tarjeta con icono por tipo, nombre, tamaño y botón de descarga
  const html1 = app.renderAttachmentHTML({
    id: 'a1', message_id: 'msg-1', file_name: 'informe.pdf', mime_type: 'application/pdf',
    file_size: 2 * 1024 * 1024, download_status: 'remote_only',
  });
  check('remote_only: muestra el nombre', html1.includes('informe.pdf'));
  check('remote_only: muestra el tamaño legible', html1.includes('2.0 MB'));
  check('remote_only: icono según tipo (pdf)', html1.includes('fa-file-pdf'));
  check('remote_only: botón de descarga que llama al handler', html1.includes('attachment-download-btn') && html1.includes("downloadAttachmentFromCard('a1')"));
  check('remote_only: no intenta cargar el archivo', !html1.includes('local-media://') && !html1.includes('<img') && !html1.includes('<video') && !html1.includes('<audio'));
  const htmlImg = app.renderAttachmentHTML({ id: 'a1b', file_name: 'foto.jpg', mime_type: 'image/jpeg', file_size: 500, download_status: 'remote_only' });
  check('remote_only imagen: icono de imagen y sin <img>', htmlImg.includes('fa-image') && !htmlImg.includes('<img') && htmlImg.includes('500 B'));

  // 2. downloading: spinner indeterminado, sin botón de descarga
  const html2 = app.renderAttachmentHTML({ id: 'a2', file_name: 'video.mp4', mime_type: 'video/mp4', file_size: 1024, download_status: 'downloading' });
  check('downloading: spinner indeterminado', html2.includes('attachment-spinner'));
  check('downloading: sin botón de descarga ni reintento', !html2.includes('attachment-download-btn') && !html2.includes('attachment-retry-btn'));

  // 3. failed: reintento
  const html3 = app.renderAttachmentHTML({ id: 'a3', file_name: 'x.zip', mime_type: 'application/zip', file_size: 10, download_status: 'failed' });
  check('failed: botón de reintento que llama al handler', html3.includes('attachment-retry-btn') && html3.includes("downloadAttachmentFromCard('a3')"));
  check('failed: indica el error', html3.includes('Falló la descarga'));
  check('failed: sin spinner', !html3.includes('attachment-spinner'));

  // 4. cached: render actual + Guardar como
  const cachedImg = { id: 'a4', file_name: 'foto.png', mime_type: 'image/png', file_size: 5, local_path: '/m/foto.png', download_status: 'cached' };
  const html4 = app.renderAttachmentHTML(cachedImg);
  check('cached imagen: render actual', html4.includes('media-photo-img') && html4.includes('local-media://'));
  check('cached imagen: sin tarjeta de descarga', !html4.includes('attachment-download-btn') && !html4.includes('attachment-spinner'));
  check('cached imagen: acción Guardar como', html4.includes('attachment-save-btn') && html4.includes("saveAttachmentFromUI('a4')") && html4.includes('Guardar como'));
  const htmlDoc = app.renderAttachmentHTML({ id: 'a5', file_name: 'a.pdf', mime_type: 'application/pdf', local_path: '/m/a.pdf', download_status: 'cached' });
  check('cached documento: render actual + Guardar como', htmlDoc.includes('media-document-card') && htmlDoc.includes("saveAttachmentFromUI('a5')"));
  const htmlAud = app.renderAttachmentHTML({ id: 'a6', file_name: 'a.ogg', mime_type: 'audio/ogg', local_path: '/m/a.ogg', download_status: 'cached' });
  const htmlVid = app.renderAttachmentHTML({ id: 'a7', file_name: 'a.mp4', mime_type: 'video/mp4', local_path: '/m/a.mp4', download_status: 'cached' });
  check('cached audio y video: Guardar como', htmlAud.includes("saveAttachmentFromUI('a6')") && htmlVid.includes("saveAttachmentFromUI('a7')"));
  check('remote_only también ofrece Guardar como', html1.includes("saveAttachmentFromUI('a1')"));
  const legacy = app.renderAttachmentHTML({ id: 'a8', file_name: 'old.png', mime_type: 'image/png', local_path: '/m/old.png' });
  check('sin estado pero con ruta: se ve como cached', legacy.includes('media-photo-img'));

  // 5. Click -> downloadAttachment(id,false) y estado optimista "downloading"
  app.renderMessages([msgWith([{ id: 'att-x', message_id: 'msg-1', file_name: 'foto.jpg', mime_type: 'image/jpeg', file_size: 2048, download_status: 'remote_only' }])]);
  check('render por mensaje: tarjeta remote_only', rowHTML(app).includes('attachment-download-btn'));
  await app.downloadAttachmentFromCard('att-x');
  check('click: llama downloadAttachment(id, false)', app.calls.downloadAttachment.length === 1 && app.calls.downloadAttachment[0][0] === 'att-x' && app.calls.downloadAttachment[0][1] === false);
  check('click: muestra downloading de inmediato', rowHTML(app).includes('attachment-spinner') && !rowHTML(app).includes('attachment-download-btn'));

  // 6. message_updated cached reemplaza la tarjeta
  app.emitEvent({ message_updated: msgWith([{ id: 'att-x', message_id: 'msg-1', file_name: 'foto.jpg', mime_type: 'image/jpeg', file_size: 2048, local_path: '/m/foto.jpg', download_status: 'cached' }]) });
  const after = rowHTML(app);
  check('message_updated cached: muestra el medio', after.includes('media-photo-img') && after.includes('local-media://'));
  check('message_updated cached: ya no hay spinner ni descarga', !after.includes('attachment-spinner') && !after.includes('attachment-download-btn'));
  check('message_updated cached: se ofrece Guardar como', after.includes("saveAttachmentFromUI('att-x')"));

  // 7. message_updated failed muestra reintento
  app.emitEvent({ message_updated: msgWith([{ id: 'att-x', message_id: 'msg-1', file_name: 'foto.jpg', mime_type: 'image/jpeg', file_size: 2048, download_status: 'failed' }]) });
  check('message_updated failed: reintento', rowHTML(app).includes('attachment-retry-btn'));

  // 8. Guardar como delega al proceso principal con el adjunto
  await app.saveAttachmentFromUI('att-x');
  check('Guardar como: llama saveAttachmentAs con el adjunto', app.calls.saveAttachmentAs.length === 1 && app.calls.saveAttachmentAs[0].id === 'att-x');

  const toasts = () => app.documentStub.body.children.filter(c => (c.className || '').includes('notice-toast'));
  check('Guardar como ok: aviso de éxito', toasts().length === 1 && toasts()[0].textContent === 'Archivo guardado' && !toasts()[0].classList.contains('error'));

  // 9. Cancelado: sin aviso; error: aviso de error
  const appCancel = createHarness({ saveResult: { canceled: true } });
  await appCancel.start();
  appCancel.renderAttachmentHTML({ id: 'c1', file_name: 'a.pdf', local_path: '/m/a.pdf', download_status: 'cached' });
  await appCancel.saveAttachmentFromUI('c1');
  check('Guardar como cancelado: sin aviso', appCancel.documentStub.body.children.length === 0);
  const appErr = createHarness({ saveResult: { error: 'sin permisos' } });
  await appErr.start();
  appErr.renderAttachmentHTML({ id: 'e1', file_name: 'a.pdf', local_path: '/m/a.pdf', download_status: 'cached' });
  await appErr.saveAttachmentFromUI('e1');
  const errToast = appErr.documentStub.body.children[0];
  check('Guardar como con error: aviso de error con el motivo', errToast && errToast.classList.contains('error') && errToast.textContent.includes('sin permisos'));

  // 10. Fallo al pedir la descarga: pasa a failed (reintento) y avisa
  const appFail = createHarness();
  appFail.sandbox.conduitAPI.downloadAttachment = async () => { throw new Error('rpc'); };
  await appFail.start();
  appFail.renderMessages([msgWith([{ id: 'f1', message_id: 'msg-1', file_name: 'a.pdf', mime_type: 'application/pdf', download_status: 'remote_only' }])]);
  await appFail.downloadAttachmentFromCard('f1');
  check('RPC de descarga falla: vuelve a mostrar reintento y avisa', rowHTML(appFail).includes('attachment-retry-btn') && appFail.documentStub.body.children.length === 1);

  finish();
}

run().catch(e => { console.error(e); process.exit(1); });
