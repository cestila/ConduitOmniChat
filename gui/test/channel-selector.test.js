// Prueba estructural del selector de cuenta en el compositor y los badges de canal:
// - Contacto con 1 canal: selector oculto
// - Contacto con >1 canal: selector visible con canales de WhatsApp y Telegram
// - Elección manual de canal: sendMessage recibe target_channel_id
// - Burbujas de mensajes: cada mensaje muestra el badge de su canal en chats unificados
// Ejecutar con: node test/channel-selector.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-multi',
    display_name: 'Martín Gómez',
    email: 'martin@example.com',
    phone: '+5491112345678',
    channels: [
      { id: 'ch-wa', provider: 'PROVIDER_WHATSAPP', remote_id: '+5491112345678', handle: '+5491112345678' },
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: 'u9981', handle: '@martingomez' },
    ],
  },
  {
    id: 'contact-single',
    display_name: 'Solo WhatsApp',
    phone: '+5491100001111',
    channels: [
      { id: 'ch-single', provider: 'PROVIDER_WHATSAPP', remote_id: '+5491100001111', handle: 'Solo WA' },
    ],
  },
];

const conversations = [
  {
    id: 'contact-multi',
    contact_id: 'contact-multi',
    title: 'Martín Gómez',
    active_channel_id: 'ch-wa',
    is_group: false,
  },
  {
    id: 'contact-single',
    contact_id: 'contact-single',
    title: 'Solo WhatsApp',
    active_channel_id: 'ch-single',
    is_group: false,
  },
];

(async () => {
  const app = createHarness({ contacts, conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // 1. Chat con un solo canal: el selector se oculta
  await app.sandbox.selectConversation('contact-single');
  const wrapper = el.channelSelectorWrapper || el.channelSelectWrapper;
  check('contacto con 1 canal oculta el selector', wrapper && wrapper.style.display === 'none');
  check('selector arranca en automático para chat simple', el.channelSelect.value === '');

  // 2. Chat con más de un canal: el selector se muestra y lista las opciones
  await app.sandbox.selectConversation('contact-multi');
  check('contacto con >1 canal muestra el selector', wrapper && wrapper.style.display !== 'none');
  const options = el.channelSelect.children;
  check('selector ofrece automático + cada canal', options.length === 3, `obtuvo ${options.length}`);
  check('primera opción es automático / último usado', options[0] && options[0].textContent.includes('Automático'));
  check('ofrece canal de WhatsApp', options.some(o => o.textContent.includes('WhatsApp')));
  check('ofrece canal de Telegram', options.some(o => o.textContent.includes('Telegram')));

  // 3. Envío manual eligiendo canal
  el.messageInput.value = 'Mensaje para enviar por Telegram';
  el.channelSelect.value = 'ch-tg';
  await app.sandbox.handleSendMessage();

  check('sendMessage fue llamado', app.calls.sendMessage.length === 1);
  const [convId, sentBody, targetChannel] = app.calls.sendMessage[0] || [];
  check('sendMessage recibe la conversación correcta', convId === 'contact-multi');
  check('sendMessage recibe el canal manual elegido', targetChannel === 'ch-tg');

  // 4. Badges de proveedor en mensajes dentro de la conversación unificada
  el.messagesContainer.innerHTML = '';
  app.sandbox.appendMessageBubble({
    id: 'm-wa',
    conversation_id: 'contact-multi',
    channel_id: 'ch-wa',
    body: 'Mensaje entrante de WhatsApp',
    sender_type: 'contact',
    sent_at_unix: 1700000000,
  });
  app.sandbox.appendMessageBubble({
    id: 'm-tg',
    conversation_id: 'contact-multi',
    channel_id: 'ch-tg',
    body: 'Mensaje entrante de Telegram',
    sender_type: 'contact',
    sent_at_unix: 1700000010,
  });

  const bubbles = el.messagesContainer.children;
  check('se renderizan 2 mensajes en el hilo', bubbles.length === 2);
  const waRow = bubbles.find(b => (b.innerHTML || '').includes('Mensaje entrante de WhatsApp'));
  const tgRow = bubbles.find(b => (b.innerHTML || '').includes('Mensaje entrante de Telegram'));

  check('mensaje de WhatsApp muestra badge-whatsapp', waRow && waRow.innerHTML.includes('badge-whatsapp'));
  check('mensaje de Telegram muestra badge-telegram', tgRow && tgRow.innerHTML.includes('badge-telegram'));

  finish();
})();
