// Manifiestos de ejemplo con la forma que entrega ListPlugins (enums como string).
// Replican los valores de plugins/*/manifest.go para probar la GUI sin daemon.

const KIND = {
  text: 'CREDENTIAL_FIELD_KIND_TEXT',
  secret: 'CREDENTIAL_FIELD_KIND_SECRET',
  number: 'CREDENTIAL_FIELD_KIND_NUMBER',
  phone: 'CREDENTIAL_FIELD_KIND_PHONE',
};

const whatsapp = {
  id: 'whatsapp',
  display_name: 'WhatsApp',
  description: 'WhatsApp Multi-Device (QR)',
  version: '1.2.0',
  author: 'Conduit contributors',
  homepage: 'https://github.com/cestila/conduit-plugin-whatsapp',
  provider: 'PROVIDER_WHATSAPP',
  icon: 'fa-brands fa-whatsapp',
  color: '#25d366',
  credential_fields: [],
  login_flows: ['LOGIN_FLOW_QR'],
  qr_instructions: '',
  capabilities: ['reactions'],
  protocol_version: 1,
  credentials_hint: 'WhatsApp se vincula escaneando el código QR; usa el identificador para nombrar el dispositivo.',
};

const telegram = {
  id: 'telegram',
  display_name: 'Telegram',
  description: 'Telegram MTProto (QR login)',
  version: '1.3.0',
  author: 'Conduit contributors',
  homepage: 'https://github.com/cestila/conduit-plugin-telegram',
  provider: 'PROVIDER_TELEGRAM',
  icon: 'fa-brands fa-telegram',
  color: '#229ed9',
  credential_fields: [
    { key: 'api_id', label: 'api_id', kind: KIND.number, required: true, placeholder: '1234567', help: 'https://my.telegram.org/apps' },
    { key: 'api_hash', label: 'api_hash', kind: KIND.secret, required: true, placeholder: '0123456789abcdef', help: 'https://my.telegram.org/apps' },
    { key: 'password', label: 'Contraseña de nube (2FA, opcional)', kind: KIND.secret, required: false, placeholder: 'Solo si tu cuenta tiene 2FA', help: '' },
  ],
  login_flows: ['LOGIN_FLOW_QR'],
  qr_instructions: 'Ajustes → Dispositivos → Vincular desktop',
  capabilities: ['reactions'],
  protocol_version: 1,
  credentials_hint: 'Telegram se vincula escaneando un QR: solo necesitas api_id y api_hash de my.telegram.org.',
};

const discord = {
  id: 'discord',
  display_name: 'Discord',
  description: 'Discord (bot o user token)',
  version: '1.1.0',
  author: 'Conduit contributors',
  homepage: 'https://github.com/cestila/conduit-plugin-discord',
  provider: 'PROVIDER_DISCORD',
  icon: 'fa-brands fa-discord',
  color: '#5865f2',
  credential_fields: [
    { key: 'bot_token', label: 'Bot token', kind: KIND.secret, required: false, placeholder: 'Bot token del portal de desarrolladores', help: '' },
    { key: 'user_token', label: 'User token (alternativo)', kind: KIND.secret, required: false, placeholder: 'Token de usuario', help: '' },
  ],
  login_flows: ['LOGIN_FLOW_TOKEN'],
  qr_instructions: '',
  capabilities: [],
  protocol_version: 1,
  credentials_hint: 'Discord real necesita un bot token del portal de desarrolladores o, como alternativa, un user token.',
};

// Plugin no oficial: id propio sobre un enum existente del SDK.
const matrix = {
  id: 'matrix',
  display_name: 'Matrix',
  description: 'Matrix homeserver bridge',
  version: '0.3.0',
  author: 'Alguien',
  homepage: 'https://example.org/matrix',
  provider: 'PROVIDER_SLACK',
  icon: 'fa-solid fa-hashtag',
  color: '#0dbd8b',
  credential_fields: [
    { key: 'homeserver', label: 'Homeserver', kind: KIND.text, required: true, placeholder: 'https://matrix.org', help: '' },
    { key: 'access_token', label: 'Access token', kind: KIND.secret, required: true, placeholder: '', help: 'Se obtiene en Ajustes → Ayuda' },
    { key: 'phone', label: 'Teléfono', kind: KIND.phone, required: false, placeholder: '+54911...', help: '' },
  ],
  login_flows: ['LOGIN_FLOW_TOKEN'],
  qr_instructions: 'Escanea desde Element → Ajustes',
  capabilities: [],
  protocol_version: 1,
  credentials_hint: 'Matrix necesita homeserver y access token.',
};

function pluginInfo(manifest, extra = {}) {
  return {
    id: manifest.id,
    name: manifest.display_name,
    description: manifest.description,
    version: manifest.version,
    author: manifest.author,
    is_installed: true,
    is_enabled: true,
    manifest,
    repository: `cestila/conduit-plugin-${manifest.id}`,
    path: `/home/user/.local/share/conduit/plugins/conduit-plugin-${manifest.id}`,
    ...extra,
  };
}

const officialPlugins = () => [whatsapp, telegram, discord].map(m => pluginInfo(m));

// Recorre el árbol del DOM simulado y devuelve los inputs generados (dataset.credKey).
function collectCredentialInputs(node, out = []) {
  if (!node) return out;
  if (node.dataset && node.dataset.credKey) out.push(node);
  (node.children || []).forEach(c => collectCredentialInputs(c, out));
  return out;
}

module.exports = { KIND, whatsapp, telegram, discord, matrix, pluginInfo, officialPlugins, collectCredentialInputs };
