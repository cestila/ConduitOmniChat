const { createHarness, createChecker } = require('./dom-harness');

async function run() {
  console.log('--- TEST MESSAGE STATUS, DISCONNECTS & CONTACTS ---');
  const { check, finish } = createChecker();

  const conversations = [
    {
      id: 'conv-1',
      contact_id: 'c-1',
      is_group: false,
      title: '.',
      contact_phone: '+5492664680361',
      last_message_at_unix: 1700000100,
      last_message_preview: 'hola',
      provider: 'whatsapp',
    },
  ];
  const messagesByConv = {
    'conv-1': [
      { id: 'm-pend', conversation_id: 'conv-1', sender_type: 'me', body: 'a', status: 'pending', sent_at_unix: 1700000010 },
      { id: 'm-sent', conversation_id: 'conv-1', sender_type: 'me', body: 'b', status: 'sent', sent_at_unix: 1700000020 },
      { id: 'm-deliv', conversation_id: 'conv-1', sender_type: 'me', body: 'c', status: 'delivered', sent_at_unix: 1700000030 },
      { id: 'm-read', conversation_id: 'conv-1', sender_type: 'me', body: 'd', status: 'read', sent_at_unix: 1700000040 },
      { id: 'm-fail', conversation_id: 'conv-1', sender_type: 'me', body: 'e', status: 'failed', sent_at_unix: 1700000050 },
    ],
  };

  const app = createHarness({
    conversations,
    messages: messagesByConv,
  });

  await app.start();
  app.setupRealtimeEvents();

  // 1. Non-legible contact title "." falls back to showing phone in sidebar
  const convList = app.documentStub.getElementById('conversation-list');
  const convRow = convList.children[0];
  check('conversation list renders phone fallback for "."', Boolean(convRow && convRow.innerHTML.includes('+5492664680361')));

  // 2. Select conversation and verify message bubble status icons in DOM
  await app.selectConversation('conv-1');
  const msgsContainer = app.documentStub.getElementById('messages-container');
  const htmlAll = msgsContainer.children.map(c => c.innerHTML).join('\n');
  check('pending renders fa-clock', htmlAll.includes('data-msg-status="pending"') && htmlAll.includes('fa-clock'));
  check('sent renders fa-check', htmlAll.includes('data-msg-status="sent"') && htmlAll.includes('fa-check '));
  check('delivered renders fa-check-double', htmlAll.includes('data-msg-status="delivered"') && htmlAll.includes('fa-check-double'));
  check('read renders msg-status-read', htmlAll.includes('data-msg-status="read"') && htmlAll.includes('msg-status-read'));
  check('failed renders msg-status-failed and No enviado', htmlAll.includes('data-msg-status="failed"') && htmlAll.includes('No enviado'));

  // 3. Live message_status_updated updates cache & shows banner on failed
  const cached = app.state.messageCache.get('conv-1');
  app.emitEvent({
    type: 'message.status_updated',
    message_status_updated: { message_id: 'm-pend', conversation_id: 'conv-1', status: 'read' },
  });
  check('live message_status_updated transitions pending to read in cache', cached.messages.find(m => m.id === 'm-pend').status === 'read');

  app.emitEvent({
    type: 'message.status_updated',
    message_status_updated: { message_id: 'm-sent', conversation_id: 'conv-1', status: 'failed', error: 'disconnected' },
  });
  check('live message_status_updated transitions sent to failed in cache', cached.messages.find(m => m.id === 'm-sent').status === 'failed');
  const banner = app.documentStub.getElementById('connection-status-banner');
  check('connection-status-banner visible on failed message', Boolean(banner && !banner.classList.contains('hidden')));

  // 4. channel_status_update disconnected / connected toggles banner
  app.emitEvent({ type: 'channel.connected', channel_status_update: 'whatsapp:connected' });
  check('banner hidden on connected', Boolean(banner && banner.classList.contains('hidden')));

  app.emitEvent({ type: 'channel.connected', channel_status_update: 'whatsapp:disconnected:eof' });
  check('banner visible on disconnected', Boolean(banner && !banner.classList.contains('hidden')));
  check('banner text includes provider disconnect reason', app.documentStub.getElementById('connection-status-text').textContent.includes('whatsapp desconectado'));

  // 5. Rename button visibility for 1:1 contact
  const btnRename = app.documentStub.getElementById('btn-rename-contact');
  check('rename button visible for 1:1 contact', Boolean(btnRename && !btnRename.classList.contains('hidden')));

  finish();
}

run().catch((err) => {
  console.error(err);
  process.exit(1);
});
