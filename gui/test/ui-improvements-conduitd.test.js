// Prueba integral de mejoras de UI y funcionalidades de conduitd:
// - 4 temas oscuros y 4 temas claros (8 temas en total)
// - Tipografía y escalado de tamaño de fuente (--font-size-msg)
// - Acciones (reply, react, pin, delete) posicionadas dentro de la burbuja
// - Restricción de borrado: solo mensajes salientes (propios) tienen botón de eliminar y pueden borrarse
// - Funcionalidades de conduitd: fijar/desfijar mensajes, banner de fijados, instalación y conmutación de plugins, modo compacto
// Ejecutar con: node test/ui-improvements-conduitd.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-test',
    display_name: 'Camila',
    channels: [
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: '12345678', handle: 'camila_tg' },
    ],
  },
];

const conversations = [
  {
    id: 'conv-test',
    title: 'Camila',
    active_channel_id: 'ch-tg',
    is_group: false,
  },
  {
    id: 'conv-pinned',
    title: 'Chat con fijado antiguo',
    active_channel_id: 'ch-tg',
    is_group: false,
  },
  {
    id: 'conv-empty',
    title: 'Chat vacio',
    active_channel_id: 'ch-tg',
    is_group: false,
  },
];

const pluginsFixture = [
  {
    id: 'conduit-plugin-whatsapp',
    name: 'WhatsApp Plugin',
    version: '1.2.0',
    description: 'Conector oficial de WhatsApp',
    is_installed: true,
    is_enabled: true,
  },
  {
    id: 'conduit-plugin-slack',
    name: 'Slack Marketplace',
    version: '0.9.0',
    description: 'Conector para Slack Workspaces',
    is_installed: false,
    is_enabled: false,
  },
];

