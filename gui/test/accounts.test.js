// Prueba estructural de la vista de cuentas: campos de credenciales generados desde los
// manifiestos de los plugins, estado de demostración explícito y mensaje de error visible.
// Ejecutar con: node test/accounts.test.js   (o npm run test:accounts)

const { createHarness, createChecker } = require('./dom-harness');
const { officialPlugins, collectCredentialInputs } = require('./helpers/manifests');

const accounts = [
  {
    id: 'acc-tg-demo',
    provider: 'PROVIDER_TELEGRAM',
    label: 'TG Trabajo',
    identifier: '+5491100000002',
    status: 'demo',
    status_message: 'Modo demostración: la sincronización de Telegram todavía es simulada.',
    credentials: { api_id: '123456', api_hash: 'hash', phone: '+5491100000002' },
  },
  {
    id: 'acc-dc-error',
    provider: 'PROVIDER_DISCORD',
    label: 'Discord Gaming',
    identifier: 'discord-bot',
    status: 'error',
    status_message: 'faltan credenciales de Discord: bot_token o user_token',
  },
];

(async () => {
  const app = createHarness({ accounts, plugins: officialPlugins() });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;
  const container = app.documentStub.getElementById('account-credential-fields');
  const inputs = () => collectCredentialInputs(container);
  const byKey = (key) => inputs().find(i => i.dataset.credKey === key);

  // 0. El formulario ya no trae bloques fijos por proveedor
  check('index.html no conserva #cred-fields-telegram / #cred-fields-discord', !require('fs').readFileSync(require('path').join(__dirname, '..', 'src', 'index.html'), 'utf8').match(/cred-fields-(telegram|discord)/));

  // 1. El selector de proveedor sale de los manifiestos instalados y habilitados
  const options = el.newAccountProvider.children;
  check('el selector ofrece un proveedor por manifiesto', options.length === 3, `obtuve ${options.length}`);
  const tgOption = options.find(o => o.value === 'PROVIDER_TELEGRAM');
  check('la opción usa el enum del manifiesto y "nombre — descripción"', !!tgOption && tgOption.textContent === 'Telegram — Telegram MTProto (QR login)', tgOption && tgOption.textContent);

  // 2. Campos de credenciales generados desde el manifiesto
  el.newAccountProvider.value = 'PROVIDER_TELEGRAM';
  app.syncAccountCredentialFields();
  check('Telegram genera api_id, api_hash y password', inputs().map(i => i.dataset.credKey).join(',') === 'api_id,api_hash,password', inputs().map(i => i.dataset.credKey).join(','));
  check('api_id NUMBER → text con inputmode numérico', byKey('api_id').type === 'text' && byKey('api_id').inputmode === 'numeric');
  check('api_hash SECRET → password', byKey('api_hash').type === 'password');
  check('el placeholder viene del manifiesto', byKey('api_id').placeholder === '1234567');
  const labelsText = container.children.map(w => (w.children.find(c => c.tagName === 'label') || {}).textContent);
  check('un campo obligatorio se marca con asterisco y el opcional no', labelsText[0] === 'api_id *' && labelsText[1] === 'api_hash *' && labelsText[2] === 'Contraseña de nube (2FA, opcional)', JSON.stringify(labelsText));
  check('la ayuda https se muestra como enlace', container.children.some(w => w.children.some(c => c.tagName === 'a' && c.href === 'https://my.telegram.org/apps')));
  check('el hint de credenciales sale del manifiesto', el.accountCredentialHint.textContent.includes('my.telegram.org'), el.accountCredentialHint.textContent);

  el.newAccountProvider.value = 'PROVIDER_DISCORD';
  app.syncAccountCredentialFields();
  check('Discord genera bot_token y user_token (password)', inputs().map(i => i.dataset.credKey).join(',') === 'bot_token,user_token' && inputs().every(i => i.type === 'password'), inputs().map(i => i.dataset.credKey).join(','));
  check('al cambiar de proveedor se descartan los campos anteriores', !byKey('api_id'));

  el.newAccountProvider.value = 'PROVIDER_WHATSAPP';
  app.syncAccountCredentialFields();
  check('WhatsApp no renderiza campos de credenciales', inputs().length === 0, `obtuve ${inputs().length}`);

  // 3. Las credenciales escritas se envían al core como { values }
  el.newAccountProvider.value = 'PROVIDER_TELEGRAM';
  app.syncAccountCredentialFields();
  check('con campos obligatorios vacíos se reporta la etiqueta faltante', app.sandbox.missingRequiredCredential() === 'api_id', app.sandbox.missingRequiredCredential());
  byKey('api_id').value = ' 123456 ';
  byKey('api_hash').value = 'hash-abc';
  check('con api_id y api_hash completos no falta nada', app.sandbox.missingRequiredCredential() === '');
  const tgCreds = app.buildAccountCredentials();
  check('las credenciales se envían como { values } recortadas y sin vacíos', JSON.stringify(tgCreds) === JSON.stringify({ values: { api_id: '123456', api_hash: 'hash-abc' } }), JSON.stringify(tgCreds));
  byKey('password').value = 'clave-2fa';
  check('la contraseña opcional se incluye cuando se escribe', app.buildAccountCredentials().values.password === 'clave-2fa');

  // El panel QR habla el idioma del proveedor que lo emitió.
  check('el QR de Telegram se anuncia como Telegram con su instructivo', app.qrCopyFor('PROVIDER_TELEGRAM').title === 'Escanea con Telegram' && app.qrCopyFor('PROVIDER_TELEGRAM').hint === 'Ajustes → Dispositivos → Vincular desktop', JSON.stringify(app.qrCopyFor('PROVIDER_TELEGRAM')));
  check('el QR de WhatsApp conserva su copy', app.qrCopyFor('PROVIDER_WHATSAPP').title === 'Escanea con WhatsApp', JSON.stringify(app.qrCopyFor('PROVIDER_WHATSAPP')));

  el.newAccountProvider.value = 'PROVIDER_DISCORD';
  app.syncAccountCredentialFields();
  byKey('bot_token').value = 'bot-token-xyz';
  const dcCreds = app.buildAccountCredentials();
  check('el token de bot se envía para Discord', JSON.stringify(dcCreds) === JSON.stringify({ values: { bot_token: 'bot-token-xyz' } }), JSON.stringify(dcCreds));

  el.newAccountProvider.value = 'PROVIDER_WHATSAPP';
  app.syncAccountCredentialFields();
  check('WhatsApp no envía credenciales', app.buildAccountCredentials() === null);

  // 4. El alta bloquea el envío si falta un campo obligatorio y limpia los campos al terminar
  const added = [];
  app.sandbox.conduitAPI.addAccount = async (...args) => { added.push(args); return { id: 'new-acc' }; };
  app.sandbox.conduitAPI.connectAccount = async () => ({});
  el.newAccountProvider.value = 'PROVIDER_TELEGRAM';
  app.syncAccountCredentialFields();
  el.newAccountLabel.value = 'TG nuevo';
  app.documentStub.getElementById('btn-submit-add-account').click();
  await new Promise(r => setTimeout(r, 20));
  check('un campo obligatorio vacío bloquea el alta', added.length === 0, JSON.stringify(added));
  check('el aviso nombra la etiqueta del campo faltante', app.calls.alerts.some(a => a.includes('api_id')), JSON.stringify(app.calls.alerts));

  byKey('api_id').value = '999';
  byKey('api_hash').value = 'h';
  app.documentStub.getElementById('btn-submit-add-account').click();
  await new Promise(r => setTimeout(r, 20));
  check('el alta envía enum, etiqueta y { values }', added.length === 1 && added[0][0] === 'PROVIDER_TELEGRAM' && added[0][1] === 'TG nuevo' && JSON.stringify(added[0][3]) === JSON.stringify({ values: { api_id: '999', api_hash: 'h' } }), JSON.stringify(added));
  check('tras guardar se limpian los campos generados', inputs().every(i => i.value === ''));

  // 5. Una cuenta en modo demostración no se presenta como conexión real
  const cards = el.accountsGrid.children;
  check('se renderizan 2 cuentas', cards.length === 2, `obtuve ${cards.length}`);
  const demoCard = cards.find(card => (card.innerHTML || '').includes('TG Trabajo'));
  check('la cuenta simulada se marca como demostración', !!demoCard && /demostraci[oó]n/i.test(demoCard.innerHTML), (demoCard && demoCard.innerHTML.slice(0, 200)) || 'sin tarjeta');
  check('la cuenta simulada no se presenta como conectada', !!demoCard && !demoCard.innerHTML.includes('status-badge-connected'));
  check('la cuenta simulada explica que no hay conexión real', !!demoCard && /todav[ií]a es simulada/i.test(demoCard.innerHTML));

  // 6. Un error de credenciales muestra el mensaje que nombra lo que falta
  const errorCard = cards.find(card => (card.innerHTML || '').includes('Discord Gaming'));
  check('la cuenta en error muestra el mensaje del core', !!errorCard && errorCard.innerHTML.includes('bot_token'), (errorCard && errorCard.innerHTML.slice(0, 200)) || 'sin tarjeta');

  finish();
})();
