const { createHarness, createChecker } = require('./dom-harness');

(async () => {
  const { check, finish } = createChecker();

  const app = createHarness({ conversations: [] });
  app.el.syncProgressModal.classList.add('hidden');
  await app.start();

  check('modal de sincronización arranca oculto', app.el.syncProgressModal.classList.contains('hidden'));

  app.emitEvent({
    type: 'sync.progress',
    channel_status_update: JSON.stringify({
      provider: 'telegram',
      completed: 2,
      total: 10,
      status: 'syncing'
    })
  });
  await new Promise(r => setTimeout(r, 20));

  check('modal de sincronización se muestra al recibir sync.progress', !app.el.syncProgressModal.classList.contains('hidden'));
  check('barra de progreso refleja 20%', app.el.syncProgressBar.style.width === '20%', app.el.syncProgressBar.style.width);
  check('texto de progreso muestra 2 / 10', app.el.syncProgressCount.textContent.includes('2 / 10'), app.el.syncProgressCount.textContent);

  app.emitEvent({
    type: 'sync.progress',
    channel_status_update: JSON.stringify({
      provider: 'telegram',
      completed: 10,
      total: 10,
      status: 'done'
    })
  });
  await new Promise(r => setTimeout(r, 450));

  check('modal se oculta automáticamente al finalizar', app.el.syncProgressModal.classList.contains('hidden'));

  finish();
})();
