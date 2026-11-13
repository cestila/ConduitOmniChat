// Prueba de búsqueda unificada: contactos/grupos arriba y mensajes abajo con nombre real.
// Ejecutar con: node test/search-unified.test.js

const { createHarness, createChecker } = require('./dom-harness');

const conversations = [
  { id: 'telegram:c1212630063', title: 'Lemon - Comunidad', provider: 'telegram', is_group: true, last_message_at_unix: 3000, unread_count: 0, last_message_preview: 'hola' },
  { id: '01476c7d-34b2-485a-b34f-1a5f6f990569', title: 'Fulano Ejemplo', provider: 'telegram', is_group: false, last_message_at_unix: 2000, unread_count: 1, last_message_preview: '¿todo bien?' },
  { id: '5491100000001@s.whatsapp.net', title: 'Fulano Prueba', provider: 'whatsapp', is_group: false, last_message_at_unix: 1000, unread_count: 0, last_message_preview: 'ok' },
  { id: 'discord:chan-general-101', title: '#general', provider: 'discord', server_id: 'guild-1', server_name: 'Conduit HQ', is_group: true, last_message_at_unix: 500, unread_count: 0, last_message_preview: '' },
];

(async () => {
  const searchedMessages = [
    {
      id: 'msg-search-1',
      conversation_id: 'telegram:c1212630063',
      body: 'Preguntale a Fulano si viene a la reunión',
      sent_at_unix: 1700000000,
      sender_name: 'Martín',
    }
  ];

  const app = createHarness({
    conversations,
    searchMessages: async (q) => {
      if (q.toLowerCase().includes('fulano')) {
        return searchedMessages;
      }
      return [];
    }
  });

  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // Disparar búsqueda "Fulano"
  app.handleSearch('Fulano');

  // Esperar el debounce de búsqueda (300ms + margen)
  await new Promise(r => setTimeout(r, 350));

  const items = el.conversationList.children;
  const allHtml = items.map(n => n.innerHTML || n.textContent || '');

  // S1: Contactos arriba
  check('hay resultados en la lista', items.length > 0, `items: ${items.length}`);
  const hasChatsHeader = allHtml.some(h => /chats|contactos/i.test(h));
  check('se muestra cabecera de sección para chats/contactos', hasChatsHeader, allHtml[0]);

  // Debe incluir Fulano Ejemplo y Fulano Prueba en los primeros resultados
  const firstSectionHtml = allHtml.slice(0, 3).join(' | ');
  check('Fulano Ejemplo aparece en contactos', firstSectionHtml.includes('Fulano Ejemplo'), firstSectionHtml);
  check('Fulano Prueba aparece en contactos', firstSectionHtml.includes('Fulano Prueba'), firstSectionHtml);

  // S2: Mensajes abajo con nombre real
  const hasMsgsHeader = allHtml.some(h => /mensajes/i.test(h));
  check('se muestra cabecera de sección para mensajes', hasMsgsHeader, allHtml.join(' | '));

  // El mensaje encontrado debe mostrar el nombre real del chat "Lemon - Comunidad", NO el ID cortado
  const msgResult = allHtml.find(h => h.includes('Preguntale a Fulano'));
  check('el resultado de mensaje existe', Boolean(msgResult), allHtml.join(' | '));
  check(
    'el resultado de mensaje muestra el título real "Lemon - Comunidad"',
    msgResult && msgResult.includes('Lemon - Comunidad'),
    msgResult
  );
  check(
    'el resultado de mensaje NO muestra id truncado "telegram..."',
    msgResult && !msgResult.includes('telegram...'),
    msgResult
  );

  // S3: Búsqueda sin resultados
  app.handleSearch('palabraquenoexiste12345');
  await new Promise(r => setTimeout(r, 350));
  const emptyHtml = el.conversationList.innerHTML;
  check('búsqueda sin coincidencias muestra mensaje sin resultados', /no se encontraron|sin resultados/i.test(emptyHtml), emptyHtml);

  // S3: Limpiar búsqueda restaura la lista de chats
  app.handleSearch('');
  await new Promise(r => setTimeout(r, 50));
  check('limpiar búsqueda oculta botón limpiar', el.searchClearBtn.classList.contains('hidden'));
  const restoredHtml = el.conversationList.children.map(n => n.innerHTML || '').join(' ');
  check('limpiar búsqueda restaura conversaciones', restoredHtml.includes('Lemon - Comunidad') && restoredHtml.includes('Fulano Ejemplo'));

  finish();
})();
