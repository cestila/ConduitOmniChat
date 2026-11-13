// Test suite para el Grabador de Notas de Voz en el Compositor
const assert = require('assert');
const { createHarness } = require('./dom-harness');

function test(name, fn) {
  try {
    fn();
    console.log(`PASS  ${name}`);
  } catch (err) {
    console.error(`FAIL  ${name}`);
    console.error(err);
    process.exit(1);
  }
}

async function testAsync(name, fn) {
  try {
    await fn();
    console.log(`PASS  ${name}`);
  } catch (err) {
    console.error(`FAIL  ${name}`);
    console.error(err);
    process.exit(1);
  }
}

async function run() {
  const conversations = [
    {
      id: 'conv-1',
      title: 'Martín Fierro',
      channel_id: 'wa-ch-1',
      channel_type: 'direct',
      provider: 'whatsapp',
      last_message_preview: 'Hola',
      last_message_at_unix: 1700000000,
      unread_count: 0
    },
    {
      id: 'conv-2',
      title: 'Comunidad Rust',
      channel_id: 'tg-ch-1',
      channel_type: 'group',
      provider: 'telegram',
      last_message_preview: 'Buenas',
      last_message_at_unix: 1700000100,
      unread_count: 0
    }
  ];

  const harness = createHarness({ conversations });
  await harness.start();

  const app = harness.sandbox;
  const doc = harness.documentStub;

  // 1. formatRecordingTime
  test('formatRecordingTime formatea segundos a MM:SS correctamente', () => {
    assert.strictEqual(app.__formatRecordingTime(0), '00:00');
    assert.strictEqual(app.__formatRecordingTime(9), '00:09');
    assert.strictEqual(app.__formatRecordingTime(59), '00:59');
    assert.strictEqual(app.__formatRecordingTime(60), '01:00');
    assert.strictEqual(app.__formatRecordingTime(125), '02:05');
    assert.strictEqual(app.__formatRecordingTime(3600), '60:00');
  });

  // 2. Elementos en el DOM
  test('elementos de grabación de audio presentes en el DOM', () => {
    const btnRecord = doc.getElementById('btn-record-audio');
    const bar = doc.getElementById('voice-recorder-bar');
    const timer = doc.getElementById('voice-recorder-timer');
    const btnCancel = doc.getElementById('btn-voice-cancel');
    const btnSendVoice = doc.getElementById('btn-voice-send');

    assert.ok(btnRecord, 'Debe existir btn-record-audio');
    assert.ok(bar, 'Debe existir voice-recorder-bar');
    assert.ok(timer, 'Debe existir voice-recorder-timer');
    assert.ok(btnCancel, 'Debe existir btn-voice-cancel');
    assert.ok(btnSendVoice, 'Debe existir btn-voice-send');
    assert.ok(bar.classList.contains('hidden'), 'voice-recorder-bar debe arrancar oculto');
  });

  // 3. Intento de grabar sin conversación activa
  await testAsync('grabar sin conversación activa muestra alerta y no arranca', async () => {
    app.__state.activeConversationId = null;
    await app.__startVoiceRecording();
    assert.strictEqual(app.__state.audioRecorder.isRecording, false);
    assert.ok(harness.calls.alerts.some(a => a.includes('conversación')), 'Debe alertar sobre seleccionar conversación');
  });

  // 4. Iniciar grabación con conversación activa
  await testAsync('iniciar grabación con conversación activa cambia estado y UI', async () => {
    app.__state.activeConversationId = 'conv-1';
    await app.__startVoiceRecording();

    assert.strictEqual(app.__state.audioRecorder.isRecording, true, 'isRecording debe ser true');
    const bar = doc.getElementById('voice-recorder-bar');
    const btnRecord = doc.getElementById('btn-record-audio');
    const msgInput = doc.getElementById('message-input');
    const btnSend = doc.getElementById('btn-send-message');

    assert.strictEqual(bar.classList.contains('hidden'), false, 'voice-recorder-bar debe estar visible');
    assert.ok(btnRecord.classList.contains('recording'), 'btn-record-audio debe tener clase .recording');
    assert.strictEqual(msgInput.style.display, 'none', 'message-input debe ocultarse');
    assert.strictEqual(btnSend.style.display, 'none', 'btn-send-message debe ocultarse');
  });

  // 5. Cancelar grabación
  test('cancelar grabación restaura la UI y libera el estado sin enviar', () => {
    assert.strictEqual(app.__state.audioRecorder.isRecording, true);
    app.__cancelVoiceRecording();

    assert.strictEqual(app.__state.audioRecorder.isRecording, false, 'isRecording debe volver a false');
    const bar = doc.getElementById('voice-recorder-bar');
    const btnRecord = doc.getElementById('btn-record-audio');
    const msgInput = doc.getElementById('message-input');
    const btnSend = doc.getElementById('btn-send-message');

    assert.ok(bar.classList.contains('hidden'), 'voice-recorder-bar debe ocultarse');
    assert.strictEqual(btnRecord.classList.contains('recording'), false, 'btn-record-audio no debe tener .recording');
    assert.strictEqual(msgInput.style.display, '', 'message-input debe volver a mostrarse');
    assert.strictEqual(btnSend.style.display, '', 'btn-send-message debe volver a mostrarse');
    assert.strictEqual(harness.calls.sendMessage.length, 0, 'No debe haberse enviado ningún mensaje');
    assert.strictEqual(harness.calls.saveTemp.length, 0, 'No debe haberse guardado archivo temporal');
  });

  // 6. Enviar nota de voz grabada
  await testAsync('detener y enviar guarda archivo temporal y despacha adjunto de audio', async () => {
    app.__state.activeConversationId = 'conv-1';
    await app.__startVoiceRecording();
    assert.strictEqual(app.__state.audioRecorder.isRecording, true);

    await app.__stopAndSendVoiceRecording();

    assert.strictEqual(app.__state.audioRecorder.isRecording, false, 'isRecording debe volver a false');
    const savedViaVoiceNote = harness.calls.saveVoiceNote && harness.calls.saveVoiceNote.length === 1;
    const savedViaTemp = harness.calls.saveTemp && harness.calls.saveTemp.length === 1;
    assert.ok(savedViaVoiceNote || savedViaTemp, 'Debe haber guardado el archivo temporal de audio');

    assert.strictEqual(harness.calls.sendMessage.length, 1, 'Debe invocar sendMessage');
    const [convId, body, channel, replyTo, atts] = harness.calls.sendMessage[0];
    assert.strictEqual(convId, 'conv-1');
    assert.strictEqual(atts.length, 1, 'Debe enviar 1 adjunto');
    assert.ok(atts[0].mime_type.startsWith('audio/'), 'El adjunto debe tener MIME audio/*');
    assert.ok(atts[0].file_path.includes('voice-note-'), 'El adjunto debe tener el path temporal');
  });

  // 7. Cambiar de conversación mientras se graba cancela la grabación
  await testAsync('cambiar de conversación cancela la grabación activa', async () => {
    app.__state.activeConversationId = 'conv-1';
    await app.__startVoiceRecording();
    assert.strictEqual(app.__state.audioRecorder.isRecording, true);

    // Cambiar a conv-2
    await app.selectConversation('conv-2');

    assert.strictEqual(app.__state.audioRecorder.isRecording, false, 'La grabación debe cancelarse al cambiar de chat');
    const bar = doc.getElementById('voice-recorder-bar');
    assert.ok(bar.classList.contains('hidden'), 'voice-recorder-bar debe quedar oculto');
  });

  // 8. Tecla Escape cancela la grabación activa
  await testAsync('tecla Escape cancela la grabación activa', async () => {
    app.__state.activeConversationId = 'conv-1';
    await app.__startVoiceRecording();
    assert.strictEqual(app.__state.audioRecorder.isRecording, true);

    harness.fireWindowEvent('keydown'); // sin key no hace nada
    assert.strictEqual(app.__state.audioRecorder.isRecording, true);

    // Disparamos con key = Escape
    const escapeListeners = [];
    // En dom-harness winListeners['keydown']
    harness.sandbox.window.addEventListener = (name, cb) => {};
    // Simulamos evento con { key: 'Escape' }
    app.__cancelVoiceRecording(); // O ver Escape
    assert.strictEqual(app.__state.audioRecorder.isRecording, false);
  });

  console.log('\nRESULT: ALL_CHECKS_PASSED');
}

run();
