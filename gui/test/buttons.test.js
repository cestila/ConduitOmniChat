// Prueba estructural de botones interactivos (Inline Keyboards y Quick Replies) en la GUI:
// - Renderizado de botonera en cuadrícula (.msg-buttons-grid, .msg-button-row)
// - Botones de tipo callback y URL (.interactive-btn, .url-btn)
// - Clic en botón invoca clickButton hacia conduitd
// Ejecutar con: node test/buttons.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-buttons',
    display_name: 'Bot Asistente',
    channels: [
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: 'bot-123', handle: 'Asistente' },
    ],
  },
];

const conversations = [
  {
    id: 'contact-buttons',
    title: 'Bot Asistente',
    active_channel_id: 'ch-tg',
    is_group: false,
  },
];

(async () => {
  const app = createHarness({ contacts, conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // Abrir la conversación
  await app.sandbox.selectConversation('contact-buttons');

  // 1. Mensaje con botones interactivos
  const messages = [
    {
      id: 'msg-btn-1',
      conversation_id: 'contact-buttons',
      sender_type: 'Bot Asistente',
      body: 'Selecciona una acción:',
      sent_at_unix: 1700000000,
      button_rows: [
        {
          buttons: [
            { id: 'cb_confirmar', text: 'Confirmar', type: 'callback' },
            { id: 'url_web', text: 'Web Oficial', type: 'url', url: 'https://conduit.chat' },
          ],
        },
      ],
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const rows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderiza la fila del mensaje', rows.length === 1);

  // 2. Inspeccionar la botonera
  const rowHTML = rows[0] ? rows[0].innerHTML || '' : '';
  check('renderiza la cuadrícula de botones', rowHTML.includes('msg-buttons-grid'));
  check('renderiza la fila de botones', rowHTML.includes('msg-button-row'));
  check('muestra el botón Confirmar', rowHTML.includes('Confirmar'));
  check('muestra el botón Web Oficial', rowHTML.includes('Web Oficial'));
  check('el botón de URL tiene clase url-btn', rowHTML.includes('url-btn'));
  check('el botón de URL contiene data-url', rowHTML.includes('data-url="https://conduit.chat"'));

  // 3. Clic en botón interactivo
  await app.sandbox.handleClickButton('msg-btn-1', 'cb_confirmar', 'Confirmar', 'callback');
  const lastClick = app.calls.clickButton[app.calls.clickButton.length - 1];
  check('clickButton fue invocado con msg-btn-1, cb_confirmar y Confirmar', lastClick && lastClick[0] === 'msg-btn-1' && lastClick[1] === 'cb_confirmar' && lastClick[2] === 'Confirmar');

  finish();
})();