(async () => {
  const oldPinnedMsg = {
    id: 'msg-pinned-old',
    conversation_id: 'conv-pinned',
    sender_type: 'contact',
    body: 'Mensaje fijado antiguo importante',
    sent_at_unix: 1600000000,
    is_pinned: true,
  };

  const messagesByConv = {
    'conv-pinned': Array.from({ length: 30 }, (_, i) => ({
      id: `msg-recent-${i}`,
      conversation_id: 'conv-pinned',
      sender_type: 'contact',
      body: `Mensaje reciente ${i}`,
      sent_at_unix: 1700000000 + i,
      is_pinned: false,
    })),
    'conv-empty': [],
  };

  const pinnedMessagesFixture = {
    'conv-pinned': [oldPinnedMsg],
    'conv-empty': [],
  };

  const app = createHarness({
    contacts,
    conversations,
    plugins: pluginsFixture,
    messages: messagesByConv,
    pinnedMessages: pinnedMessagesFixture,
  });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // =========================================================================
  // 1. Temas Visuales (4 oscuros y 4 claros)
  // =========================================================================
  const darkThemes = ['dark', 'telegram', 'oled', 'nord'];
  const lightThemes = ['light', 'solarized-light', 'sky', 'mint'];

  darkThemes.forEach(theme => {
    app.sandbox.applySettingsUI({ theme });
    check(`tema oscuro '${theme}' aplica clase 'theme-${theme}' en body`, app.documentStub.body.className.includes(`theme-${theme}`));
  });

  lightThemes.forEach(theme => {
    app.sandbox.applySettingsUI({ theme });
    check(`tema claro '${theme}' aplica clase 'theme-${theme}' en body`, app.documentStub.body.className.includes(`theme-${theme}`));
  });

  // =========================================================================
  // 2. Tipografía y Tamaño de Fuente
  // =========================================================================
  app.sandbox.applySettingsUI({ message_font_size: 18 });
  check('tamaño de fuente 18px se aplica a mensajes', app.documentStub.documentElement.style.getPropertyValue('--font-size-msg') === '18px');
  check('tamaño de fuente 18px se aplica al composer', el.messageInput.style.fontSize === '18px');

  // =========================================================================
  // 3. Modo Compacto (conduitd AppSettings)
  // =========================================================================
  app.sandbox.applySettingsUI({ compact_message_mode: true });
  check('modo compacto agrega clase compact-mode en body', app.documentStub.body.classList.contains('compact-mode'));

  app.sandbox.applySettingsUI({ compact_message_mode: false });
  check('desactivar modo compacto remueve clase compact-mode', !app.documentStub.body.classList.contains('compact-mode'));

  // =========================================================================
  // 4. Mensajes: Acciones dentro de la burbuja y Borrado solo para mensajes propios
  // =========================================================================
  await app.sandbox.selectConversation('conv-test');

  const messages = [
    {
      id: 'msg-incoming-1',
      conversation_id: 'conv-test',
      sender_type: 'contact',
      body: 'Hola, este es un mensaje entrante de Camila',
      sent_at_unix: 1700000100,
      is_pinned: false,
    },
    {
      id: 'msg-outgoing-1',
      conversation_id: 'conv-test',
      sender_type: 'me',
      body: 'Hola Camila, este es mi mensaje saliente',
      sent_at_unix: 1700000150,
      is_pinned: true,
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const rows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderizan 2 mensajes en la conversación', rows.length === 2);

  // Mensaje entrante (de Camila)
  const incomingRow = rows[0];
  const incomingHTML = incomingRow ? incomingRow.innerHTML || '' : '';
  const incomingBubble = incomingRow.querySelector('.message-bubble');
  check('mensaje entrante tiene burbuja message-bubble', incomingBubble !== null);
  check('las acciones están dentro del cuadro message-bubble en mensaje entrante', incomingBubble.innerHTML.includes('msg-action-toolbar'));
  check('mensaje entrante contiene botón de responder', incomingRow.querySelector('.btn-action-reply') !== null);
  check('mensaje entrante contiene botón de reaccionar', incomingRow.querySelector('.btn-action-react') !== null);
  check('mensaje entrante contiene botón de fijar', incomingRow.querySelector('.btn-action-pin') !== null);
  check('mensaje entrante NO contiene botón de eliminar', incomingRow.querySelector('.btn-action-delete') === null);

  // Intentar borrar mensaje entrante debe ser bloqueado por seguridad
  const deleteCallsBefore = app.calls.deleteMessage.length;
  await app.sandbox.handleDeleteMessage('msg-incoming-1');
  check('intento de borrar mensaje ajeno es rechazado sin invocar backend', app.calls.deleteMessage.length === deleteCallsBefore);

  // Mensaje saliente (mío)
  const outgoingRow = rows[1];
  const outgoingBubble = outgoingRow.querySelector('.message-bubble');
  check('mensaje saliente tiene burbuja message-bubble', outgoingBubble !== null);
  check('las acciones están dentro del cuadro message-bubble en mensaje saliente', outgoingBubble.innerHTML.includes('msg-action-toolbar'));
  check('mensaje saliente SÍ contiene botón de eliminar', outgoingRow.querySelector('.btn-action-delete') !== null);
  check('mensaje saliente fijado muestra indicador de fijado', outgoingBubble.innerHTML.includes('msg-pinned-indicator'));

  // Borrar mensaje propio debe funcionar
  await app.sandbox.handleDeleteMessage('msg-outgoing-1');
  const lastDelete = app.calls.deleteMessage[app.calls.deleteMessage.length - 1];
  check('borrar mensaje propio invoca conduitAPI.deleteMessage', lastDelete && lastDelete[0] === 'msg-outgoing-1');

  // =========================================================================
  // 5. Funcionalidad de Fijar/Desfijar Mensajes (conduitd pinMessage)
  // =========================================================================
  // Fijar mensaje entrante
  await app.sandbox.handlePinMessage('msg-incoming-1', true);
  const lastPin = app.calls.pinMessage[app.calls.pinMessage.length - 1];
  check('fijar mensaje invoca conduitAPI.pinMessage con id y true', lastPin && lastPin[0] === 'msg-incoming-1' && lastPin[1] === true);

  // Desfijar mensaje
  await app.sandbox.handlePinMessage('msg-incoming-1', false);
  const lastUnpin = app.calls.pinMessage[app.calls.pinMessage.length - 1];
  check('desfijar mensaje invoca conduitAPI.pinMessage con id y false', lastUnpin && lastUnpin[0] === 'msg-incoming-1' && lastUnpin[1] === false);

  // =========================================================================
  // 6. Marketplace de Plugins (conduitd listPlugins, installPlugin, togglePlugin)
  // =========================================================================
  app.sandbox.renderPlugins();
  const pluginsContainer = el.pluginsList;
  const pluginCards = pluginsContainer.children;
  check('se renderizan 2 tarjetas de plugins', pluginCards.length === 2);

  const waCard = pluginCards[0];
  check('tarjeta muestra plugin habilitado', waCard.innerHTML.includes('Habilitado'));
  check('plugin instalado ofrece botón Deshabilitar', waCard.innerHTML.includes('Deshabilitar'));

  const slackCard = pluginCards[1];
  check('tarjeta muestra plugin no instalado', slackCard.innerHTML.includes('Deshabilitado'));
  check('plugin no instalado ofrece botón Instalar', slackCard.innerHTML.includes('Instalar'));

  // Conmutar plugin (toggle)
  const btnToggle = waCard.querySelector('.btn-toggle-plugin');
  if (btnToggle) {
    btnToggle.click();
    await new Promise(r => setTimeout(r, 10));
    const lastToggle = app.calls.togglePlugin[app.calls.togglePlugin.length - 1];
    check('clic en deshabilitar invoca conduitAPI.togglePlugin con false', lastToggle && lastToggle[0] === 'conduit-plugin-whatsapp' && lastToggle[1] === false);
  }

  // Instalar plugin (install)
  const btnInstall = slackCard.querySelector('.btn-install-plugin');
  if (btnInstall) {
    btnInstall.click();
    await new Promise(r => setTimeout(r, 10));
    const lastInstall = app.calls.installPlugin[app.calls.installPlugin.length - 1];
    check('clic en instalar invoca conduitAPI.installPlugin', lastInstall && lastInstall[0] === 'conduit-plugin-slack');
  }

  // =========================================================================
  // 7. Mensajes Fijados Antiguos y Ocultamiento del Banner en Chat Vacío
  // =========================================================================
  await app.sandbox.selectConversation('conv-pinned');
  check('getPinnedMessages fue invocado para conv-pinned', app.calls.getPinnedMessages.includes('conv-pinned'));
  check('banner de mensajes fijados visible para mensaje antiguo fuera de los 30', !el.pinnedMessageBanner.classList.contains('hidden'));
  check('banner de mensajes fijados muestra el texto del mensaje antiguo', el.pinnedBannerText.textContent === 'Mensaje fijado antiguo importante');

  // Formato de banner con encuesta
  app.state.pinnedMessages.set('conv-pinned', [{
    id: 'msg-poll-pinned',
    conversation_id: 'conv-pinned',
    poll: { question: '¿Cuál es tu color favorito?' },
    is_pinned: true,
  }]);
  app.sandbox.updatePinnedBanner();
  check('banner muestra la pregunta de la encuesta fijada', el.pinnedBannerText.textContent === '¿Cuál es tu color favorito?');

  // Formato de banner con ubicación
  app.state.pinnedMessages.set('conv-pinned', [{
    id: 'msg-loc-pinned',
    conversation_id: 'conv-pinned',
    location: { latitude: 40.4168, longitude: -3.7038 },
    is_pinned: true,
  }]);
  app.sandbox.updatePinnedBanner();
  check('banner muestra Ubicación compartida', el.pinnedBannerText.textContent === 'Ubicación compartida');

  // Formato de banner con contacto compartido
  app.state.pinnedMessages.set('conv-pinned', [{
    id: 'msg-contact-pinned',
    conversation_id: 'conv-pinned',
    shared_contact: { display_name: 'Juan' },
    is_pinned: true,
  }]);
  app.sandbox.updatePinnedBanner();
  check('banner muestra Contacto compartido', el.pinnedBannerText.textContent === 'Contacto compartido');

  // Desfijar mediante handlePinMessage remueve el mensaje de pinnedMessages y oculta el banner
  await app.sandbox.handlePinMessage('msg-contact-pinned', false);
  check('desfijar mensaje deja pinnedMessages vacío para la conversación', (app.state.pinnedMessages.get('conv-pinned') || []).length === 0);
  check('banner oculto tras desfijar el único mensaje', el.pinnedMessageBanner.classList.contains('hidden'));

  // Cambiar a conversación vacía debe ocultar el banner
  await app.sandbox.selectConversation('conv-empty');
  check('banner de mensajes fijados oculto al cambiar a conversación vacía', el.pinnedMessageBanner.classList.contains('hidden'));

  // Un mensaje fijado muy largo no debe ensanchar el layout: el texto se recorta en una línea
  // y su contenedor no aporta el ancho del texto al ancho mínimo del chat (contain: inline-size).
  const cssContent = require('fs').readFileSync(require('path').join(__dirname, '../src/styles.css'), 'utf8');
  const cssRule = (selector) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = cssContent.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`));
    return match ? match[1] : '';
  };
  check('.pinned-banner-content no propaga el ancho del texto fijado (contain: inline-size)', /contain:\s*inline-size\s*;/.test(cssRule('.pinned-banner-content')));
  check('.pinned-banner-text recorta el texto largo en una sola línea con elipsis',
    /white-space:\s*nowrap\s*;/.test(cssRule('.pinned-banner-text'))
    && /overflow:\s*hidden\s*;/.test(cssRule('.pinned-banner-text'))
    && /text-overflow:\s*ellipsis\s*;/.test(cssRule('.pinned-banner-text')));

  finish();
})();
