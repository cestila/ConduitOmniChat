const { createHarness, createChecker } = require('./dom-harness');

(async () => {
  const { check, finish } = createChecker();

  const conversations = [
    { id: 'conv-1', title: 'Ana', is_group: false, last_message_at_unix: 1000 }
  ];

  const messages = {
    'conv-1': [
      {
        id: 'm-aud-1',
        conversation_id: 'conv-1',
        sender_type: 'CONTACT',
        sender_name: 'Ana',
        body: '',
        sent_at_unix: 1700000100,
        attachments: [
          {
            id: 'att-aud-1',
            file_name: 'voice_note.ogg',
            mime_type: 'audio/ogg',
            download_status: 'cached',
            local_path: '/tmp/voice_note.ogg'
          }
        ]
      }
    ]
  };

  const app = createHarness({ conversations, messages });
  await app.start();
  await app.selectConversation('conv-1');
  await new Promise(r => setTimeout(r, 20));

  const feedHtml = app.el.messagesContainer.children.map(c => c.innerHTML).join('\n');

  check('renderiza tarjeta media-audio-card personalizada', feedHtml.includes('media-audio-card'));
  check('no usa controles nativos <audio controls>', !feedHtml.includes('<audio controls'));
  check('incluye botón play/pausa, pista de progreso y selector de velocidad',
    feedHtml.includes('audio-play-btn') &&
    feedHtml.includes('audio-waveform-track') &&
    feedHtml.includes('audio-speed-btn') &&
    feedHtml.includes('1x')
  );
  check('muestra duración inicial formateada 0:00', feedHtml.includes('0:00'));

  finish();
})();
