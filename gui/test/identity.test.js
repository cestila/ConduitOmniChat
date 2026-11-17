// Prueba estructural de la vista de Contactos rediseñada:
// - Eliminación de UI y funciones de fusión manual / split / edición de identidad
// - Lista limpia y buscable con chips clickeables por canal/cuenta (etiqueta de cuenta cuando hay >1 cuenta del mismo proveedor)
// - Inicio de chat por canal vía startChat(contactId, channelId), cambiando a vista de chat y preseleccionando channelId
// - Botón Sync Contacts llama syncContacts y refresca state.contacts
// - Evento contact_updated actualiza state.contacts y re-renderiza la lista
// Ejecutar con: node test/identity.test.js   (o npm run test:identity)

const fs = require('fs');
const path = require('path');
const { createHarness, createChecker } = require('./dom-harness');

const accounts = [
  { id: 'acc-wa-1', provider: 'PROVIDER_WHATSAPP', label: 'Personal', identifier: '+549110001' },
  { id: 'acc-wa-2', provider: 'PROVIDER_WHATSAPP', label: 'Trabajo', identifier: '+549110002' },
  { id: 'acc-tg-1', provider: 'PROVIDER_TELEGRAM', label: 'Mi Telegram', identifier: '@yo' },
];

const initialContacts = [
  {
    id: 'contact-martin',
    display_name: 'Martín Gómez',
    email: 'martin@example.com',
    phone: '+5491112345678',
    channels: [
      { id: 'ch-wa-1', provider: 'PROVIDER_WHATSAPP', account_id: 'acc-wa-1', remote_id: '+5491112345678', handle: '+5491112345678' },
      { id: 'ch-wa-2', provider: 'PROVIDER_WHATSAPP', account_id: 'acc-wa-2', remote_id: '+5491112345678', handle: '+5491112345678' },
      { id: 'ch-tg-1', provider: 'PROVIDER_TELEGRAM', account_id: 'acc-tg-1', remote_id: 'tg-9981', handle: '@martingomez' },
    ],
  },
  {
    id: 'contact-ana',
    display_name: 'Ana López',
    email: 'ana@example.com',
    phone: '',
    channels: [
      { id: 'ch-ana-tg', provider: 'PROVIDER_TELEGRAM', account_id: 'acc-tg-1', remote_id: 'tg-777', handle: '@analopez' },
    ],
  },
];

