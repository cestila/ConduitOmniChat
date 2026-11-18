const { createHarness, createChecker } = require('./dom-harness');

(async () => {
  const { check, finish } = createChecker();

  const conversations = [
    { id: 'conv-1', title: 'Ana', is_group: false, last_message_at_unix: 1000 }
  ];

  const messages = {
    'conv-1': [
      {
        id: 'm-url-1',
        conversation_id: 'conv-1',
        sender_type: 'CONTACT',
        sender_name: 'Ana',
        body: 'Mirá este artículo: https://example.com/guia-conduit y decime qué opinás.',
        sent_at_unix: 1700000100,
        attachments: []
      },
      {
        id: 'm-xss-1',
        conversation_id: 'conv-1',
        sender_type: 'CONTACT',
        sender_name: 'Ana',
        body: 'No debería ejecutar <script>alert(1)</script> ni javascript:alert(2)',
        sent_at_unix: 1700000200,
        attachments: []
      }
    ]
  };

  const linkPreviews = {
    'https://example.com/guia-conduit': {
      url: 'https://example.com/guia-conduit',
      title: 'Guía de ConduitOmniChat',
      description: 'Documentación oficial del cliente unificado.',
      image: 'https://example.com/og.png',
      siteName: 'Example Docs',
      domain: 'example.com'
    }
  };

  const app = createHarness({ conversations, messages, linkPreviews });
  await app.start();
  await app.selectConversation('conv-1');
  await new Promise(r => setTimeout(r, 30));

  const feedHtml = app.el.messagesContainer.children.map(c => c.innerHTML).join('\n');

  check('renderiza enlace clicable para URL https', feedHtml.includes('class="msg-link"') && feedHtml.includes('https://example.com/guia-conduit'));
  check('escapa HTML malicioso fuera y dentro de enlaces', !feedHtml.includes('<script>alert(1)</script>') && !feedHtml.includes('href="javascript:'));
  check('invoca fetchLinkPreview para la URL detectada', app.calls.fetchLinkPreview.includes('https://example.com/guia-conduit'));
  check('renderiza tarjeta link-preview-card con título y dominio', feedHtml.includes('link-preview-card') && feedHtml.includes('Guía de ConduitOmniChat') && feedHtml.includes('example.com'));

  finish();
})();
