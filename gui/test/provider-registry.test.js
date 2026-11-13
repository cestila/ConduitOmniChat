// Registro de proveedores construido desde los manifiestos de los plugins instalados:
// búsquedas por id, enum (nombre o número) y prefijo de conversación; plugins no oficiales
// y valores por defecto cuando no hay manifiestos.
// Ejecutar con: node test/provider-registry.test.js

const { createHarness, createChecker } = require('./dom-harness');
const { whatsapp, telegram, matrix, pluginInfo, officialPlugins, collectCredentialInputs } = require('./helpers/manifests');

const conversations = [
  { id: 'matrix:room-1', title: 'Sala Matrix', is_group: true, last_message_at_unix: 10 },
  { id: 'telegram:chan-1', title: 'Canal TG', is_group: true, channel_type: 'announcement', last_message_at_unix: 9 },
];

(async () => {
  const { check, finish } = createChecker();

  // ---- Con manifiestos (oficiales + Matrix no oficial) ----
  const withManifests = createHarness({
    conversations,
    plugins: [...officialPlugins(), pluginInfo(matrix), { id: 'conduit-plugin-slack', name: 'Slack', is_installed: false, is_enabled: false }],
    accounts: [{ id: 'acc-mx', provider: 'PROVIDER_SLACK', label: 'Mi Matrix', identifier: 'me', status: 'connected' }],
  });
  await withManifests.start();
  const s = withManifests.sandbox;
  const el = withManifests.el;

  check('búsqueda por id de manifiesto', s.getProviderByManifestId('matrix').name === 'Matrix' && s.getProviderByManifestId('telegram').name === 'Telegram');
  check('búsqueda por enum (nombre)', s.getProviderByEnum('PROVIDER_SLACK').id === 'matrix' && s.getProviderByEnum('PROVIDER_TELEGRAM').id === 'telegram');
  check('búsqueda por enum numérico (orden WHATSAPP=1, TELEGRAM=2, DISCORD=3)', s.getProviderByEnum(1).id === 'whatsapp' && s.getProviderByEnum(2).id === 'telegram' && s.getProviderByEnum(3).id === 'discord');
  check('búsqueda por prefijo de conversación', s.getProviderByConversationId('matrix:room-1').id === 'matrix' && s.getProviderByConversationId('telegram:x').id === 'telegram');
  check('un id sin prefijo conocido no coincide con ningún manifiesto', s.getProviderByConversationId('5491100000000@s.whatsapp.net') === null);

  const mx = s.getProviderByManifestId('matrix');
  check('el manifiesto aporta color, ícono y nombre', mx.color === '#0dbd8b' && mx.icon === 'fa-solid fa-hashtag' && mx.name === 'Matrix', JSON.stringify(mx));
  check('providerKindOf detecta el plugin no oficial por prefijo', s.providerKindOf(conversations[0]) === 'matrix', s.providerKindOf(conversations[0]));
  check('providerKindOf respeta conv.provider del core', s.providerKindOf({ id: 'uuid', provider: 'telegram' }) === 'telegram');

  const info = s.providerInfoForChannel({ provider: 'PROVIDER_SLACK' });
  check('providerInfoForChannel usa el manifiesto', info.kind === 'matrix' && info.name === 'Matrix' && info.badge === 'badge-matrix' && info.icon === 'fa-solid fa-hashtag', JSON.stringify(info));
  const msgInfo = s.providerInfoForMessage({ conversation_id: 'matrix:room-1' }, conversations[0]);
  check('providerInfoForMessage detecta el prefijo del plugin', msgInfo.kind === 'matrix' && msgInfo.name === 'Matrix');

  // Badges con color del manifiesto
  withManifests.renderConversations();
  const rows = el.conversationList ? el.conversationList.children : [];
  const html = rows.map(r => r.innerHTML).join('\n');
  check('el badge del plugin no oficial lleva su color inline, ícono y clase', html.includes('badge-matrix') && html.includes('#0dbd8b') && html.includes('fa-solid fa-hashtag'), html.slice(0, 300));
  check('los plugins oficiales conservan badge-<id>', html.includes('badge-telegram'));
  check('la semántica de channel_type se conserva (📢 anuncio)', html.includes('📢'));

  // Sidebar de cuentas y chips de filtro
  const pills = el.accountsPills.children.map(p => p.innerHTML).join('');
  check('la píldora de la cuenta usa ícono y color del manifiesto', pills.includes('fa-solid fa-hashtag') && pills.includes('#0dbd8b'), pills.slice(0, 300));
  const chips = app => app.documentStub.getElementById('chat-filters-container').innerHTML;
  check('el chip de filtro del plugin usa su id e ícono', chips(withManifests).includes('data-filter="matrix"') || withManifests.documentStub.getElementById('chat-filters-container').children.some(c => c.dataset.filter === 'matrix'));

  // Grilla de cuentas
  const grid = el.accountsGrid.children.map(c => c.innerHTML).join('');
  check('la tarjeta de cuenta muestra ícono y color del plugin', grid.includes('fa-solid fa-hashtag') && grid.includes('#0dbd8b'));

  // QR
  const qr = s.qrCopyFor('PROVIDER_SLACK');
  check('qrCopyFor usa display_name y qr_instructions', qr.title === 'Escanea con Matrix' && qr.hint === 'Escanea desde Element → Ajustes', JSON.stringify(qr));

  // Formulario de alta: solo plugins instalados y habilitados
  const values = el.newAccountProvider.children.map(o => o.value);
  check('el selector incluye el plugin no oficial y excluye los no instalados', values.includes('PROVIDER_SLACK') && values.length === 4, values.join(','));
  el.newAccountProvider.value = 'PROVIDER_SLACK';
  s.syncAccountCredentialFields();
  const inputs = collectCredentialInputs(withManifests.documentStub.getElementById('account-credential-fields'));
  check('el formulario del plugin no oficial se genera (text/password/tel)', inputs.map(i => `${i.dataset.credKey}:${i.type}`).join(',') === 'homeserver:text,access_token:password,phone:tel', inputs.map(i => `${i.dataset.credKey}:${i.type}`).join(','));
  check('la ayuda que no es https se muestra como texto plano', !withManifests.documentStub.getElementById('account-credential-fields').children.some(w => w.children.some(c => c.tagName === 'a' && /Ajustes/.test(c.textContent || ''))));
  check('el hint sale de credentials_hint', el.accountCredentialHint.textContent === 'Matrix necesita homeserver y access token.');
  check('falta homeserver → se reporta su etiqueta', s.missingRequiredCredential() === 'Homeserver');
  inputs.find(i => i.dataset.credKey === 'homeserver').value = 'https://matrix.org';
  inputs.find(i => i.dataset.credKey === 'access_token').value = 'tok';
  inputs.find(i => i.dataset.credKey === 'phone').value = '  ';
  check('buildAccountCredentials omite valores vacíos', JSON.stringify(s.buildAccountCredentials()) === JSON.stringify({ values: { homeserver: 'https://matrix.org', access_token: 'tok' } }), JSON.stringify(s.buildAccountCredentials()));

  // Panel de plugins
  withManifests.sandbox.renderPlugins();
  const cards = el.pluginsList.children.map(c => c.innerHTML);
  check('el panel de plugins muestra versión, repositorio y ruta del instalado', cards[3].includes('0.3.0') && cards[3].includes('cestila/conduit-plugin-matrix') && cards[3].includes('/conduit-plugin-matrix'), cards[3]);
  check('no hay tratamiento especial de gmail', !require('fs').readFileSync(require('path').join(__dirname, '..', 'src', 'app.js'), 'utf8').toLowerCase().includes('gmail'));

  // ---- Sin manifiestos (daemon sin plugins / pruebas) ----
  const bare = createHarness({ conversations: [{ id: 'discord:c1', title: 'x' }, { id: 'zulip:c2', title: 'y' }], plugins: [] });
  await bare.start();
  const b = bare.sandbox;
  check('sin manifiestos, los oficiales salen de la tabla por defecto', b.getProviderByEnum('PROVIDER_TELEGRAM').name === 'Telegram' && b.getProviderByEnum(3).icon === 'fa-brands fa-discord');
  check('sin manifiestos, el prefijo oficial sigue funcionando', b.providerKindOf({ id: 'discord:c1' }) === 'discord');
  check('sin manifiestos, qrCopyFor conserva el copy de Telegram', b.qrCopyFor('PROVIDER_TELEGRAM').title === 'Escanea con Telegram' && b.qrCopyFor('PROVIDER_TELEGRAM').hint === 'Ajustes → Dispositivos → Vincular desktop');
  const unknown = b.getProviderByEnum('PROVIDER_MASTODON');
  check('un proveedor desconocido recibe el fallback genérico', unknown.id === 'mastodon' && unknown.name === 'Mastodon' && unknown.icon === 'fa-solid fa-plug' && !unknown.color, JSON.stringify(unknown));
  check('el selector de alta ofrece los oficiales por defecto', bare.el.newAccountProvider.children.map(o => o.value).join(',') === 'PROVIDER_WHATSAPP,PROVIDER_TELEGRAM,PROVIDER_DISCORD', bare.el.newAccountProvider.children.map(o => o.value).join(','));
  bare.el.newAccountProvider.value = 'PROVIDER_TELEGRAM';
  b.syncAccountCredentialFields();
  check('sin manifiestos, Telegram sigue pidiendo api_id/api_hash', collectCredentialInputs(bare.documentStub.getElementById('account-credential-fields')).map(i => i.dataset.credKey).join(',') === 'api_id,api_hash,password');

  finish();
})();