(async () => {
  let currentContacts = initialContacts.map(c => ({ ...c, channels: [...(c.channels || [])] }));

  const app = createHarness({
    accounts,
    contacts: currentContacts,
    listContacts: async () => currentContacts,
    syncContacts: async () => {
      currentContacts = [
        ...currentContacts,
        {
          id: 'contact-lucas',
          display_name: 'Lucas Pérez',
          phone: '+5491199998888',
          channels: [
            { id: 'ch-lucas-wa', provider: 'PROVIDER_WHATSAPP', account_id: 'acc-wa-1', remote_id: '+5491199998888', handle: 'Lucas' },
          ],
        },
      ];
      return { synced_count: 3, unified_count: 1 };
    },
    startChat: async (contactId, channelId) => ({
      id: `conv-${contactId}`,
      contact_id: contactId,
      title: 'Martín Gómez',
      active_channel_id: channelId,
      is_group: false,
    }),
  });

  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // 1. Eliminación de UI y funciones de fusión/split/edición manual de identidad
  const removedFunctions = [
    'populateMergeSelects',
    'searchContactsByIdentity',
    'mergeIdentityMatch',
    'splitContactChannel',
    'editContactIdentity',
    'renderContactsGrid',
  ];
  for (const fn of removedFunctions) {
    check(`función manual ${fn} fue eliminada`, typeof app.sandbox[fn] === 'undefined', `typeof=${typeof app.sandbox[fn]}`);
  }

  const indexHtml = fs.readFileSync(path.join(__dirname, '..', 'src', 'index.html'), 'utf8');
  const removedIds = [
    'btn-show-merge-modal',
    'btn-show-identity-modal',
    'btn-show-link-identity-modal',
    'merge-contacts-box',
    'identity-set-box',
    'identity-link-box',
    'contacts-grid',
  ];
  for (const id of removedIds) {
    check(`index.html no conserva #${id}`, !indexHtml.includes(id));
  }

  const preloadJs = fs.readFileSync(path.join(__dirname, '..', 'preload.js'), 'utf8');
  for (const apiName of ['mergeContacts', 'splitChannel', 'setContactIdentity', 'findContactsByIdentity']) {
    check(`preload.js no expone ${apiName}`, !preloadJs.includes(apiName));
  }
  check('preload.js expone syncContacts', preloadJs.includes('syncContacts'));
  check('preload.js expone startChat', preloadJs.includes('startChat'));

  // 2. renderContactsList renderiza lista limpia y chips por canal/cuenta
  check('renderContactsList está definido', typeof app.renderContactsList === 'function');
  if (typeof app.renderContactsList === 'function') {
    app.renderContactsList();
  }

  const rows = el.contactsList.children;
  check('se renderizan 2 filas de contactos', rows.length === 2, `obtuvo ${rows.length}`);

  const martinRow = rows.find(r => (r.innerHTML || '').includes('Martín Gómez'));
  check('fila de Martín Gómez presente', !!martinRow);
  const martinHtml = martinRow ? martinRow.innerHTML : '';
  check('Martín muestra chip ch-wa-1 con etiqueta Personal', martinHtml.includes('ch-wa-1') && martinHtml.includes('Personal'), martinHtml.slice(0, 200));
  check('Martín muestra chip ch-wa-2 con etiqueta Trabajo', martinHtml.includes('ch-wa-2') && martinHtml.includes('Trabajo'), martinHtml.slice(0, 200));
  check('Martín muestra chip ch-tg-1', martinHtml.includes('ch-tg-1'));

  const anaRow = rows.find(r => (r.innerHTML || '').includes('Ana López'));
  const anaHtml = anaRow ? anaRow.innerHTML : '';
  check('Ana muestra chip ch-ana-tg sin etiqueta duplicada de cuenta', anaHtml.includes('ch-ana-tg') && !anaHtml.includes('Mi Telegram'));

  // Búsqueda local en lista de contactos
  if (typeof app.renderContactsList === 'function') {
    el.contactsSearchInput.value = 'ana';
    app.renderContactsList();
    check('filtro de búsqueda deja solo a Ana López', el.contactsList.children.length === 1 && (el.contactsList.children[0].innerHTML || '').includes('Ana López'), `len=${el.contactsList.children.length}`);
    el.contactsSearchInput.value = '';
    app.renderContactsList();
  }

  // 3. Click en chip de proveedor/cuenta inicia chat, cambia a vista chat y selecciona channelId
  check('startChatWithChannel está definido', typeof app.startChatWithChannel === 'function');
  if (typeof app.startChatWithChannel === 'function') {
    await app.startChatWithChannel('contact-martin', 'ch-wa-2');
  }
  check('startChat fue llamado con (contactId, channelId)', app.calls.startChat.length === 1 && app.calls.startChat[0][0] === 'contact-martin' && app.calls.startChat[0][1] === 'ch-wa-2', JSON.stringify(app.calls.startChat));
  check('cambia a la vista de chat', el.viewChat.classList.contains('active'));
  check('activa la conversación devuelta por startChat', app.state.activeConversationId === 'conv-contact-martin', app.state.activeConversationId);
  check('preselecciona channelId en el compositor', el.channelSelect.value === 'ch-wa-2', `value=${el.channelSelect.value}`);
  check('nueva conversación aparece en el sidebar', el.conversationList.children.some(item => (item.innerHTML || '').includes('Martín Gómez')), `children=${el.conversationList.children.length}`);

  // 3b. Regresión: si la conversación ya está activa con mensajes renderizados y el usuario está en view-contacts,
  // startChatWithChannel debe volver a view-chat, refrescar el selector, seleccionar el nuevo channelId,
  // conservar last_message_preview / unread_count y activar la píldora tabAllConvs.
  const martinConvIdx = app.state.conversations.findIndex(c => c.id === 'conv-contact-martin');
  if (martinConvIdx >= 0) {
    app.state.conversations[martinConvIdx].last_message_preview = 'Hola Martín';
    app.state.conversations[martinConvIdx].unread_count = 3;
  }
  if (typeof app.sandbox.setActiveSidebarPill === 'function') {
    app.sandbox.setActiveSidebarPill(el.btnContacts);
  }
  el.tabAllConvs.classList.remove('active');
  const bubble = app.documentStub.createElement('div');
  bubble.className = 'message-bubble';
  el.messagesContainer.innerHTML = '';
  el.messagesContainer.appendChild(bubble);
  app.sandbox.showView('view-contacts');
  el.viewChat.classList.remove('active');
  el.channelSelect.value = '';
  await app.startChatWithChannel('contact-martin', 'ch-wa-1');
  check('con conversación ya activa, startChatWithChannel cambia igualmente a view-chat', el.viewChat.classList.contains('active'));
  check('con conversación ya activa, startChatWithChannel preselecciona el nuevo channelId', el.channelSelect.value === 'ch-wa-1', `value=${el.channelSelect.value}`);
  const mergedConv = app.state.conversations.find(c => c.id === 'conv-contact-martin');
  check('startChatWithChannel conserva last_message_preview existente', !!mergedConv && mergedConv.last_message_preview === 'Hola Martín', mergedConv && mergedConv.last_message_preview);
  check('startChatWithChannel conserva unread_count existente', !!mergedConv && mergedConv.unread_count === 3, mergedConv && mergedConv.unread_count);
  check('startChatWithChannel activa píldora tabAllConvs', el.tabAllConvs.classList.contains('active'));

  // 4. Botón Sync Contacts llama syncContacts y refresca state.contacts
  el.btnSyncContacts.click();
  await new Promise(r => setTimeout(r, 20));
  check('syncContacts fue llamado al pulsar Sync Contacts', app.calls.syncContacts.length === 1, JSON.stringify(app.calls.syncContacts));
  check('state.contacts se refresca tras sincronizar (3 contactos)', app.state.contacts.length === 3, `len=${app.state.contacts.length}`);
  check('el nuevo contacto sincronizado aparece en la lista', el.contactsList.children.some(r => (r.innerHTML || '').includes('Lucas Pérez')));

  // 5. Evento stream contact_updated actualiza state.contacts y la vista
  app.emitEvent({
    contact_updated: {
      contact: {
        id: 'contact-ana',
        display_name: 'Ana María López',
        email: 'ana@example.com',
        channels: [
          { id: 'ch-ana-tg', provider: 'PROVIDER_TELEGRAM', account_id: 'acc-tg-1', remote_id: 'tg-777', handle: '@analopez' },
        ],
      },
    },
  });
  const updatedAna = app.state.contacts.find(c => c.id === 'contact-ana');
  check('contact_updated actualiza el contacto en state.contacts', !!updatedAna && updatedAna.display_name === 'Ana María López');
  check('contact_updated re-renderiza la lista de contactos', el.contactsList.children.some(r => (r.innerHTML || '').includes('Ana María López')));

  finish();
})();
