// Prueba estructural de encuestas interactivas (Polls) en la GUI:
// - Renderizado de tarjetas de encuesta (.msg-poll-card)
// - Visualización de la pregunta, opciones, barras de porcentaje y conteo de votos
// - Opciones marcadas como chosen si el usuario votó
// - Clic en una opción de la encuesta invoca sendVote
// - Conteo total de votos en el footer
// Ejecutar con: node test/polls.test.js

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-polls',
    display_name: 'Comunidad Dev',
    channels: [
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: 'g-999', handle: 'Dev Chat' },
    ],
  },
];

const conversations = [
  {
    id: 'contact-polls',
    title: 'Comunidad Dev',
    active_channel_id: 'ch-tg',
    is_group: true,
  },
];

(async () => {
  const app = createHarness({ contacts, conversations });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // Abrir la conversación
  await app.sandbox.selectConversation('contact-polls');

  // 1. Mensaje con encuesta
  const messages = [
    {
      id: 'poll-msg-1',
      conversation_id: 'contact-polls',
      sender_type: 'Martín',
      content_type: 'poll',
      body: '¿Qué lenguaje preferís para backend?',
      sent_at_unix: 1700000000,
      poll: {
        question: '¿Qué lenguaje preferís para backend?',
        selectable_count: 1,
        total_votes: 10,
        options: [
          { id: '0', text: 'Go', vote_count: 6, chosen: true },
          { id: '1', text: 'Rust', vote_count: 3, chosen: false },
          { id: '2', text: 'TypeScript', vote_count: 1, chosen: false },
        ],
      },
    },
  ];

  app.sandbox.renderMessages(messages);

  const container = el.messagesContainer;
  const rows = container.children.filter ? container.children.filter(c => (c.innerHTML || '').includes('message-bubble')) : container.children;
  check('se renderiza la fila del mensaje', rows.length === 1);

  // 2. Inspeccionar la tarjeta de encuesta
  const rowHTML = rows[0] ? rows[0].innerHTML || '' : '';
  check('renderiza la tarjeta de encuesta', rowHTML.includes('msg-poll-card'));
  check('muestra la pregunta de la encuesta', rowHTML.includes('¿Qué lenguaje preferís para backend?'));
  check('muestra la opción Go', rowHTML.includes('Go'));
  check('muestra la opción Rust', rowHTML.includes('Rust'));
  check('muestra la opción TypeScript', rowHTML.includes('TypeScript'));
  check('muestra el porcentaje de Go (60%)', rowHTML.includes('60%'));
  check('la opción Go tiene clase chosen', rowHTML.includes('poll-option-row chosen'));
  check('muestra el total de 10 votos', rowHTML.includes('10 votos'));

  // 3. Votar en la encuesta
  await app.sandbox.handleVotePoll('poll-msg-1', '1');
  const lastVote = app.calls.sendVote[app.calls.sendVote.length - 1];
  check('sendVote fue invocado con poll-msg-1 y opción 1', lastVote && lastVote[0] === 'poll-msg-1' && lastVote[1][0] === '1');

  finish();
})();
