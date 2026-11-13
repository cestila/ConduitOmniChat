const { createHarness, createChecker } = require('./dom-harness');

async function testDesktopNotifications() {
  console.log('--- TEST DESKTOP NOTIFICATIONS ---');
  const { check, finish } = createChecker();

  const conversations = [
    { id: 'wa-chat-1', title: 'Grupo Amigos', unread_count: 0, last_message_preview: 'Hola', last_message_at_unix: 1700000000 },
    { id: 'tg-chat-2', title: 'Carla Gómez', unread_count: 0, last_message_preview: 'Todo bien', last_message_at_unix: 1700000100 }
  ];

  const app = createHarness({ conversations });
  await app.start();
  app.setupRealtimeEvents();

  // 1. shouldNotifyMessage: Descarte de mensajes salientes e históricos
  const outMsg = { conversation_id: 'wa-chat-1', sender_type: 'USER', body: 'Mensaje mío' };
  check('mensaje saliente USER no notifica', app.shouldNotifyMessage(outMsg, false) === false);

  const meMsg = { conversation_id: 'wa-chat-1', sender_type: 'me', body: 'Mensaje propio' };
  check('mensaje saliente me no notifica', app.shouldNotifyMessage(meMsg, false) === false);

  const histMsg = { conversation_id: 'wa-chat-1', sender_type: 'CONTACT', body: 'Mensaje viejo' };
  check('mensaje histórico no notifica', app.shouldNotifyMessage(histMsg, true) === false);

  // Descarte de mensajes viejos al inicio (catch-up / startup)
  app.state.appStartTime = 1700000400;
  const startupOldMsg = { conversation_id: 'tg-chat-2', sender_type: 'CONTACT', body: 'Mensaje de hace 2 horas', sent_at_unix: 1700000100 };
  check('mensaje anterior al arranque de la app no notifica', app.shouldNotifyMessage(startupOldMsg, false) === false);

  // 2. shouldNotifyMessage: Foco y conversación activa
  app.state.activeConversationId = 'wa-chat-1';
  app.setWindowState({ focused: true, visibility: 'visible' });

  const incActiveMsg = { conversation_id: 'wa-chat-1', sender_type: 'CONTACT', sender_name: 'Marcos', body: 'Hola grupo' };
  check('mensaje entrante en chat activo con foco NO notifica', app.shouldNotifyMessage(incActiveMsg, false) === false);

  const incOtherChatMsg = { conversation_id: 'tg-chat-2', sender_type: 'CONTACT', sender_name: 'Carla', body: 'Che, viste esto?' };
  check('mensaje entrante en OTRO chat con foco SÍ notifica', app.shouldNotifyMessage(incOtherChatMsg, false) === true);

  app.setWindowState({ focused: false });
  check('mensaje entrante en chat activo SIN foco SÍ notifica', app.shouldNotifyMessage(incActiveMsg, false) === true);

  // 3. Respeto a configuración: notifications_enabled = false
  app.state.settings = { notifications_enabled: false };
  check('notificaciones desactivadas en settings no notifica', app.shouldNotifyMessage(incActiveMsg, false) === false);
  app.state.settings = { notifications_enabled: true, notify_preview: true, notify_sound: true, notify_flash_taskbar: true };

  // 4. buildNotificationPayload: Vista previa completa vs Privacidad
  const payloadFull = app.buildNotificationPayload(incOtherChatMsg);
  check('payload con vista previa incluye remitente', payloadFull.title.includes('Carla'));
  check('payload con vista previa incluye cuerpo', payloadFull.body === 'Che, viste esto?');
  check('payload con sonido activo tiene silent=false', payloadFull.silent === false);

  // Adjunto multimedia sin cuerpo de texto
  const photoMsg = { conversation_id: 'tg-chat-2', sender_type: 'CONTACT', sender_name: 'Carla', body: '', attachments: [{ mime_type: 'image/jpeg', file_name: 'foto.jpg' }] };
  const payloadPhoto = app.buildNotificationPayload(photoMsg);
  check('adjunto foto genera texto descriptivo', payloadPhoto.body.includes('📷'));

  const voiceMsg = { conversation_id: 'tg-chat-2', sender_type: 'CONTACT', sender_name: 'Carla', body: '', attachments: [{ mime_type: 'audio/ogg', file_name: 'audio.ogg' }] };
  const payloadVoice = app.buildNotificationPayload(voiceMsg);
  check('adjunto nota de voz genera texto descriptivo', payloadVoice.body.includes('🎤'));

  // Modo privacidad (notify_preview = false)
  app.state.settings.notify_preview = false;
  app.state.settings.notify_sound = false;
  const payloadPrivate = app.buildNotificationPayload(incOtherChatMsg);
  check('modo privacidad oculta texto sensible en título', !payloadPrivate.title.includes('Che, viste esto?'));
  check('modo privacidad oculta texto del mensaje en cuerpo', payloadPrivate.body.includes('nuevo mensaje') && !payloadPrivate.body.includes('Che, viste esto?'));
  check('sonido desactivado genera silent=true', payloadPrivate.silent === true);

  // Restaurar settings normales
  app.state.settings.notify_preview = true;
  app.state.settings.notify_sound = true;

  // 5. Integración con flujo en tiempo real: disparo de notificación y taskbar flash
  app.calls.showNotification = [];
  app.calls.flashFrame = [];
  app.setWindowState({ focused: false });

  app.emitEvent({
    type: 'message.received',
    message_received: {
      id: 'msg-realtime-1',
      conversation_id: 'wa-chat-1',
      sender_type: 'CONTACT',
      sender_name: 'Lucas',
      body: 'Reunión en 10 minutos',
      sent_at_unix: 1700000500,
      attachments: []
    }
  });

  check('evento realtime sin foco dispara showNotification', app.calls.showNotification.length > 0);
  check('showNotification recibe conversationId correcto', app.calls.showNotification[0].conversationId === 'wa-chat-1');
  check('showNotification incluye el texto del mensaje', app.calls.showNotification[0].body === 'Reunión en 10 minutos');
  check('evento realtime sin foco activa flashFrame(true)', app.calls.flashFrame.includes(true));

  // 6. Recuperar foco apaga el destello de la barra de tareas
  app.fireWindowEvent('focus');
  check('recuperar foco en ventana desactiva flashFrame(false)', app.calls.flashFrame.includes(false));

  // 7. Clic en notificación: onOpenConversation navega al chat correspondiente
  app.state.activeConversationId = 'wa-chat-1';
  app.triggerOpenConversation('tg-chat-2');
  check('triggerOpenConversation cambia activeConversationId', app.state.activeConversationId === 'tg-chat-2');

  finish();
}

testDesktopNotifications().catch(err => {
  console.error('Error en testDesktopNotifications:', err);
  process.exit(1);
});
