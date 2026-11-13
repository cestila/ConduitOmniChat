// Conduit GUI Application Client Logic
// Bridges Electron UI with conduitd via gRPC preload API

const state = {
  appStartTime: 0,
  conversations: [],
  activeConversationId: null,
  activeFilter: 'all',
  accounts: [],
  plugins: [],
  contacts: [],
  isSearching: false,
  activeServerId: null, // Servidor/supergrupo abierto en modo drill-down (null = lista unificada)
  messageCache: new Map(), // convId -> { messages: [], lastFetch: number }
  pinnedMessages: new Map(), // convId -> ProtoMessage[]
  lastConversationsHash: '', // Para evitar re-renderizados innecesarios del sidebar
  pendingAttachments: [], // Adjuntos listos para enviar (se limpian al enviar)
  activeReply: null, // Cita activa en el compositor { id, sender, body }
  activeEditMessage: null, // Edición activa en el compositor { id, body }
  forwardTargetMessage: null, // Mensaje seleccionado para reenvío
  audioRecorder: {
    stream: null,
    mediaRecorder: null,
    chunks: [],
    timerInterval: null,
    startTime: 0,
    isRecording: false
  }
};

// DOM Elements
const el = {
  conversationList: document.getElementById('conversation-list'),
  serverNavHeader: document.getElementById('server-nav-header'),
  serverNavTitle: document.getElementById('server-nav-title'),
  serverNavCount: document.getElementById('server-nav-count'),
  btnServerBack: document.getElementById('btn-server-back'),
  chatFiltersContainer: document.getElementById('chat-filters-container'),
  messagesContainer: document.getElementById('messages-container'),
  btnScrollBottom: document.getElementById('btn-scroll-bottom'),
  scrollBottomBadge: document.getElementById('scroll-bottom-badge'),
  chatProgressBar: document.getElementById('chat-progress-bar'),
  pinnedMessageBanner: document.getElementById('pinned-message-banner'),
  pinnedBannerText: document.getElementById('pinned-banner-text'),
  pinnedBannerContent: document.getElementById('pinned-banner-content'),
  btnUnpinBanner: document.getElementById('btn-unpin-banner'),
  messageInput: document.getElementById('message-input'),
  btnSend: document.getElementById('btn-send-message'),
  composer: document.getElementById('chat-composer'),
  composerAttachments: document.getElementById('composer-attachments'),
  composerReplyPreview: document.getElementById('composer-reply-preview'),
  replyPreviewSender: document.getElementById('reply-preview-sender'),
  replyPreviewText: document.getElementById('reply-preview-text'),
  btnCancelReply: document.getElementById('btn-cancel-reply'),
  composerEditPreview: document.getElementById('composer-edit-preview'),
  editPreviewText: document.getElementById('edit-preview-text'),
  btnCancelEdit: document.getElementById('btn-cancel-edit'),
  btnAttachFile: document.getElementById('btn-attach-file'),
  btnEmojiPicker: document.getElementById('btn-emoji-picker'),
  emojiPanel: document.getElementById('emoji-panel'),
  chatSearchInput: document.getElementById('chat-search-input'),
  searchClearBtn: document.getElementById('search-clear-btn'),
  activeChatTitle: document.getElementById('active-chat-title'),
  activeChatSubtitle: document.getElementById('active-chat-subtitle'),
  activeChatAvatar: document.getElementById('active-chat-avatar'),
  channelSelect: document.getElementById('channel-select'),
  channelSelectorWrapper: document.getElementById('channel-selector-wrapper'),
  daemonStatusIndicator: document.getElementById('daemon-status-indicator'),
  daemonStatusText: document.getElementById('daemon-status-text'),
  filterChips: document.querySelectorAll('.filter-chip'),
  accountsPills: document.getElementById('accounts-pills'),
  accountsGrid: document.getElementById('accounts-grid'),
  pluginsList: document.getElementById('plugins-list'),
  
  // Vistas
  viewChat: document.getElementById('view-chat'),
  viewAccounts: document.getElementById('view-accounts'),
  viewContacts: document.getElementById('view-contacts'),
  viewPlugins: document.getElementById('view-plugins'),
  viewSettings: document.getElementById('view-settings'),
  
  // Botones de navegación
  tabAllConvs: document.getElementById('tab-all-convs'),
  btnOpenAccounts: document.getElementById('btn-open-accounts'),
  btnContacts: document.getElementById('btn-contacts'),
  btnPlugins: document.getElementById('btn-plugins'),
  btnSettings: document.getElementById('btn-settings'),
  btnRefreshChats: document.getElementById('btn-refresh-chats'),
  btnRestartApp: document.getElementById('btn-restart-app'),
  settingDaemonInfoText: document.getElementById('setting-daemon-info-text'),
  btnSettingRestartDaemon: document.getElementById('btn-setting-restart-daemon'),
  btnSettingRestartApp: document.getElementById('btn-setting-restart-app'),

  // Contactos Unificados
  contactsGrid: document.getElementById('contacts-grid'),
  mergeContactsBox: document.getElementById('merge-contacts-box'),
  btnShowMergeModal: document.getElementById('btn-show-merge-modal'),
  btnCancelMerge: document.getElementById('btn-cancel-merge'),
  btnSubmitMerge: document.getElementById('btn-submit-merge'),
  mergePrimarySelect: document.getElementById('merge-primary-select'),
  mergeSecondarySelect: document.getElementById('merge-secondary-select'),

  // Identidad canónica (email / teléfono)
  identitySetBox: document.getElementById('identity-set-box'),
  identityLinkBox: document.getElementById('identity-link-box'),
  identityContactSelect: document.getElementById('identity-contact-select'),
  identityEmailInput: document.getElementById('identity-email-input'),
  identityPhoneInput: document.getElementById('identity-phone-input'),
  btnShowIdentityModal: document.getElementById('btn-show-identity-modal'),
  btnCancelIdentity: document.getElementById('btn-cancel-identity'),
  btnSaveIdentity: document.getElementById('btn-save-identity'),
  identityQueryInput: document.getElementById('identity-query-input'),
  identityTargetSelect: document.getElementById('identity-target-select'),
  identityResults: document.getElementById('identity-results'),
  btnShowLinkIdentityModal: document.getElementById('btn-show-link-identity-modal'),
  btnCancelLinkIdentity: document.getElementById('btn-cancel-link-identity'),
  btnFindIdentity: document.getElementById('btn-find-identity'),

  // Alta de cuenta y credenciales por proveedor
  newAccountProvider: document.getElementById('new-account-provider'),
  newAccountLabel: document.getElementById('new-account-label'),
  newAccountIdentifier: document.getElementById('new-account-identifier'),
  accountCredentialFields: document.getElementById('account-credential-fields'),
  accountCredentialHint: document.getElementById('account-credential-hint'),

  // Modal de Reenvío
  forwardModal: document.getElementById('forward-modal'),
  forwardBtnClose: document.getElementById('forward-btn-close'),
  forwardPreview: document.getElementById('forward-preview'),
  forwardSearchInput: document.getElementById('forward-search-input'),
  forwardConvList: document.getElementById('forward-conversation-list'),

  // Grabador de notas de voz
  btnRecordAudio: document.getElementById('btn-record-audio'),
  voiceRecorderBar: document.getElementById('voice-recorder-bar'),
  voiceRecorderTimer: document.getElementById('voice-recorder-timer'),
  btnVoiceCancel: document.getElementById('btn-voice-cancel'),
  btnVoiceSend: document.getElementById('btn-voice-send')
};

// Inicialización
document.addEventListener('DOMContentLoaded', async () => {
  state.appStartTime = Math.floor(Date.now() / 1000);
  setupEventListeners();
  await checkDaemonStatus();
  await loadSettings();
  await loadPlugins();
  await loadConversations();
  await loadContacts();
  await loadAccounts();
  setupRealtimeEvents();

  // Polling de salud cada 10s
  setInterval(checkDaemonStatus, 10000);
});

// Comprobar estado de conduitd
async function checkDaemonStatus() {
  try {
    const status = await window.conduitAPI.getStatus();
    let daemonInfo = null;
    if (window.conduitAPI.getDaemonInfo) {
      try { daemonInfo = await window.conduitAPI.getDaemonInfo(); } catch (_) {}
    }

    if (status && !status.error) {
      el.daemonStatusIndicator.querySelector('.status-dot').className = 'status-dot online';
      const mode = (daemonInfo && daemonInfo.isOwned) ? `Sidecar [PID ${daemonInfo.pid}]` : `v${status.version || '1.0'}`;
      el.daemonStatusText.textContent = `conduitd ${mode} OK`;

      if (el.settingDaemonInfoText) {
        if (daemonInfo && daemonInfo.isOwned) {
          el.settingDaemonInfoText.textContent = `Gestionado automáticamente por Electron como Sidecar (PID ${daemonInfo.pid}, Puerto ${daemonInfo.port || 50051}).`;
        } else if (daemonInfo && daemonInfo.isRunning) {
          el.settingDaemonInfoText.textContent = `Conectado a demonio externo en 127.0.0.1:${daemonInfo.port || 50051}.`;
        } else {
          el.settingDaemonInfoText.textContent = `En línea (v${status.version || '1.0'}).`;
        }
      }
    } else {
      markDaemonOffline(status ? status.error : 'Desconectado');
    }
  } catch (err) {
    markDaemonOffline(err.message);
  }
}

function markDaemonOffline(msg) {
  el.daemonStatusIndicator.querySelector('.status-dot').className = 'status-dot offline';
  el.daemonStatusText.textContent = `conduitd Offline (${msg})`;
  if (el.settingDaemonInfoText) {
    el.settingDaemonInfoText.textContent = `Demonio fuera de línea (${msg}).`;
  }
}

// Cargar y mostrar lista de conversaciones
async function loadConversations() {
  try {
    const convs = await window.conduitAPI.listConversations(200, 0);
    state.conversations = convs || [];
    renderConversations();

    // Restaurar conversación previa guardada en localStorage o seleccionar la más reciente
    const savedActiveId = localStorage.getItem('conduit_active_conversation_id');
    if (!state.activeConversationId) {
      if (savedActiveId && state.conversations.some(c => c.id === savedActiveId)) {
        selectConversation(savedActiveId);
      } else if (state.conversations.length > 0) {
        const sorted = [...state.conversations].sort((a, b) => (b.last_message_at_unix || 0) - (a.last_message_at_unix || 0));
        selectConversation(sorted[0].id);
      }
    }
  } catch (err) {
    el.conversationList.innerHTML = `<div class="loading-state" style="color:var(--danger)">Error al cargar chats: ${err.message}</div>`;
  }
}

function renderConversations() {
  let list = [...state.conversations];

  // 1. ORDEN: Desde el último en tener un cambio (más reciente) a los cambios más viejos
  list.sort((a, b) => (b.last_message_at_unix || 0) - (a.last_message_at_unix || 0));

  if (state.activeFilter !== 'all') {
    list = list.filter(c => providerKindOf(c) === state.activeFilter);
  }

  // Comprobar si hubo cambios reales en orden, previews o unread_counts antes de tocar el DOM
  const currentHash = list.map(c => `${c.id}:${c.last_message_at_unix}:${c.unread_count}:${c.last_message_preview}`).join('|') + `|filter:${state.activeFilter}|srv:${state.activeServerId}|active:${state.activeConversationId}`;
  if (currentHash === state.lastConversationsHash) {
    return; // Nada ha cambiado, evitamos reescribir el DOM para que no haya micro-congelamientos
  }
  state.lastConversationsHash = currentHash;

  // 2. Modo drill-down: dentro de un servidor/supergrupo se listan sus canales (estilo Telegram)
  if (state.activeServerId) {
    const servers = buildServerGroups(list);
    const server = servers.get(state.activeServerId);
    if (server) {
      renderServerChannelsView(server);
      return;
    }
    // El servidor ya no existe (cuenta desconectada o sin canales): volver a la lista unificada
    state.activeServerId = null;
  }

  updateServerNavHeader(null);

  if (list.length === 0) {
    el.conversationList.innerHTML = `<div class="loading-state">No hay conversaciones activas.</div>`;
    return;
  }

  el.conversationList.innerHTML = '';

  // 3. Estructurar jerarquía estilo Telegram / Discord
  // Los servidores o supergrupos agrupan sus canales secundarios y se abren con drill-down
  const serverGroups = buildServerGroups(list);
  const groupedIds = groupedConversationIds(serverGroups);

  // Combinar servidores y chats individuales, ordenados globalmente por actividad reciente
  const unifiedTimeline = [
    ...Array.from(serverGroups.values()).map(g => ({ isServerContainer: true, lastActivity: g.lastActivity, server: g })),
    ...list
      .filter(conv => !groupedIds.has(conv.id))
      .map(conv => ({ isServerContainer: false, lastActivity: conv.last_message_at_unix || 0, conv }))
  ];

  unifiedTimeline.sort((a, b) => b.lastActivity - a.lastActivity);

  unifiedTimeline.forEach(item => {
    if (item.isServerContainer) {
      renderServerContainer(item.server);
    } else {
      // Chat directo o grupo standalone (WhatsApp, DM Telegram, DM Discord)
      renderChannelItem(item.conv, false);
    }
  });
}

// Devuelve el identificador del servidor/supergrupo que contiene la conversación ('' si es individual).
function conversationServerId(conv) {
  if (conv.server_id) return conv.server_id;
  if (conv.is_group && conv.parent_channel_id) return conv.parent_channel_id;
  return '';
}

// Agrupa las conversaciones de servidores por su contenedor.
//
// Un contenedor solo tiene sentido si agrupa varios canales: un grupo de
// Telegram con un único canal se muestra como chat directo, sin segunda ventana.
function buildServerGroups(list) {
  const groups = new Map();
  list.forEach(conv => {
    const serverId = conversationServerId(conv);
    if (!serverId) return;

    if (!groups.has(serverId)) {
      groups.set(serverId, {
        id: serverId,
        name: conv.server_name || conv.title,
        provider: providerKindOf(conv),
        avatarUrl: conv.avatar_url || '',
        lastActivity: 0,
        unreadCount: 0,
        channels: []
      });
    }

    const group = groups.get(serverId);
    const provider = providerKindOf(conv);
    const isRootServerConv = conv.id === `${provider}:${serverId}` || conv.id === serverId;

    if (!isRootServerConv) {
      group.channels.push(conv);
    }
    group.unreadCount += (conv.unread_count || 0);
    if (!group.avatarUrl && conv.avatar_url) group.avatarUrl = conv.avatar_url;
    if ((conv.last_message_at_unix || 0) > group.lastActivity) {
      group.lastActivity = conv.last_message_at_unix || 0;
    }
  });

  for (const [id, group] of groups) {
    if (group.channels.length < 2) {
      groups.delete(id);
    }
  }
  return groups;
}

// IDs de las conversaciones que quedaron agrupadas dentro de un contenedor.
function groupedConversationIds(groups) {
  const ids = new Set();
  groups.forEach(group => {
    group.channels.forEach(conv => ids.add(conv.id));
  });
  return ids;
}

// ================= REGISTRO DE PROVEEDORES =================
// Todo lo específico de un proveedor (nombre, ícono, color, campos de credenciales, copy del QR)
// sale de los manifiestos de los plugins instalados. La tabla de abajo es el ÚNICO lugar con
// valores por defecto para los proveedores oficiales y solo se usa cuando no hay manifiestos
// (daemon sin conexión o pruebas); un manifiesto con el mismo id la reemplaza.
const GENERIC_PROVIDER_ICON = 'fa-solid fa-plug';
const OFFICIAL_PROVIDER_FALLBACKS = [
  {
    id: 'whatsapp', provider: 'PROVIDER_WHATSAPP', number: 1, name: 'WhatsApp',
    description: 'Multi-Device / QR', icon: 'fa-brands fa-whatsapp',
    credentialsHint: 'WhatsApp se vincula escaneando el código QR; usa el identificador para nombrar el dispositivo.',
    credentialFields: [],
  },
  {
    id: 'telegram', provider: 'PROVIDER_TELEGRAM', number: 2, name: 'Telegram',
    description: 'MTProto / QR', icon: 'fa-brands fa-telegram',
    qrInstructions: 'Ajustes → Dispositivos → Vincular desktop',
    credentialsHint: 'Telegram se vincula escaneando un QR: solo necesitas api_id y api_hash de my.telegram.org. La contraseña de nube solo hace falta si tu cuenta tiene 2FA.',
    credentialFields: [
      { key: 'api_id', label: 'api_id', kind: 'CREDENTIAL_FIELD_KIND_NUMBER', required: true, placeholder: '1234567', help: 'https://my.telegram.org/apps' },
      { key: 'api_hash', label: 'api_hash', kind: 'CREDENTIAL_FIELD_KIND_SECRET', required: true, placeholder: '0123456789abcdef', help: 'https://my.telegram.org/apps' },
      { key: 'password', label: 'Contraseña de nube (2FA, opcional)', kind: 'CREDENTIAL_FIELD_KIND_SECRET', required: false, placeholder: 'Solo si tu cuenta tiene 2FA', help: '' },
    ],
  },
  {
    id: 'discord', provider: 'PROVIDER_DISCORD', number: 3, name: 'Discord',
    description: 'Gateway / Bot / User', icon: 'fa-brands fa-discord',
    credentialsHint: 'Discord real necesita un bot token del portal de desarrolladores o, como alternativa, un user token.',
    credentialFields: [
      { key: 'bot_token', label: 'Bot token', kind: 'CREDENTIAL_FIELD_KIND_SECRET', required: false, placeholder: 'Bot token del portal de desarrolladores', help: '' },
      { key: 'user_token', label: 'User token (alternativo)', kind: 'CREDENTIAL_FIELD_KIND_SECRET', required: false, placeholder: 'Token de usuario', help: '' },
    ],
  },
];

// Proveedores construidos desde los manifiestos instalados (se reconstruye en cada loadPlugins).
let providerManifests = [];

function safeProviderId(id) {
  return String(id == null ? '' : id).toLowerCase().replace(/[^a-z0-9_-]/g, '');
}

function capitalizeName(text) {
  const t = String(text || '');
  return t ? t.charAt(0).toUpperCase() + t.slice(1) : t;
}

// Normaliza un manifiesto (o una entrada de la tabla por defecto) a la forma que usa la UI.
function makeProviderInfo(src) {
  const id = safeProviderId(src.id);
  const color = /^#[0-9a-fA-F]{3,8}$/.test(src.color || '') ? src.color : '';
  const icon = /^[\w\- ]+$/.test(src.icon || '') ? src.icon : GENERIC_PROVIDER_ICON;
  return {
    id,
    kind: id,
    provider: src.provider || '',
    number: src.number || 0,
    name: src.name || capitalizeName(id),
    description: src.description || '',
    icon,
    color,
    badge: `badge-${id}`,
    badgeStyle: color ? `background-color:${color};` : '',
    qrInstructions: src.qrInstructions || '',
    credentialsHint: src.credentialsHint || '',
    credentialFields: src.credentialFields || [],
    version: src.version || '',
    enabled: src.enabled !== false,
    fromManifest: !!src.fromManifest,
  };
}

function providerFromManifest(manifest, pluginInfo) {
  return makeProviderInfo({
    id: manifest.id,
    provider: manifest.provider,
    name: manifest.display_name || manifest.id,
    description: manifest.description,
    icon: manifest.icon,
    color: manifest.color,
    qrInstructions: manifest.qr_instructions,
    credentialsHint: manifest.credentials_hint,
    credentialFields: manifest.credential_fields,
    version: manifest.version,
    enabled: pluginInfo ? pluginInfo.is_enabled !== false : true,
    fromManifest: true,
  });
}

function rebuildProviderRegistry(plugins) {
  providerManifests = (plugins || [])
    .filter(p => p && p.manifest && p.manifest.id && p.is_installed !== false)
    .map(p => providerFromManifest(p.manifest, p));
}

// Manifiestos primero; los oficiales de la tabla solo completan los ids sin manifiesto.
function allKnownProviders() {
  const fallbacks = OFFICIAL_PROVIDER_FALLBACKS
    .filter(f => !providerManifests.some(m => m.id === f.id))
    .map(makeProviderInfo);
  return providerManifests.concat(fallbacks);
}

function defaultProvider() {
  return allKnownProviders().find(p => p.id === 'whatsapp') || makeProviderInfo(OFFICIAL_PROVIDER_FALLBACKS[0]);
}

function getProviderByManifestId(id) {
  const key = safeProviderId(id);
  return allKnownProviders().find(p => p.id === key) || null;
}

// Acepta el nombre del enum ('PROVIDER_TELEGRAM'), su número (solo para los oficiales, cuando
// no hay manifiesto) o el id del plugin ('telegram'). Un valor desconocido recibe un fallback
// genérico derivado del nombre del enum; un valor vacío cae en el proveedor por defecto.
function getProviderByEnum(value) {
  if (value == null || value === '' || value === 0 || value === '0') return defaultProvider();
  const known = allKnownProviders();
  let enumName = String(value).trim();
  if (/^\d+$/.test(enumName)) {
    const fb = OFFICIAL_PROVIDER_FALLBACKS.find(f => f.number === Number(enumName));
    if (!fb) return makeProviderInfo({ id: `provider-${enumName}`, name: `Proveedor ${enumName}`, provider: enumName });
    enumName = fb.provider;
  }
  const upper = enumName.toUpperCase();
  const byEnum = known.find(p => p.provider && p.provider.toUpperCase() === upper);
  if (byEnum) return byEnum;
  const byId = known.find(p => p.id === safeProviderId(enumName));
  if (byId) return byId;
  const derived = enumName.replace(/^PROVIDER_/i, '');
  return makeProviderInfo({ id: derived, provider: enumName, name: capitalizeName(derived.toLowerCase()) });
}

// Proveedor dueño de una conversación según el prefijo '<id>:' de su ID.
function getProviderByConversationId(convId) {
  const id = String(convId || '');
  return allKnownProviders().find(p => id.startsWith(`${p.id}:`)) || null;
}

// Pista adicional: el nombre del servidor menciona al proveedor (p. ej. "Telegram").
function getProviderByServerName(serverName) {
  const name = String(serverName || '');
  if (!name) return null;
  return allKnownProviders().find(p => p.name && name.includes(p.name)) || null;
}

function providerBadgeStyleAttr(info) {
  return info && info.badgeStyle ? ` style="${info.badgeStyle}"` : '';
}

// Proveedor de una conversación según el prefijo de su ID (<id>:) o el que resolvió el core.
function providerKindOf(conv) {
  // El core ya resuelve el proveedor del canal activo. Sin ese dato, un chat
  // directo (id de contacto, UUID) caía siempre en WhatsApp.
  if (conv && conv.provider) return getProviderByEnum(conv.provider).id;
  const hit = getProviderByConversationId((conv && conv.id) || '') || getProviderByServerName(conv && conv.server_name);
  return hit ? hit.id : defaultProvider().id;
}

// Información de visualización de una conversación (nombre, badge, ícono, color).
function providerInfoForConversation(conv) {
  return getProviderByManifestId(providerKindOf(conv)) || getProviderByEnum(conv && conv.provider);
}

// Prefijo del título según el tipo de canal (semántica de channel_type, no del proveedor).
function channelTypePrefix(kind, conv) {
  if (kind === 'telegram') {
    if (conv.channel_type === 'announcement') return '📢 ';
    if (conv.channel_type === 'forum') return '💬 ';
    return '';
  }
  if (kind === 'discord') {
    if (conv.channel_type === 'voice') return '🔊 ';
    if (conv.channel_type === 'text') return '# ';
    return '';
  }
  return conv.is_group ? '👥 ' : '';
}

// Convierte un identificador de categoría (cat-text-channels) en una etiqueta legible.
function humanizeCategory(key) {
  return key
    .replace(/^cat[-_]?/i, '')
    .replace(/[-_]+/g, ' ')
    .trim()
    .toUpperCase() || 'CANALES';
}

// Cabecera superior con botón de volver cuando estamos dentro de un servidor.
function updateServerNavHeader(server) {
  if (!el.serverNavHeader) return;

  if (!server) {
    el.serverNavHeader.classList.add('hidden');
    if (el.chatFiltersContainer) el.chatFiltersContainer.classList.remove('hidden');
    return;
  }

  el.serverNavHeader.classList.remove('hidden');
  if (el.chatFiltersContainer) el.chatFiltersContainer.classList.add('hidden');
  if (el.serverNavTitle) el.serverNavTitle.textContent = server.name || 'Servidor';
  if (el.serverNavCount) {
    const total = server.channels.length;
    el.serverNavCount.textContent = `${total} ${total === 1 ? 'canal' : 'canales'}`;
  }
}

// Fila raíz de un servidor/supergrupo: al hacer clic se entra a su lista de canales.
function renderServerContainer(server) {
  const item = document.createElement('div');
  item.className = `conversation-item is-group-parent ${state.activeServerId === server.id ? 'active' : ''}`;
  item.dataset.serverId = server.id;

  const timeStr = server.lastActivity
    ? new Date(server.lastActivity * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : '';

  const serverProvider = getProviderByManifestId(server.provider) || getProviderByEnum(server.provider);
  const serverIcon = serverProvider.icon;
  const serverColor = serverProvider.color || `var(--accent-${serverProvider.id}, #5865F2)`;

  const avatarContent = server.avatarUrl
    ? getAvatarHTML(server.avatarUrl, server.name || 'Servidor', '')
    : `<i class="${serverIcon}"></i>`;

  item.innerHTML = `
    <div class="conv-avatar" style="background:${serverColor};">
      ${avatarContent}
    </div>
    <div class="conv-content">
      <div class="conv-header-row">
        <div class="conv-title" style="color:var(--text-main);">
          <i class="fa-solid fa-chevron-right topic-chevron"></i>
          ${escapeHTML(server.name || 'Servidor')}
        </div>
        <div class="conv-time">${timeStr}</div>
      </div>
      <div class="conv-preview-row">
        <div class="conv-snippet">${server.channels.length} ${server.channels.length === 1 ? 'canal' : 'canales'}</div>
        ${server.unreadCount > 0 ? `<div class="unread-pill">${server.unreadCount}</div>` : ''}
      </div>
    </div>
  `;

  item.onclick = () => openServer(server.id);
  el.conversationList.appendChild(item);
}

// Vista interna del servidor: lista de canales agrupados por categoría.
function renderServerChannelsView(server) {
  updateServerNavHeader(server);
  el.conversationList.innerHTML = '';

  if (!server.channels || server.channels.length === 0) {
    el.conversationList.innerHTML = `<div class="loading-state">Este servidor no tiene canales disponibles.</div>`;
    return;
  }

  // Agrupar por categoría (parent_channel_id). La categoría idéntica al servidor no genera cabecera.
  const categories = new Map();
  server.channels.forEach(channel => {
    const key = channel.parent_channel_id && channel.parent_channel_id !== server.id
      ? channel.parent_channel_id
      : '__nocat__';
    if (!categories.has(key)) {
      categories.set(key, { key, channels: [] });
    }
    categories.get(key).channels.push(channel);
  });

  // El servidor dicta el orden: se usa la posición que publicó al sincronizar los canales.
  const positionOf = channel => (typeof channel.position === 'number' ? channel.position : 0);
  const hasServerOrder = server.channels.some(channel => positionOf(channel) > 0);

  const sortedKeys = Array.from(categories.keys()).sort((a, b) => {
    if (a === '__nocat__') return -1;
    if (b === '__nocat__') return 1;
    if (hasServerOrder) {
      const posA = Math.min(...categories.get(a).channels.map(positionOf));
      const posB = Math.min(...categories.get(b).channels.map(positionOf));
      if (posA !== posB) return posA - posB;
    }
    return humanizeCategory(a).localeCompare(humanizeCategory(b));
  });

  sortedKeys.forEach(key => {
    const category = categories.get(key);
    if (key !== '__nocat__') {
      const header = document.createElement('div');
      header.className = 'category-header';
      header.innerHTML = `<i class="fa-solid fa-folder-tree"></i> ${escapeHTML(humanizeCategory(key))}`;
      el.conversationList.appendChild(header);
    }
    category.channels
      .slice()
      .sort((a, b) => {
        if (hasServerOrder) {
          const posA = positionOf(a);
          const posB = positionOf(b);
          if (posA !== posB) return posA - posB;
        }
        return (b.last_message_at_unix || 0) - (a.last_message_at_unix || 0);
      })
      .forEach(channel => renderChannelItem(channel, false));
  });
}

// Entrar a un servidor/supergrupo (drill-down).
function openServer(serverId) {
  if (!serverId) return;
  state.activeServerId = serverId;
  state.lastConversationsHash = '';
  renderConversations();
}

// Volver a la lista unificada de chats.
function closeServer() {
  if (!state.activeServerId) return;
  state.activeServerId = null;
  state.lastConversationsHash = '';
  renderConversations();
}

function renderChannelItem(conv, isSubchannel) {
  const item = document.createElement('div');
  item.className = `conversation-item ${isSubchannel ? 'is-subchannel' : ''} ${conv.id === state.activeConversationId ? 'active' : ''}`;
  item.dataset.convId = conv.id;
  item.onclick = () => selectConversation(conv.id);

  const initial = (conv.title || 'C').charAt(0).toUpperCase();
  const timeStr = conv.last_message_at_unix 
    ? new Date(conv.last_message_at_unix * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) 
    : '';

  const provInfo = providerInfoForConversation(conv);
  const provBadge = provInfo.badge;
  const provIcon = provInfo.icon;
  const provStyle = providerBadgeStyleAttr(provInfo);
  const channelPrefix = channelTypePrefix(provInfo.id, conv);

  const fallbackIcon = conv.channel_type === 'voice' ? '🔊' : (conv.channel_type === 'text' ? '#' : '');
  const avatarContent = getAvatarHTML(conv.avatar_url, conv.title || 'Conversación', fallbackIcon);

  item.innerHTML = `
    <div class="conv-avatar">
      ${avatarContent}
      <div class="conv-badge-provider ${provBadge}"${provStyle}><i class="${provIcon}"></i></div>
    </div>
    <div class="conv-content">
      <div class="conv-header-row">
        <div class="conv-title">${channelPrefix}${escapeHTML(conv.title || 'Conversación')}</div>
        <div class="conv-time">${timeStr}</div>
      </div>
      <div class="conv-preview-row">
        <div class="conv-snippet">${escapeHTML(conv.last_message_preview || 'Sin mensajes aún')}</div>
        ${conv.unread_count > 0 ? `<div class="unread-pill">${conv.unread_count}</div>` : ''}
      </div>
    </div>
  `;
  el.conversationList.appendChild(item);
}

function showLoadingBar() {
  if (el.chatProgressBar) el.chatProgressBar.classList.remove('hidden');
}

function hideLoadingBar() {
  if (el.chatProgressBar) el.chatProgressBar.classList.add('hidden');
}

// Estado para controlar la paginación infinita y botón de scroll
let isLoadingOlderMessages = false;
let hasReachedOldestMessage = false;
let unreadBelowCount = 0;

function isNearBottom(threshold = 120) {
  if (!el.messagesContainer) return true;
  const distance = el.messagesContainer.scrollHeight - el.messagesContainer.scrollTop - el.messagesContainer.clientHeight;
  return distance <= threshold;
}

function updateScrollBottomButton() {
  if (!el.btnScrollBottom) return;
  const atBottom = isNearBottom(120);

  if (atBottom) {
    el.btnScrollBottom.classList.add('hidden');
    unreadBelowCount = 0;
    if (el.scrollBottomBadge) {
      el.scrollBottomBadge.classList.add('hidden');
      el.scrollBottomBadge.textContent = '0';
    }
  } else {
    el.btnScrollBottom.classList.remove('hidden');
    if (el.scrollBottomBadge) {
      if (unreadBelowCount > 0) {
        el.scrollBottomBadge.textContent = unreadBelowCount > 99 ? '99+' : unreadBelowCount;
        el.scrollBottomBadge.classList.remove('hidden');
      } else {
        el.scrollBottomBadge.classList.add('hidden');
      }
    }
  }
}

// Indica si el usuario está viendo la app: ventana con foco y documento visible.
// Defensivo: si las APIs no existen se asume que sí la está viendo.
function isUserViewingApp() {
  try {
    if (typeof document === 'undefined') return true;
    if (typeof document.hasFocus === 'function' && !document.hasFocus()) return false;
    if (document.visibilityState === 'hidden') return false;
  } catch (e) {
    return true;
  }
  return true;
}

// Marcar como leídos los mensajes de la conversación activa en memoria y conduitd
function markActiveConversationAsRead(convId) {
  if (!convId) return;
  
  // 1. Limpiar estado local del contador de la conversación
  const conv = state.conversations.find(c => c.id === convId);
  if (conv) {
    conv.unread_count = 0;
  }
  updateActiveConversationVisual();

  // 2. Notificar a conduitd para marcar mensajes como 'read' en SQLite y en la plataforma remota (WhatsApp / Telegram)
  if (window.conduitAPI.markConversationRead) {
    window.conduitAPI.markConversationRead(convId).catch(() => {});
  } else {
    window.conduitAPI.getMessages(convId, 1, '').catch(() => {});
  }
}

// Determina el nombre, badge y clase del proveedor de un canal de contacto
function providerInfoForChannel(channel) {
  const info = channel ? getProviderByEnum(channel.provider) : defaultProvider();
  return { kind: info.id, name: info.name, badge: info.badge, badgeStyle: info.badgeStyle, icon: info.icon, color: info.color };
}

// Determina el proveedor de un mensaje, respetando el canal por el que entró o salió
function providerInfoForMessage(msg, currentConv) {
  if (msg && msg.channel_id && state.contacts) {
    for (const c of state.contacts) {
      if (c.channels) {
        const found = c.channels.find(ch => ch.id === msg.channel_id);
        if (found) {
          return providerInfoForChannel(found);
        }
      }
    }
  }

  const cId = (msg && msg.conversation_id) || (currentConv ? currentConv.id : '');
  const hinted = getProviderByConversationId(cId) || getProviderByServerName(currentConv && currentConv.server_name);
  if (hinted) return providerInfoForChannel({ provider: hinted.id });

  if (currentConv && state.contacts) {
    const contact = state.contacts.find(c => c.id === currentConv.contact_id || c.id === currentConv.id);
    if (contact && contact.channels && contact.channels.length === 1) {
      return providerInfoForChannel(contact.channels[0]);
    }
  }

  return providerInfoForChannel(null);
}

// Actualiza el dropdown de canales en el compositor según el contacto activo
function updateChannelSelector(conv) {
  if (!el.channelSelect) return;

  const wrapper = el.channelSelectorWrapper || document.getElementById('channel-selector-wrapper');
  if (!conv) {
    if (wrapper) wrapper.style.display = 'none';
    el.channelSelect.innerHTML = '';
    const defOpt = document.createElement('option');
    defOpt.value = '';
    defOpt.textContent = 'Automático (último usado)';
    el.channelSelect.appendChild(defOpt);
    el.channelSelect.value = '';
    return;
  }

  const contact = (state.contacts || []).find(c => c.id === conv.contact_id || c.id === conv.id);
  const channels = (contact && Array.isArray(contact.channels)) ? contact.channels : [];

  if (channels.length <= 1) {
    if (wrapper) wrapper.style.display = 'none';
    el.channelSelect.innerHTML = '';
    const defOpt = document.createElement('option');
    defOpt.value = '';
    defOpt.textContent = 'Automático';
    el.channelSelect.appendChild(defOpt);
    el.channelSelect.value = '';
    return;
  }

  if (wrapper) wrapper.style.display = '';

  const activeId = conv.active_channel_id || '';
  el.channelSelect.innerHTML = '';

  const defaultOption = document.createElement('option');
  defaultOption.value = '';
  defaultOption.textContent = 'Automático (último usado)';
  el.channelSelect.appendChild(defaultOption);

  for (const ch of channels) {
    const info = providerInfoForChannel(ch);
    const handleText = ch.handle || ch.remote_id || ch.id;
    const isLastUsed = ch.id === activeId ? ' (activo)' : '';
    const opt = document.createElement('option');
    opt.value = ch.id;
    opt.textContent = `${info.name} · ${handleText}${isLastUsed}`;
    el.channelSelect.appendChild(opt);
  }

  el.channelSelect.value = '';
}

// Seleccionar conversación y cargar mensajes
async function selectConversation(convId) {
  if (state.activeConversationId === convId && el.messagesContainer.querySelector('.message-bubble')) {
    return; // Ya está seleccionada y renderizada
  }

  state.activeConversationId = convId;
  localStorage.setItem('conduit_active_conversation_id', convId);
  hasReachedOldestMessage = false;
  unreadBelowCount = 0;
  updateScrollBottomButton();
  updateActiveConversationVisual();
  showView('view-chat');
  clearReply();
  cancelEditMessage();
  if (state.audioRecorder && state.audioRecorder.isRecording) {
    cancelVoiceRecording();
  }

  const conv = state.conversations.find(c => c.id === convId);
  if (conv) {
    el.activeChatTitle.textContent = conv.title || 'Conversación';
    const fallbackIcon = conv.channel_type === 'voice' ? '🔊' : (conv.channel_type === 'text' ? '#' : '');
    el.activeChatAvatar.innerHTML = getAvatarHTML(conv.avatar_url, conv.title || 'Conversación', fallbackIcon);
    el.activeChatSubtitle.textContent = `ID: ${conv.id.substring(0, 8)}... | Activo en conduitd`;
  }
  updateChannelSelector(conv);

  if (window.conduitAPI && typeof window.conduitAPI.getPinnedMessages === 'function') {
    try {
      const pinnedMsgs = await window.conduitAPI.getPinnedMessages(convId);
      state.pinnedMessages.set(convId, pinnedMsgs || []);
      if (state.activeConversationId === convId) {
        updatePinnedBanner();
      }
    } catch (e) {
      console.error('Error fetching pinned messages:', e);
    }
  }

  // 1. Si los mensajes ya están en caché local, renderizarlos de inmediato sin esperar la red
  const cached = state.messageCache.get(convId);
  if (cached && cached.messages && cached.messages.length > 0) {
    renderMessages(cached.messages);
    hideLoadingBar();
    markActiveConversationAsRead(convId);
    // Continuar en segundo plano para verificar si llegaron nuevos mensajes
  } else {
    // 2. Si no están en caché, mostrar estado de carga inicial
    el.messagesContainer.innerHTML = '<div class="loading-state"><i class="fa-solid fa-spinner fa-spin"></i> Cargando mensajes...</div>';
    showLoadingBar();
  }

  try {
    const timeoutPromise = new Promise((_, reject) => 
      setTimeout(() => reject(new Error('Tiempo de espera agotado al consultar conduitd')), 6000)
    );
    
    // Traer los 30 mensajes más recientes
    const messages = await Promise.race([
      window.conduitAPI.getMessages(convId, 30, ''),
      timeoutPromise
    ]);
    let msgList = messages || [];

    // Primera carga de un chat sin mensajes locales: el core le está pidiendo la
    // última página al proveedor, así que se reintenta antes de mostrar el vacío.
    if (msgList.length === 0) {
      msgList = await fetchPageWithRetry(convId, '', 2, 1400);
    }

    // Guardar en caché cliente
    state.messageCache.set(convId, {
      messages: msgList,
      lastFetch: Date.now()
    });

    // Renderizar si el usuario sigue en esta misma conversación
    if (state.activeConversationId === convId) {
      renderMessages(msgList);
      markActiveConversationAsRead(convId);
    }
  } catch (err) {
    console.error('Error selectConversation:', err);
    if (state.activeConversationId === convId) {
      el.messagesContainer.innerHTML = `<div class="loading-state" style="color:var(--danger)">No se pudieron cargar los mensajes: ${err.message}</div>`;
    }
  } finally {
    hideLoadingBar();
  }
}

// El proveedor entrega el historial de forma asíncrona: una página vacía puede
// significar "todavía no llegó", no "no hay más". Reintenta antes de rendirse.
async function fetchPageWithRetry(convId, beforeId, attempts = 3, waitMs = 1400) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    const page = await window.conduitAPI.getMessages(convId, 30, beforeId);
    if (page && page.length > 0) return page;
    if (attempt < attempts - 1) {
      await new Promise(resolve => setTimeout(resolve, waitMs));
    }
  }
  return [];
}

// Cargar mensajes históricos más viejos al hacer scroll hacia arriba (Paginación Infinita)
async function loadOlderMessages() {
  if (isLoadingOlderMessages || hasReachedOldestMessage || !state.activeConversationId) return;

  const cached = state.messageCache.get(state.activeConversationId);
  if (!cached || !cached.messages || cached.messages.length === 0) return;

  // Ordenar para encontrar el mensaje más viejo actualmente cargado
  const sorted = [...cached.messages].sort((a, b) => (Number(a.sent_at_unix) || 0) - (Number(b.sent_at_unix) || 0));
  const oldestMsg = sorted[0];
  if (!oldestMsg) return;

  isLoadingOlderMessages = true;
  showLoadingBar();

  try {
    const olderMsgs = await fetchPageWithRetry(state.activeConversationId, oldestMsg.id || oldestMsg.remote_message_id);
    if (!olderMsgs || olderMsgs.length === 0) {
      hasReachedOldestMessage = true;
      return;
    }

    // Unir mensajes viejos evitando duplicados
    const existingIds = new Set(cached.messages.map(m => m.id));
    olderMsgs.forEach(m => {
      if (!existingIds.has(m.id)) {
        cached.messages.push(m);
      }
    });

    // Guardar altura previa para mantener la posición del scroll
    const prevScrollHeight = el.messagesContainer.scrollHeight;
    const prevScrollTop = el.messagesContainer.scrollTop;

    // Renderizar lista completa unificada sin autoscroll
    renderMessages(cached.messages, false);

    // Ajustar scroll para que la pantalla mantenga la posición visual exacta
    const newScrollHeight = el.messagesContainer.scrollHeight;
    el.messagesContainer.scrollTop = prevScrollTop + (newScrollHeight - prevScrollHeight);
    updateScrollBottomButton();
  } catch (err) {
    console.error('Error loadOlderMessages:', err);
  } finally {
    isLoadingOlderMessages = false;
    hideLoadingBar();
  }
}

// Actualizar clase 'active' en el sidebar y limpiar visualmente el badge de no leídos
function updateActiveConversationVisual() {
  const items = el.conversationList.querySelectorAll('.conversation-item');
  items.forEach(it => {
    if (it.dataset.convId === state.activeConversationId) {
      it.classList.add('active');
      const unreadBadge = it.querySelector('.unread-pill');
      if (unreadBadge) {
        unreadBadge.remove(); // Quitar de inmediato la píldora de no leídos al abrir el chat
      }
    } else {
      it.classList.remove('active');
    }
  });
}

function formatDateDivider(unixSeconds) {
  if (!unixSeconds) return 'Mensajes';
  const d = new Date(unixSeconds * 1000);
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const msgDay = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  
  const diffTime = today.getTime() - msgDay.getTime();
  const diffDays = Math.round(diffTime / (1000 * 3600 * 24));

  if (diffDays === 0) return 'Hoy';
  if (diffDays === 1) return 'Ayer';
  if (diffDays < 7 && diffDays > 1) {
    return d.toLocaleDateString([], { weekday: 'long' });
  }
  return d.toLocaleDateString([], { day: 'numeric', month: 'short', year: d.getFullYear() !== now.getFullYear() ? 'numeric' : undefined });
}

function renderMessages(messages, autoScroll = true) {
  if (!messages || messages.length === 0) {
    el.messagesContainer.innerHTML = `
      <div class="empty-conversation-state">
        <i class="fa-regular fa-message empty-icon"></i>
        <h3>Sin mensajes</h3>
        <p>Inicia la conversación enviando un mensaje a través de conduitd.</p>
      </div>`;
    updatePinnedBanner();
    return;
  }

  el.messagesContainer.innerHTML = '';
  // Orden cronológico: el mensaje más viejo arriba y el más nuevo abajo de todo
  const sorted = [...messages].sort((a, b) => (Number(a.sent_at_unix) || 0) - (Number(b.sent_at_unix) || 0));

  let lastDateKey = null;

  sorted.forEach(msg => {
    // Agrupar e insertar divisor de fecha si cambió el día
    if (msg.sent_at_unix) {
      const msgDate = new Date(msg.sent_at_unix * 1000);
      const dateKey = `${msgDate.getFullYear()}-${msgDate.getMonth()}-${msgDate.getDate()}`;
      if (dateKey !== lastDateKey) {
        lastDateKey = dateKey;
        const divider = document.createElement('div');
        divider.className = 'message-date-divider';
        divider.innerHTML = `<span class="date-pill">${formatDateDivider(msg.sent_at_unix)}</span>`;
        el.messagesContainer.appendChild(divider);
      }
    }

    appendMessageBubble(msg);
  });

  if (autoScroll) {
    scrollMessagesToBottom();
  }
  updatePinnedBanner();
}

// Detecta si un mensaje está compuesto únicamente por 1 a 3 emojis (para renderizarlo más grande con animación)
function isOnlyEmojis(text) {
  if (!text || typeof text !== 'string') return false;
  const trimmed = text.trim();
  if (!trimmed) return false;
  const emojiRegex = /^(?:\s*(?:\p{Extended_Pictographic}|\p{Emoji_Presentation})(?:\uFE0F|\u200D(?:\p{Extended_Pictographic}|\p{Emoji_Presentation})*|\p{Emoji_Modifier})*\s*){1,3}$/u;
  return emojiRegex.test(trimmed);
}

function appendMessageBubble(msg) {
  if (!msg) return;

  // 1. Deduplicación por id o remote_message_id en el DOM
  if (msg.id && el.messagesContainer.querySelector(`.message-row[data-msg-id="${msg.id}"]`)) {
    return;
  }
  if (msg.remote_message_id && el.messagesContainer.querySelector(`.message-row[data-remote-id="${msg.remote_message_id}"]`)) {
    return;
  }

  const isOutgoing = msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me';

  // 2. Si es mensaje saliente ('me'), verificar si el último mensaje saliente en el DOM tiene el mismo contenido
  // para actualizar sus identificadores en vez de agregar una segunda burbuja idéntica
  if (isOutgoing) {
    const existingOutgoingRows = el.messagesContainer.querySelectorAll('.message-row.outgoing');
    if (existingOutgoingRows.length > 0) {
      const lastRow = existingOutgoingRows[existingOutgoingRows.length - 1];
      const bodyEl = lastRow.querySelector('.msg-body');
      if (bodyEl && bodyEl.textContent.trim() === (msg.body || '').trim()) {
        if (msg.id) lastRow.dataset.msgId = msg.id;
        if (msg.remote_message_id) lastRow.dataset.remoteId = msg.remote_message_id;
        return;
      }
    }
  }

  const row = document.createElement('div');
  row.className = `message-row ${isOutgoing ? 'outgoing' : 'incoming'}`;
  if (msg.id) row.dataset.msgId = msg.id;
  if (msg.remote_message_id) row.dataset.remoteId = msg.remote_message_id;

  const timeStr = msg.sent_at_unix 
    ? new Date(msg.sent_at_unix * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) 
    : '';

  // Determinar datos de la conversación activa para los mensajes entrantes
  const currentConv = state.conversations.find(c => c.id === state.activeConversationId);
  const convTitle = (currentConv && currentConv.title) ? currentConv.title : 'Contacto';

  // Nombre de quien envió el mensaje
  let senderName = '';
  if (!isOutgoing) {
    if (msg.sender_type && msg.sender_type !== 'contact' && msg.sender_type !== 'Remoto') {
      senderName = msg.sender_type; // Nombre del participante en grupos
    } else {
      senderName = convTitle; // Nombre del contacto en chats individuales
    }
  }

  // Avatar e inicial
  let msgAvatarHTML = '';
  if (isOutgoing) {
    const myAccount = state.accounts[0];
    const myName = (myAccount && (myAccount.label || myAccount.identifier)) ? (myAccount.label || myAccount.identifier) : 'YO';
    let myAvatar = '';
    const myContact = findMyAccountContact(myAccount);
    if (myContact && myContact.avatar_url) {
      myAvatar = myContact.avatar_url;
    }
    msgAvatarHTML = getAvatarHTML(myAvatar, myName);
  } else {
    let avatarUrl = '';
    if (currentConv && currentConv.is_group) {
      // En grupos: buscar el avatar del participante remitente en state.contacts
      if (senderName && state.contacts && state.contacts.length > 0) {
        const cleanSender = senderName.trim().toLowerCase();
        const contactMatch = state.contacts.find(c => {
          if (!c.display_name) return false;
          const cName = c.display_name.trim().toLowerCase();
          return cName === cleanSender || cName.startsWith(cleanSender) || cleanSender.startsWith(cName);
        });
        if (contactMatch && contactMatch.avatar_url) {
          avatarUrl = contactMatch.avatar_url;
        }
      }
    } else if (currentConv) {
      // En chats 1-a-1: usar el avatar de la conversación/contacto
      avatarUrl = currentConv.avatar_url || '';
    }
    msgAvatarHTML = getAvatarHTML(avatarUrl, senderName || 'Contacto');
  }

  // Proveedor asociado (badge sobre el avatar)
  const prov = providerInfoForMessage(msg, currentConv);
  const provBadge = prov.badge;
  const provIcon = prov.icon;
  const provStyle = providerBadgeStyleAttr(prov);

  const avatarHTML = `
    <div class="msg-avatar" style="${isOutgoing ? 'background:#2b5278;' : 'background:#4752c4;'}">
      ${msgAvatarHTML}
      <div class="msg-avatar-badge ${provBadge}"${provStyle}><i class="${provIcon}"></i></div>
    </div>
  `;

  // Renderizar Adjuntos Multimedia (Fotos, Audios, Documentos, Videos, GIFs)
  let attachmentsHTML = '';
  if (msg.attachments && msg.attachments.length > 0) {
    if (msg.id) {
      attachmentSigByMessage.set(msg.id, attachmentsSignature(msg.attachments));
      attachmentsByMessage.set(msg.id, msg.attachments);
    }
    attachmentsHTML = `<div class="msg-attachments">` + msg.attachments.map(att => renderAttachmentHTML(att)).join('') + `</div>`;
  }

  // Renderizar Encuesta (Poll)
  let pollHTML = '';
  if (msg.poll) {
    pollHTML = renderPollHTML(msg.poll, msg.id);
  }

  // Renderizar Botones Interactivos (Inline Keyboards & Quick Replies)
  let buttonsHTML = '';
  if (msg.button_rows && msg.button_rows.length > 0) {
    buttonsHTML = renderButtonsHTML(msg.button_rows, msg.id);
  }

  // Renderizar Tarjeta de Ubicación
  let locationHTML = '';
  if (msg.location) {
    const lat = Number(msg.location.latitude) || 0;
    const lon = Number(msg.location.longitude) || 0;
    const mapURL = `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lon}#map=16/${lat}/${lon}`;
    locationHTML = `
      <div class="msg-location-card">
        <div class="location-header"><i class="fa-solid fa-location-dot"></i> ${escapeHTML(msg.location.title || 'Ubicación')}</div>
        ${msg.location.address ? `<div class="location-address">${escapeHTML(msg.location.address)}</div>` : ''}
        <div class="location-coords">${lat.toFixed(6)}, ${lon.toFixed(6)}</div>
        <a class="btn-open-map" href="${escapeHTML(mapURL)}" target="_blank" data-url="${escapeHTML(mapURL)}">
          <span>Ver en mapa</span> <i class="fa-solid fa-arrow-up-right-from-square"></i>
        </a>
      </div>
    `;
  }

  // Renderizar Tarjeta de Contacto Compartido
  let contactCardHTML = '';
  if (msg.shared_contact) {
    contactCardHTML = `
      <div class="msg-contact-card">
        <div class="contact-card-icon"><i class="fa-solid fa-user"></i></div>
        <div class="contact-card-info">
          <span class="contact-card-name">${escapeHTML(msg.shared_contact.display_name || 'Contacto')}</span>
          <span class="contact-card-phone">${escapeHTML(msg.shared_contact.phone_number || '')}</span>
        </div>
      </div>
    `;
  }

  // Renderizar Tarjeta de Video Social (YouTube, TikTok, Instagram)
  let socialVideoHTML = '';
  if (msg.body && !msg.poll && !msg.location && !msg.shared_contact) {
    const social = parseSocialVideoLink(msg.body);
    if (social) {
      socialVideoHTML = renderSocialVideoCardHTML(social);
    }
  }

  // Renderizar Mensaje Citado (Quote / Reply)
  let quoteHTML = '';
  if (msg.reply_to_message_id) {
    quoteHTML = `
      <div class="msg-quoted-reply" data-reply-to-id="${escapeHTML(msg.reply_to_message_id)}">
        <span class="quoted-sender">${escapeHTML(msg.reply_to_sender || 'Mensaje citado')}</span>
        <span class="quoted-text">${escapeHTML(msg.reply_to_body || '')}</span>
      </div>
    `;
  }

  // Renderizar Reacciones en burbuja
  let reactionsHTML = '';
  if (msg.reactions && msg.reactions.length > 0) {
    reactionsHTML = `<div class="msg-reactions">` + msg.reactions.map(r => `
      <button class="reaction-pill ${r.includes_me ? 'active' : ''}" data-msg-id="${escapeHTML(msg.id || '')}" data-emoji="${escapeHTML(r.emoji)}" title="${escapeHTML((r.senders || []).join(', '))}">
        <span class="reaction-emoji">${escapeHTML(r.emoji)}</span>
        <span class="reaction-count">${r.count}</span>
      </button>
    `).join('') + `</div>`;
  }

  // Barra de acciones al pasar sobre el mensaje (Responder / Reaccionar / Fijar / Editar / Eliminar)
  const actionsHTML = `
    <div class="msg-action-toolbar">
      <button class="msg-action-btn btn-action-reply" title="Responder"><i class="fa-solid fa-reply"></i></button>
      <button class="msg-action-btn btn-action-forward" title="Reenviar"><i class="fa-solid fa-share"></i></button>
      <button class="msg-action-btn btn-action-react" title="Reaccionar"><i class="fa-regular fa-face-smile"></i></button>
      <button class="msg-action-btn btn-action-pin" title="${msg.is_pinned ? 'Desfijar' : 'Fijar'}"><i class="fa-solid fa-thumbtack ${msg.is_pinned ? 'pinned-active' : ''}"></i></button>
      ${isOutgoing ? `<button class="msg-action-btn btn-action-edit" title="Editar"><i class="fa-solid fa-pen"></i></button>` : ''}
      ${isOutgoing ? `<button class="msg-action-btn btn-action-delete" title="Eliminar"><i class="fa-solid fa-trash"></i></button>` : ''}
      <div class="quick-reaction-popup hidden">
        <span class="quick-emoji" data-emoji="👍">👍</span>
        <span class="quick-emoji" data-emoji="❤️">❤️</span>
        <span class="quick-emoji" data-emoji="😂">😂</span>
        <span class="quick-emoji" data-emoji="😮">😮</span>
        <span class="quick-emoji" data-emoji="😢">😢</span>
        <span class="quick-emoji" data-emoji="🙏">🙏</span>
        <span class="quick-emoji" data-emoji="🔥">🔥</span>
      </div>
    </div>
  `;

  const isJumbo = isOnlyEmojis(msg.body);

  const bubbleHTML = `
    <div class="message-bubble ${isOutgoing ? 'outgoing' : 'incoming'}">
      ${actionsHTML}
      ${msg.is_pinned ? `<div class="msg-pinned-indicator"><i class="fa-solid fa-thumbtack"></i> Fijado</div>` : ''}
      ${!isOutgoing ? `<div class="msg-sender">${escapeHTML(senderName)}</div>` : ''}
      ${quoteHTML}
      ${attachmentsHTML}
      ${locationHTML}
      ${contactCardHTML}
      ${pollHTML}
      ${msg.body && !msg.poll && !msg.location && !msg.shared_contact ? `<div class="msg-body ${isJumbo ? 'emoji-jumbo' : ''}">${escapeHTML(msg.body)}</div>` : ''}
      ${socialVideoHTML}
      ${buttonsHTML}
      <div class="msg-meta">
        <span>${timeStr}</span>
        ${msg.is_edited ? '<span class="msg-edited-badge">(editado)</span>' : ''}
        ${isOutgoing ? `<i class="fa-solid fa-check-double" style="font-size:10px;"></i>` : ''}
      </div>
      ${reactionsHTML}
    </div>
  `;

  if (isOutgoing) {
    row.innerHTML = bubbleHTML + avatarHTML;
  } else {
    row.innerHTML = avatarHTML + bubbleHTML;
  }

  // Asignar listeners interactivos a la burbuja
  const btnReply = row.querySelector('.btn-action-reply');
  if (btnReply) {
    btnReply.addEventListener('click', () => setReplyTarget(msg));
  }

  const btnForward = row.querySelector('.btn-action-forward');
  if (btnForward) {
    btnForward.addEventListener('click', () => openForwardModal(msg));
  }

  const btnPin = row.querySelector('.btn-action-pin');
  if (btnPin) {
    btnPin.addEventListener('click', () => handlePinMessage(msg.id, !msg.is_pinned));
  }

  const btnEdit = row.querySelector('.btn-action-edit');
  if (btnEdit) {
    btnEdit.addEventListener('click', () => startEditMessage(msg));
  }

  const btnDelete = row.querySelector('.btn-action-delete');
  if (btnDelete) {
    btnDelete.addEventListener('click', () => handleDeleteMessage(msg.id));
  }

  const btnReact = row.querySelector('.btn-action-react');
  const popup = row.querySelector('.quick-reaction-popup');
  if (btnReact && popup) {
    btnReact.addEventListener('click', (e) => {
      e.stopPropagation();
      if (el.messagesContainer) {
        el.messagesContainer.querySelectorAll('.quick-reaction-popup:not(.hidden)').forEach(p => {
          if (p !== popup) p.classList.add('hidden');
        });
      }
      popup.classList.toggle('hidden');
    });
  }

  const emojis = row.querySelectorAll('.quick-emoji');
  emojis.forEach(em => {
    em.addEventListener('click', (e) => {
      e.stopPropagation();
      em.classList.add('emoji-pop');
      if (popup) popup.classList.add('hidden');
      const emChar = em.dataset.emoji;
      if (emChar && msg.id) toggleReaction(msg.id, emChar);
    });
  });

  attachReactionListeners(row.querySelector('.msg-reactions'), msg.id);
  attachPollListeners(row.querySelector('.msg-poll-card'), msg.id);
  attachButtonListeners(row.querySelector('.msg-buttons-grid'), msg.id);

  const quoteEl = row.querySelector('.msg-quoted-reply');
  if (quoteEl) {
    quoteEl.addEventListener('click', () => {
      const parentId = quoteEl.dataset.replyToId;
      if (parentId && el.messagesContainer) {
        const targetRow = el.messagesContainer.querySelector(`.message-row[data-msg-id="${parentId}"]`);
        if (targetRow && typeof targetRow.scrollIntoView === 'function') {
          targetRow.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }
      }
    });
  }

  el.messagesContainer.appendChild(row);
}

function scrollMessagesToBottom(smooth = false) {
  if (!el.messagesContainer) return;
  if (smooth) {
    el.messagesContainer.scrollTo({
      top: el.messagesContainer.scrollHeight,
      behavior: 'smooth'
    });
  } else {
    el.messagesContainer.scrollTop = el.messagesContainer.scrollHeight;
  }
  unreadBelowCount = 0;
  updateScrollBottomButton();
}

// ==========================================================================
// Quotes & Reactions Management
// ==========================================================================

function setReplyTarget(msg) {
  if (!msg) return;
  const isOutgoing = msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me';
  let senderName = 'Contacto';
  if (isOutgoing) {
    senderName = 'Tú';
  } else if (msg.sender_type && msg.sender_type !== 'contact' && msg.sender_type !== 'Remoto') {
    senderName = msg.sender_type;
  } else {
    const currentConv = state.conversations.find(c => c.id === state.activeConversationId);
    if (currentConv && currentConv.title) senderName = currentConv.title;
  }

  state.activeReply = {
    id: msg.id,
    remote_id: msg.remote_message_id,
    sender: senderName,
    body: msg.body || (msg.attachments && msg.attachments.length > 0 ? 'Archivo adjunto' : '')
  };

  if (el.composerReplyPreview) {
    if (el.replyPreviewSender) el.replyPreviewSender.textContent = senderName;
    if (el.replyPreviewText) el.replyPreviewText.textContent = state.activeReply.body || '';
    el.composerReplyPreview.classList.remove('hidden');
  }
  if (el.messageInput) {
    el.messageInput.focus();
  }
}

function clearReply() {
  state.activeReply = null;
  if (el.composerReplyPreview) {
    el.composerReplyPreview.classList.add('hidden');
    if (el.replyPreviewSender) el.replyPreviewSender.textContent = '';
    if (el.replyPreviewText) el.replyPreviewText.textContent = '';
  }
}

async function toggleReaction(messageId, emoji) {
  if (!messageId || !emoji) return;
  try {
    const res = await window.conduitAPI.sendReaction(messageId, emoji);
    if (res && res.reactions) {
      if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
        const cached = state.messageCache.get(state.activeConversationId);
        if (cached && cached.messages) {
          const m = cached.messages.find(x => x.id === messageId || x.remote_message_id === messageId);
          if (m) m.reactions = res.reactions;
        }
      }
      updateMessageReactionsInDOM(messageId, res.reactions);
    }
  } catch (err) {
    console.error('Error toggling reaction:', err);
  }
}

function updateMessageReactionsInDOM(messageId, reactions) {
  if (!el.messagesContainer || !messageId) return;
  const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${messageId}"]`)
    || el.messagesContainer.querySelector(`.message-row[data-remote-id="${messageId}"]`);
  if (!row) return;
  let container = row.querySelector('.msg-reactions');
  if (!reactions || reactions.length === 0) {
    if (container) container.remove();
    return;
  }
  if (!container) {
    container = document.createElement('div');
    container.className = 'msg-reactions';
    const bubble = row.querySelector('.message-bubble');
    if (bubble) bubble.appendChild(container);
  }
  container.innerHTML = reactions.map(r => `
    <button class="reaction-pill ${r.includes_me ? 'active' : ''}" data-msg-id="${escapeHTML(messageId)}" data-emoji="${escapeHTML(r.emoji)}" title="${escapeHTML((r.senders || []).join(', '))}">
      <span class="reaction-emoji">${escapeHTML(r.emoji)}</span>
      <span class="reaction-count">${r.count}</span>
    </button>
  `).join('');
  attachReactionListeners(container, messageId);
}

function attachReactionListeners(container, messageId) {
  if (!container) return;
  const pills = container.querySelectorAll('.reaction-pill');
  pills.forEach(pill => {
    pill.addEventListener('click', () => {
      const emoji = pill.dataset.emoji;
      if (emoji) toggleReaction(messageId, emoji);
    });
  });
}

// ==========================================================================
// Polls Management
// ==========================================================================

function renderPollHTML(poll, messageId) {
  if (!poll) return '';
  const totalVotes = Number(poll.total_votes) || 0;
  const isClosed = Boolean(poll.is_closed);
  const isMultiple = (Number(poll.selectable_count) || 1) > 1;

  const optionsHTML = (poll.options || []).map(opt => {
    const vCount = Number(opt.vote_count) || 0;
    const percent = totalVotes > 0 ? Math.round((vCount / totalVotes) * 100) : 0;
    const isChosen = Boolean(opt.chosen);
    return `
      <div class="poll-option-row ${isChosen ? 'chosen' : ''}" data-msg-id="${escapeHTML(messageId || '')}" data-opt-id="${escapeHTML(opt.id || opt.text)}">
        <div class="poll-option-bar" style="width: ${percent}%;"></div>
        <div class="poll-option-content">
          <div class="poll-option-left">
            <span class="poll-option-indicator"></span>
            <span class="poll-option-text">${escapeHTML(opt.text)}</span>
          </div>
          <span class="poll-option-percent">${percent}% (${vCount})</span>
        </div>
      </div>
    `;
  }).join('');

  return `
    <div class="msg-poll-card" data-msg-id="${escapeHTML(messageId || '')}">
      <div class="poll-question">${escapeHTML(poll.question)}</div>
      <div class="poll-options">${optionsHTML}</div>
      <div class="poll-footer">
        <span>${totalVotes} ${totalVotes === 1 ? 'voto' : 'votos'}${isMultiple ? ' • Opción múltiple' : ''}</span>
        ${isClosed ? `<span class="poll-closed-badge">Encuesta cerrada</span>` : ''}
      </div>
    </div>
  `;
}

async function handleVotePoll(messageId, optionId) {
  if (!messageId || optionId === undefined) return;
  try {
    const res = await window.conduitAPI.sendVote(messageId, [String(optionId)]);
    if (res && res.poll) {
      if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
        const cached = state.messageCache.get(state.activeConversationId);
        if (cached && cached.messages) {
          const m = cached.messages.find(x => x.id === messageId || x.remote_message_id === messageId);
          if (m) m.poll = res.poll;
        }
      }
      updatePollInDOM(messageId, res.poll);
    }
  } catch (err) {
    console.error('Error votando en encuesta:', err);
  }
}

function updatePollInDOM(messageId, poll) {
  if (!el.messagesContainer || !poll) return;
  const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${messageId}"]`);
  if (!row) return;
  const existingCard = row.querySelector('.msg-poll-card');
  if (existingCard) {
    const wrapper = document.createElement('div');
    wrapper.innerHTML = renderPollHTML(poll, messageId);
    const newCard = wrapper.firstElementChild;
    existingCard.replaceWith(newCard);
    attachPollListeners(newCard, messageId);
  }
}

function attachPollListeners(card, messageId) {
  if (!card) return;
  const optionRows = card.querySelectorAll ? card.querySelectorAll('.poll-option-row') : [];
  optionRows.forEach(optRow => {
    optRow.addEventListener('click', () => {
      const optId = optRow.dataset.optId;
      if (optId !== undefined) handleVotePoll(messageId, optId);
    });
  });
}

// ==========================================================================
// Interactive Buttons Management (Inline Keyboards & Quick Replies)
// ==========================================================================

function renderButtonsHTML(buttonRows, messageId) {
  if (!buttonRows || buttonRows.length === 0) return '';
  const rowsHTML = buttonRows.map(row => {
    const btnsHTML = (row.buttons || []).map(btn => {
      const isUrl = btn.type === 'url' && btn.url;
      const icon = isUrl ? '<i class="fa-solid fa-arrow-up-right-from-square"></i>' : '';
      return `
        <button class="interactive-btn ${isUrl ? 'url-btn' : ''}" 
                data-msg-id="${escapeHTML(messageId || '')}" 
                data-btn-id="${escapeHTML(btn.id || '')}"
                data-btn-type="${escapeHTML(btn.type || 'callback')}"
                data-btn-text="${escapeHTML(btn.text || '')}"
                ${isUrl ? `data-url="${escapeHTML(btn.url)}"` : ''}>
          <span>${escapeHTML(btn.text)}</span>
          ${icon}
        </button>
      `;
    }).join('');
    return `<div class="msg-button-row">${btnsHTML}</div>`;
  }).join('');

  return `<div class="msg-buttons-grid" data-msg-id="${escapeHTML(messageId || '')}">${rowsHTML}</div>`;
}

async function handleClickButton(messageId, buttonId, buttonText, buttonType, url) {
  if (buttonType === 'url' && url) {
    if (window.conduitAPI && typeof window.conduitAPI.openPath === 'function') {
      window.conduitAPI.openPath(url);
    } else {
      window.open(url, '_blank');
    }
    return;
  }

  if (!messageId || !buttonId) return;
  try {
    const res = await window.conduitAPI.clickButton(messageId, buttonId, buttonText);
    if (res && res.feedback_message) {
      if (res.is_alert) {
        alert(res.feedback_message);
      } else {
        console.log('Bot feedback:', res.feedback_message);
      }
    }
    if (res && res.url) {
      if (window.conduitAPI && typeof window.conduitAPI.openPath === 'function') {
        window.conduitAPI.openPath(res.url);
      } else {
        window.open(res.url, '_blank');
      }
    }
  } catch (err) {
    console.error('Error al clickear botón interactivo:', err);
  }
}

function attachButtonListeners(grid, messageId) {
  if (!grid) return;
  const btns = grid.querySelectorAll ? grid.querySelectorAll('.interactive-btn') : [];
  btns.forEach(btn => {
    btn.addEventListener('click', () => {
      const bId = btn.dataset.btnId;
      const bText = btn.dataset.btnText;
      const bType = btn.dataset.btnType;
      const bUrl = btn.dataset.url;
      handleClickButton(messageId, bId, bText, bType, bUrl);
    });
  });
}

// ==========================================================================
// Message Editing & Deletion Management
// ==========================================================================

function startEditMessage(msg) {
  if (!msg) return;
  state.activeEditMessage = { id: msg.id, body: msg.body || '' };
  if (el.composerEditPreview) {
    if (el.editPreviewText) el.editPreviewText.textContent = msg.body || '';
    el.composerEditPreview.classList.remove('hidden');
  }
  if (el.messageInput) {
    el.messageInput.value = msg.body || '';
    resizeMessageInput();
    el.messageInput.focus();
  }
  clearReply();
}

function cancelEditMessage() {
  state.activeEditMessage = null;
  if (el.composerEditPreview) {
    el.composerEditPreview.classList.add('hidden');
    if (el.editPreviewText) el.editPreviewText.textContent = '';
  }
  if (el.messageInput) {
    el.messageInput.value = '';
    resizeMessageInput();
  }
}

async function handleDeleteMessage(messageId) {
  if (!messageId) return;
  // Solo se pueden borrar mensajes propios
  if (el.messagesContainer) {
    const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${messageId}"]`);
    if (row && (row.classList.contains('incoming') || !row.querySelector('.btn-action-delete'))) {
      console.warn('Operación rechazada: no puedes eliminar mensajes de otros remitentes.', messageId);
      return;
    }
  }
  if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
    const cached = state.messageCache.get(state.activeConversationId);
    if (cached && cached.messages) {
      const msg = cached.messages.find(m => m.id === messageId || m.remote_message_id === messageId);
      if (msg) {
        const isOutgoing = msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me';
        if (!isOutgoing) {
          console.warn('Operación rechazada: no puedes eliminar mensajes de otros remitentes.', messageId);
          return;
        }
      }
    }
  }
  try {
    await window.conduitAPI.deleteMessage(messageId);
    removeMessageFromDOM(messageId);
    if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
      const cached = state.messageCache.get(state.activeConversationId);
      if (cached && cached.messages) {
        cached.messages = cached.messages.filter(m => m.id !== messageId && m.remote_message_id !== messageId);
      }
    }
    updatePinnedBanner();
  } catch (err) {
    console.error('Error eliminando mensaje:', err);
  }
}

async function handlePinMessage(messageId, pinned) {
  if (!messageId) return;
  try {
    await window.conduitAPI.pinMessage(messageId, pinned);
    let targetMsg = null;
    if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
      const cached = state.messageCache.get(state.activeConversationId);
      if (cached && cached.messages) {
        const m = cached.messages.find(x => x.id === messageId || x.remote_message_id === messageId);
        if (m) {
          m.is_pinned = pinned;
          targetMsg = m;
        }
      }
    }
    if (state.activeConversationId) {
      let list = state.pinnedMessages.get(state.activeConversationId) || [];
      if (pinned) {
        const idx = list.findIndex(x => (x.id && messageId && x.id === messageId) || (x.remote_message_id && messageId && x.remote_message_id === messageId));
        if (idx >= 0) {
          list[idx] = { ...list[idx], is_pinned: true };
        } else {
          list.push(targetMsg || { id: messageId, is_pinned: true });
        }
      } else {
        list = list.filter(x => (x.id ? x.id !== messageId : true) && (x.remote_message_id ? x.remote_message_id !== messageId : true));
      }
      state.pinnedMessages.set(state.activeConversationId, list);
    }
    updateMessagePinnedInDOM(messageId, pinned);
    updatePinnedBanner();
  } catch (err) {
    console.error('Error fijando mensaje:', err);
  }
}

function updateMessagePinnedInDOM(messageId, pinned) {
  if (!el.messagesContainer) return;
  const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${messageId}"]`);
  if (!row) return;
  const bubble = row.querySelector('.message-bubble');
  if (!bubble) return;
  let indicator = bubble.querySelector('.msg-pinned-indicator');
  if (pinned) {
    if (!indicator) {
      indicator = document.createElement('div');
      indicator.className = 'msg-pinned-indicator';
      indicator.innerHTML = '<i class="fa-solid fa-thumbtack"></i> Fijado';
      if (typeof bubble.prepend === 'function') {
        bubble.prepend(indicator);
      } else if (typeof bubble.insertBefore === 'function' && bubble.firstChild) {
        bubble.insertBefore(indicator, bubble.firstChild);
      } else {
        bubble.appendChild(indicator);
      }
    }
  } else if (indicator) {
    if (typeof indicator.remove === 'function') {
      indicator.remove();
    } else if (indicator.parentNode && typeof indicator.parentNode.removeChild === 'function') {
      indicator.parentNode.removeChild(indicator);
    }
  }
  const btnPin = row.querySelector('.btn-action-pin');
  if (btnPin) {
    btnPin.title = pinned ? 'Desfijar' : 'Fijar';
    const icon = btnPin.querySelector('i');
    if (icon) {
      if (pinned) icon.classList.add('pinned-active');
      else icon.classList.remove('pinned-active');
    }
  }
}

function updatePinnedBanner() {
  if (!el.pinnedMessageBanner || !el.pinnedBannerText) return;
  if (!state.activeConversationId) {
    el.pinnedMessageBanner.classList.add('hidden');
    return;
  }

  let pinnedMsg = null;
  const pinnedList = state.pinnedMessages && state.pinnedMessages.get(state.activeConversationId);
  if (Array.isArray(pinnedList) && pinnedList.length > 0) {
    pinnedMsg = pinnedList[pinnedList.length - 1];
  } else if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
    const cached = state.messageCache.get(state.activeConversationId);
    pinnedMsg = cached && cached.messages ? cached.messages.slice().reverse().find(m => m.is_pinned) : null;
  }

  if (!pinnedMsg) {
    el.pinnedMessageBanner.classList.add('hidden');
    return;
  }

  let bannerText = '';
  if (pinnedMsg.poll && pinnedMsg.poll.question) {
    bannerText = pinnedMsg.poll.question;
  } else if (pinnedMsg.body) {
    bannerText = pinnedMsg.body;
  } else if (pinnedMsg.location) {
    bannerText = 'Ubicación compartida';
  } else if (pinnedMsg.shared_contact) {
    bannerText = 'Contacto compartido';
  } else {
    bannerText = 'Archivo adjunto';
  }

  el.pinnedBannerText.textContent = bannerText;
  el.pinnedMessageBanner.classList.remove('hidden');

  if (el.pinnedBannerContent) {
    el.pinnedBannerContent.onclick = () => {
      const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${pinnedMsg.id}"]`);
      if (row && typeof row.scrollIntoView === 'function') {
        row.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    };
  }

  if (el.btnUnpinBanner) {
    el.btnUnpinBanner.onclick = (e) => {
      e.stopPropagation();
      handlePinMessage(pinnedMsg.id, false);
    };
  }
}

function removeMessageFromDOM(messageId) {
  if (!el.messagesContainer || !messageId) return;
  const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${messageId}"]`)
    || el.messagesContainer.querySelector(`.message-row[data-remote-id="${messageId}"]`);
  if (!row) return;
  if (typeof row.remove === 'function') {
    row.remove();
  } else if (row.parentNode && typeof row.parentNode.removeChild === 'function') {
    row.parentNode.removeChild(row);
  } else if (el.messagesContainer.children) {
    const idx = el.messagesContainer.children.indexOf(row);
    if (idx >= 0) el.messagesContainer.children.splice(idx, 1);
  }
}

function updateMessageInDOM(updatedMsg) {
  if (!el.messagesContainer || !updatedMsg) return;
  const row = (updatedMsg.id && el.messagesContainer.querySelector(`.message-row[data-msg-id="${updatedMsg.id}"]`))
    || (updatedMsg.remote_message_id && el.messagesContainer.querySelector(`.message-row[data-remote-id="${updatedMsg.remote_message_id}"]`));
  if (!row) return;
  if (updatedMsg.id && !row.dataset.msgId) {
    row.dataset.msgId = updatedMsg.id;
  }
  if (updatedMsg.remote_message_id && !row.dataset.remoteId) {
    row.dataset.remoteId = updatedMsg.remote_message_id;
  }
  applyAttachmentsToRow(updatedMsg.id || (row.dataset ? row.dataset.msgId : ''), updatedMsg.attachments);
  const bodyEl = row.querySelector('.msg-body');
  if (bodyEl) bodyEl.textContent = updatedMsg.body || '';
  const metaEl = row.querySelector('.msg-meta');
  if (metaEl && updatedMsg.is_edited && !metaEl.querySelector('.msg-edited-badge')) {
    const badge = document.createElement('span');
    badge.className = 'msg-edited-badge';
    badge.textContent = '(editado)';
    metaEl.prepend(badge);
  }
  // En entornos simulados donde el DOM no parsea hijos reales en row.innerHTML:
  if (row._html) {
    if (updatedMsg.body !== undefined) {
      row._html = row._html.replace(/<div class="msg-body[^"]*">[\s\S]*?<\/div>/, `<div class="msg-body">${escapeHTML(updatedMsg.body || '')}</div>`);
    }
    if (updatedMsg.is_edited && !row._html.includes('msg-edited-badge')) {
      row._html = row._html.replace(/<div class="msg-meta">/, `<div class="msg-meta"><span class="msg-edited-badge">(editado)</span>`);
    }
  }
}

// ==========================================================================
// Composer Attachments (attach, drag & drop, paste and previews)
// ==========================================================================

// MIME por extensión: el core deriva el MIME si se omite, pero mandarlo explícito
// permite clasificar el adjunto antes de enviarlo (imagen, audio, video, documento).
const MIME_BY_EXTENSION = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  bmp: 'image/bmp',
  svg: 'image/svg+xml',
  avif: 'image/avif',
  heic: 'image/heic',
  mp3: 'audio/mpeg',
  m4a: 'audio/mp4',
  ogg: 'audio/ogg',
  oga: 'audio/ogg',
  opus: 'audio/ogg',
  wav: 'audio/wav',
  flac: 'audio/flac',
  aac: 'audio/aac',
  mp4: 'video/mp4',
  m4v: 'video/mp4',
  weba: 'audio/webm',
  webm: 'video/webm',
  mov: 'video/quicktime',
  mkv: 'video/x-matroska',
  avi: 'video/x-msvideo',
  pdf: 'application/pdf',
  txt: 'text/plain',
  md: 'text/markdown',
  csv: 'text/csv',
  json: 'application/json',
  zip: 'application/zip',
  doc: 'application/msword',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  ppt: 'application/vnd.ms-powerpoint',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation'
};

let nextAttachmentId = 1;

function getFileExtension(fileName) {
  const name = String(fileName || '');
  const index = name.lastIndexOf('.');
  if (index < 0 || index === name.length - 1) return '';
  return name.slice(index + 1).toLowerCase();
}

function detectMimeType(fileName) {
  return MIME_BY_EXTENSION[getFileExtension(fileName)] || 'application/octet-stream';
}

// Clasificación usada solo para la UI: el core vuelve a clasificar al persistir.
function classifyAttachmentType(mime) {
  const value = String(mime || '').toLowerCase();
  if (value.startsWith('image/')) return 'image';
  if (value.startsWith('audio/')) return 'audio';
  if (value.startsWith('video/')) return 'video';
  return 'document';
}

function attachmentIconClass(kind) {
  if (kind === 'image') return 'fa-solid fa-file-image';
  if (kind === 'audio') return 'fa-solid fa-file-audio';
  if (kind === 'video') return 'fa-solid fa-file-video';
  return 'fa-solid fa-file-lines';
}

function getPendingAttachments() {
  return state.pendingAttachments.map(att => ({ ...att }));
}

function addPendingAttachment(file) {
  if (!file || !file.path) return null;
  const name = file.name || String(file.path).split(/[\\/]/).pop();
  const attachment = {
    id: nextAttachmentId++,
    path: file.path,
    name,
    size: Number(file.size) || 0,
    mime: file.mime || detectMimeType(name)
  };
  state.pendingAttachments.push(attachment);
  renderPendingAttachments();
  return attachment;
}

function removePendingAttachment(id) {
  const index = state.pendingAttachments.findIndex(att => att.id === id);
  if (index < 0) return false;
  state.pendingAttachments.splice(index, 1);
  renderPendingAttachments();
  return true;
}

function clearPendingAttachments() {
  state.pendingAttachments = [];
  renderPendingAttachments();
}

function buildAttachmentChip(attachment) {
  const chip = document.createElement('div');
  chip.className = 'composer-attachment-chip';
  const kind = classifyAttachmentType(attachment.mime);
  const thumbnail = kind === 'image'
    ? `<img class="composer-attachment-thumb" src="local-media://${encodeURIComponent(attachment.path)}" alt="${escapeHTML(attachment.name)}" />`
    : `<span class="composer-attachment-icon"><i class="${attachmentIconClass(kind)}"></i></span>`;

  chip.innerHTML = `
    ${thumbnail}
    <span class="composer-attachment-info">
      <span class="composer-attachment-name">${escapeHTML(attachment.name)}</span>
      <span class="composer-attachment-size">${formatFileSize(attachment.size)}</span>
    </span>
    <button class="composer-attachment-remove" type="button" title="Remove attachment"><i class="fa-solid fa-xmark"></i></button>
  `;

  const removeBtn = chip.querySelector ? chip.querySelector('.composer-attachment-remove') : null;
  if (removeBtn) {
    removeBtn.addEventListener('click', () => removePendingAttachment(attachment.id));
  }
  return chip;
}

function renderPendingAttachments() {
  const container = el.composerAttachments;
  if (!container) return;
  container.innerHTML = '';
  container.classList.toggle('hidden', state.pendingAttachments.length === 0);
  state.pendingAttachments.forEach(att => container.appendChild(buildAttachmentChip(att)));
}

// Electron 33 removed File.path: the renderer must resolve it through webUtils.
function resolveFilePath(file) {
  if (!file) return '';
  try {
    if (window.conduitAPI && typeof window.conduitAPI.getPathForFile === 'function') {
      const resolved = window.conduitAPI.getPathForFile(file);
      if (resolved) return resolved;
    }
  } catch (err) {
    console.warn('Could not resolve the filesystem path for a dropped file:', err);
  }
  return file.path || '';
}

function extensionForMime(mime) {
  const value = String(mime || '').toLowerCase();
  const extension = Object.keys(MIME_BY_EXTENSION).find(ext => MIME_BY_EXTENSION[ext] === value);
  return extension ? `.${extension}` : '.bin';
}

// Clipboard bitmaps have no filesystem path, so they are written to a temp file.
async function addFileWithoutPath(file) {
  const buffer = await file.arrayBuffer();
  const name = file.name || `pasted-${Date.now()}-${nextAttachmentId}${extensionForMime(file.type)}`;
  const filePath = await window.conduitAPI.saveTempAttachment(buffer, name);
  if (!filePath) return null;
  return addPendingAttachment({
    path: filePath,
    name,
    size: file.size || (buffer && buffer.byteLength) || 0,
    mime: file.type
  });
}

async function addFilesFromFileList(fileList) {
  const files = Array.from(fileList || []);
  for (const file of files) {
    const filePath = resolveFilePath(file);
    if (filePath) {
      addPendingAttachment({ path: filePath, name: file.name, size: file.size, mime: file.type });
      continue;
    }
    try {
      await addFileWithoutPath(file);
    } catch (err) {
      console.error('Could not stage a pasted file:', err);
    }
  }
}

async function pickAttachmentsWithDialog() {
  try {
    const picked = await window.conduitAPI.pickFiles();
    (picked || []).forEach(file => addPendingAttachment(file));
  } catch (err) {
    console.error('Could not open the file picker:', err);
  }
}

// Enviar Mensaje
async function handleSendMessage() {
  const body = el.messageInput.value.trim();

  // Si hay una edición activa, despachar edición en lugar de nuevo envío
  if (state.activeEditMessage) {
    const editId = state.activeEditMessage.id;
    cancelEditMessage();
    if (!body) return;
    try {
      const updatedMsg = await window.conduitAPI.editMessage(editId, body);
      if (updatedMsg) {
        if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
          const cached = state.messageCache.get(state.activeConversationId);
          if (cached && cached.messages) {
            const idx = cached.messages.findIndex(m => m.id === editId);
            if (idx >= 0) cached.messages[idx] = updatedMsg;
          }
        }
        updateMessageInDOM(updatedMsg);
      }
    } catch (err) {
      alert(`Error editando mensaje: ${err.message}`);
    }
    return;
  }

  const attachments = getPendingAttachments();
  if ((!body && attachments.length === 0) || !state.activeConversationId) return;

  const targetChannel = el.channelSelect.value;
  const replyToId = state.activeReply ? state.activeReply.id : '';
  el.messageInput.value = '';
  resizeMessageInput();
  clearPendingAttachments();
  clearReply();

  try {
    const sentMsg = await window.conduitAPI.sendMessage(state.activeConversationId, body, targetChannel, replyToId, attachments);
    if (sentMsg) {
      if (state.messageCache.has(state.activeConversationId)) {
        const cached = state.messageCache.get(state.activeConversationId);
        if (cached && cached.messages && !cached.messages.some(m => m.id === sentMsg.id)) {
          cached.messages.push(sentMsg);
        }
      }
      appendMessageBubble(sentMsg);
      scrollMessagesToBottom();

      // Mover inmediatamente la conversación al top de la lista
      const conv = state.conversations.find(c => c.id === state.activeConversationId);
      if (conv) {
        conv.last_message_at_unix = sentMsg.sent_at_unix || Math.floor(Date.now() / 1000);
        conv.last_message_preview = sentMsg.body;
        if (sentMsg.channel_id) {
          conv.active_channel_id = sentMsg.channel_id;
          updateChannelSelector(conv);
        }
        renderConversations();
      }
      loadConversations();
    }
  } catch (err) {
    alert(`Error enviando mensaje via conduitd: ${err.message}`);
  }
}

// Busca en el Identity Graph el contacto asociado a una cuenta del usuario
function findMyAccountContact(acc) {
  if (!state.contacts || state.contacts.length === 0) return null;
  const cleanIdent = (acc && acc.identifier ? acc.identifier : '').replace(/[^0-9]/g, '');
  const digits = cleanIdent.length >= 7 ? cleanIdent.slice(-7) : cleanIdent;

  return state.contacts.find(c => {
    if (c.channels && c.channels.length > 0) {
      if (digits && c.channels.some(ch => (ch.remote_id || '').replace(/[^0-9]/g, '').includes(digits))) {
        return true;
      }
    }
    return false;
  }) || state.contacts.find(c => {
    if (!c.display_name) return false;
    const dn = c.display_name.toLowerCase();
    const l = (acc && acc.label ? acc.label : '').toLowerCase();
    return Boolean(l && (dn.includes(l) || l.includes(dn)));
  }) || null;
}

// Cargar Cuentas (ZapZap inspired)
async function loadAccounts() {
  try {
    const accounts = await window.conduitAPI.listAccounts();
    state.accounts = accounts || [];
    renderAccountsSidebar();
    renderAccountsGrid();
  } catch (err) {
    console.error('Error loadAccounts:', err);
  }
}

function renderAccountsSidebar() {
  el.accountsPills.innerHTML = '';
  const registeredProviders = new Set();

  state.accounts.forEach(acc => {
    const pill = document.createElement('div');
    pill.className = 'provider-pill';
    pill.title = `${acc.label || acc.id} (${acc.provider})`;

    const provInfo = getProviderByEnum(acc.provider);
    const iconClass = provInfo.icon;
    const provBadge = provInfo.badge;
    registeredProviders.add(provInfo.id);

    let accountAvatarUrl = '';
    const myContact = findMyAccountContact(acc);
    if (myContact && myContact.avatar_url) {
      accountAvatarUrl = myContact.avatar_url;
    }

    const avatarInner = getAvatarHTML(accountAvatarUrl, acc.label || acc.identifier || 'U');

    pill.innerHTML = `
      <div class="pill-marker"></div>
      <div class="provider-icon">
        ${avatarInner}
        <div class="provider-sub-badge ${provBadge}"${providerBadgeStyleAttr(provInfo)}><i class="${iconClass}"></i></div>
      </div>
    `;
    pill.onclick = () => {
      showView('view-accounts');
    };
    el.accountsPills.appendChild(pill);
  });

  // Renderizar chips de filtro dinámicos (solo los proveedores con cuenta registrada)
  renderDynamicFilterChips(registeredProviders);
}

function renderDynamicFilterChips(providers) {
  const container = document.getElementById('chat-filters-container');
  if (!container) return;

  container.innerHTML = `<button class="filter-chip ${state.activeFilter === 'all' ? 'active' : ''}" data-filter="all">Todos</button>`;

  const ordered = allKnownProviders().filter(p => providers.has(p.id));
  providers.forEach(id => {
    if (!ordered.some(p => p.id === id)) ordered.push(getProviderByEnum(id));
  });
  ordered.forEach(info => {
    const btn = document.createElement('button');
    btn.className = `filter-chip ${state.activeFilter === info.id ? 'active' : ''}`;
    btn.dataset.filter = info.id;
    btn.title = info.name;
    btn.innerHTML = `<i class="${info.icon}"></i>`;
    container.appendChild(btn);
  });

  // Re-enlazar listeners
  container.querySelectorAll('.filter-chip').forEach(chip => {
    chip.addEventListener('click', () => {
      container.querySelectorAll('.filter-chip').forEach(c => c.classList.remove('active'));
      chip.classList.add('active');
      state.activeFilter = chip.dataset.filter;
      renderConversations();
    });
  });
}

// Clave de proveedor usada por la UI (id del manifiesto: 'whatsapp', 'telegram', 'matrix'…).
// El enum llega como nombre desde gRPC y como número en algunos fixtures de prueba.
function providerKey(provider) {
  return getProviderByEnum(provider).id;
}

// Copy del panel de emparejamiento según el proveedor que emitió el QR.
function qrCopyFor(provider) {
  const info = getProviderByEnum(provider);
  return { title: `Escanea con ${info.name}`, hint: info.qrInstructions || '' };
}

// Proveedores ofrecidos al crear una cuenta: manifiestos instalados y habilitados
// (o la tabla oficial cuando no hay ninguno).
function accountCreationProviders() {
  const fromManifests = providerManifests.filter(p => p.enabled && p.provider);
  return fromManifests.length > 0 ? fromManifests : allKnownProviders().filter(p => !p.fromManifest && p.provider);
}

// Rellena el selector de proveedor del alta de cuenta.
function populateAccountProviderOptions() {
  const select = el.newAccountProvider;
  if (!select) return;
  const previous = select.value;
  select.innerHTML = '';
  const providers = accountCreationProviders();
  providers.forEach(info => {
    const opt = document.createElement('option');
    opt.value = info.provider;
    opt.textContent = info.description ? `${info.name} — ${info.description}` : info.name;
    select.appendChild(opt);
  });
  const stillThere = providers.some(info => info.provider === previous);
  select.value = stillThere ? previous : (providers[0] ? providers[0].provider : '');
}

// Entradas generadas para el proveedor elegido: [{ field, input }].
let credentialInputs = [];

function credentialInputType(kind) {
  switch (kind) {
    case 'CREDENTIAL_FIELD_KIND_SECRET': return 'password';
    case 'CREDENTIAL_FIELD_KIND_PHONE': return 'tel';
    default: return 'text';
  }
}

// Ayuda de un campo: enlace externo si empieza con https://, texto plano en otro caso.
function buildCredentialHelp(help) {
  const text = String(help || '');
  if (/^https:\/\//i.test(text)) {
    const link = document.createElement('a');
    link.href = text;
    link.textContent = text;
    link.style.fontSize = '11px';
    link.style.color = 'var(--accent-conduit)';
    link.addEventListener('click', (e) => {
      if (e && e.preventDefault) e.preventDefault();
      if (window.conduitAPI && typeof window.conduitAPI.openExternal === 'function') window.conduitAPI.openExternal(text);
    });
    return link;
  }
  const small = document.createElement('small');
  small.textContent = text;
  small.className = 'text-muted';
  small.style.fontSize = '11px';
  return small;
}

// Genera los campos de credenciales del proveedor elegido a partir de su manifiesto.
function syncAccountCredentialFields() {
  if (!el.newAccountProvider) return;
  const info = getProviderByEnum(el.newAccountProvider.value);
  const container = el.accountCredentialFields;
  credentialInputs = [];
  if (container) {
    container.innerHTML = '';
    (info.credentialFields || []).forEach(field => {
      const wrapper = document.createElement('div');
      wrapper.className = 'cred-field';

      const label = document.createElement('label');
      label.textContent = field.required ? `${field.label || field.key} *` : (field.label || field.key);
      label.style.fontSize = '12px';
      label.style.color = 'var(--text-muted)';
      label.style.display = 'block';
      label.style.marginBottom = '4px';
      wrapper.appendChild(label);

      const input = document.createElement('input');
      input.type = credentialInputType(field.kind);
      if (field.kind === 'CREDENTIAL_FIELD_KIND_NUMBER') input.setAttribute('inputmode', 'numeric');
      input.id = `account-cred-${field.key}`;
      input.className = 'form-input';
      input.placeholder = field.placeholder || '';
      input.dataset.credKey = field.key;
      input.style.width = '100%';
      wrapper.appendChild(input);

      if (field.help) wrapper.appendChild(buildCredentialHelp(field.help));
      container.appendChild(wrapper);
      credentialInputs.push({ field, input });
    });
  }
  if (el.accountCredentialHint) {
    el.accountCredentialHint.textContent = info.credentialsHint || '';
  }
}

// Etiqueta del primer campo obligatorio vacío ('' si no falta ninguno).
function missingRequiredCredential() {
  const missing = credentialInputs.find(({ field, input }) => field.required && !(input.value || '').trim());
  return missing ? (missing.field.label || missing.field.key) : '';
}

// Reúne las credenciales escritas como { values }, o null si el proveedor no pide ninguna.
function buildAccountCredentials() {
  if (credentialInputs.length === 0) return null;
  const values = {};
  credentialInputs.forEach(({ field, input }) => {
    const value = (input.value || '').trim();
    if (value) values[field.key] = value;
  });
  return { values };
}

// Limpia los campos de credenciales después de guardar una cuenta.
function resetAccountCredentialFields() {
  credentialInputs.forEach(({ input }) => { input.value = ''; });
}

function renderAccountsGrid() {
  el.accountsGrid.innerHTML = '';
  if (state.accounts.length === 0) {
    el.accountsGrid.innerHTML = '<p class="text-muted">No hay cuentas de proveedores configuradas aún.</p>';
    return;
  }

  state.accounts.forEach(acc => {
    const card = document.createElement('div');
    card.className = 'account-card';
    const isConn = acc.status === 'connected';
    const isPairing = acc.status === 'pairing';
    const isDemo = acc.status === 'demo';
    const isError = acc.status === 'error';

    const provInfo = getProviderByEnum(acc.provider);
    const provIcon = provInfo.icon;

    let accountAvatarUrl = '';
    const myContact = findMyAccountContact(acc);
    if (myContact && myContact.avatar_url) {
      accountAvatarUrl = myContact.avatar_url;
    }
    const avatarContent = getAvatarHTML(accountAvatarUrl, acc.label || acc.identifier || 'Cuenta', `<i class="${provIcon}"></i>`);
    const qrCopy = qrCopyFor(acc.provider);

    const provBadge = provInfo.badge;

    // Una cuenta simulada nunca se pinta como conexión real.
    let statusClass = 'status-badge-disconnected';
    let statusLabel = acc.status || 'disconnected';
    if (isConn) statusClass = 'status-badge-connected';
    else if (isPairing) statusClass = 'status-badge-pairing';
    else if (isDemo) { statusClass = 'status-badge-demo'; statusLabel = 'demostración'; }
    else if (isError) { statusClass = 'status-badge-error'; statusLabel = 'error'; }

    card.innerHTML = `
      <div class="account-card-header">
        <div class="conv-avatar">
          ${avatarContent}
          <div class="conv-badge-provider ${provBadge}"${providerBadgeStyleAttr(provInfo)}><i class="${provIcon}"></i></div>
        </div>
        <div>
          <h4>${acc.label || acc.id}</h4>
          <span class="account-status-badge ${statusClass}">
            ${statusLabel}
          </span>
        </div>
      </div>
      <div>
        <p style="font-size:12px;color:var(--text-muted)">Proveedor: <strong>${acc.provider}</strong></p>
        <p style="font-size:11px;color:var(--text-dim)">Identificador: ${acc.identifier || 'default'}</p>
        ${isDemo || isError ? `
        <div class="account-status-message ${isError ? 'account-status-message-error' : ''}">
          ${escapeHTML(acc.status_message || (isDemo ? 'Modo demostración: esta cuenta todavía no habla con el proveedor real.' : 'La conexión falló.'))}
        </div>` : ''}
        ${isDemo ? '<p style="font-size:11px;color:var(--text-dim);margin-top:6px;">Datos de muestra: no hay mensajes reales del proveedor.</p>' : ''}
      </div>
      ${acc.qr_data_url ? `
        <div style="background:white;padding:12px;border-radius:8px;display:flex;flex-direction:column;align-items:center;margin-top:10px;">
          <p style="color:#111;font-size:12px;font-weight:bold;margin-bottom:6px;">${qrCopy.title}</p>
          <img src="${acc.qr_data_url}" alt="${qrCopy.title}" style="width:160px;height:160px;display:block;" />
          ${qrCopy.hint ? `<p style="color:#444;font-size:10px;margin-top:6px;">${qrCopy.hint}</p>` : ''}
        </div>` : (acc.qr_code ? `
        <div style="background:white;padding:8px;border-radius:6px;display:inline-block;text-align:center;margin-top:8px;">
          <p style="color:black;font-size:11px;font-weight:bold;margin-bottom:4px;">${qrCopy.title}</p>
          <pre style="color:black;font-size:8px;line-height:8px;font-family:monospace;">${escapeHTML(acc.qr_code)}</pre>
        </div>` : '')}
      <div style="display:flex;gap:8px;margin-top:auto;padding-top:10px;">
        ${isConn || isPairing || isDemo
          ? `<button class="btn-card btn-danger" onclick="disconnectAccount('${acc.id}')">Desconectar</button>`
          : `<button class="btn-card btn-primary" onclick="connectAccount('${acc.id}')">Conectar Ahora</button>`
        }
        <button class="btn-card" style="background:#3f4147;color:white;" onclick="removeAccount('${acc.id}')" title="Eliminar Cuenta"><i class="fa-solid fa-trash"></i></button>
      </div>
    `;
    el.accountsGrid.appendChild(card);
  });
}

window.connectAccount = async (id) => {
  try {
    const res = await window.conduitAPI.connectAccount(id);
    if (res && res.account) {
      const idx = state.accounts.findIndex(a => a.id === id);
      if (idx !== -1) {
        state.accounts[idx] = res.account;
      }
      renderAccountsGrid();
      renderAccountsPills();
    }
    await loadAccounts();
    await loadConversations();
  } catch (e) {
    alert('Error conectando cuenta: ' + e.message);
  }
};

window.disconnectAccount = async (id) => {
  try {
    await window.conduitAPI.disconnectAccount(id);
    await loadAccounts();
  } catch (e) {
    alert('Error desconectando: ' + e.message);
  }
};

window.removeAccount = async (id) => {
  if (!confirm('¿Estás seguro de eliminar esta cuenta de conduitd?')) return;
  try {
    await window.conduitAPI.removeAccount(id);
    await loadAccounts();
  } catch (e) {
    alert('Error eliminando cuenta: ' + e.message);
  }
};

// ================= GESTIÓN DE CONTACTOS UNIFICADOS (Identity Graph) =================
async function loadContacts() {
  try {
    const contacts = await window.conduitAPI.listContacts();
    state.contacts = contacts || [];
    renderContactsGrid();
    populateMergeSelects();
    populateIdentitySelects();
    renderAccountsSidebar();
    renderAccountsGrid();
    if (state.activeConversationId) {
      const activeConv = state.conversations.find(c => c.id === state.activeConversationId);
      if (activeConv) updateChannelSelector(activeConv);
    }
  } catch (err) {
    console.error('Error loadContacts:', err);
  }
}

function renderContactsGrid() {
  if (!el.contactsGrid) return;
  el.contactsGrid.innerHTML = '';
  if (state.contacts.length === 0) {
    el.contactsGrid.innerHTML = '<p class="text-muted">No hay contactos registrados en el Identity Graph.</p>';
    return;
  }

  state.contacts.forEach(c => {
    const card = document.createElement('div');
    card.className = 'account-card';
    const channels = c.channels || [];

    const channelsBadges = channels.map(ch => {
      const chInfo = getProviderByEnum(ch.provider);
      const icon = chInfo.icon;
      const chColor = chInfo.color || `var(--accent-${chInfo.id})`;
      return `
        <div style="display:flex;align-items:center;justify-content:space-between;background:rgba(255,255,255,0.04);padding:6px 10px;border-radius:6px;margin-top:6px;">
          <div style="display:flex;align-items:center;gap:8px;">
            <i class="${icon}" style="color:${chColor}"></i>
            <span style="font-size:12px;">${escapeHTML(ch.handle || ch.remote_id)}</span>
          </div>
          ${channels.length > 1 ? `
            <button class="icon-btn-sm" onclick="splitContactChannel('${ch.id}', '${escapeHTML(ch.handle || ch.remote_id)}')" title="Desacoplar canal a un contacto independiente">
              <i class="fa-solid fa-scissors"></i>
            </button>` : ''}
        </div>
      `;
    }).join('');

    const avatarContent = getAvatarHTML(c.avatar_url, c.display_name || 'Contacto');

    const identityLines = [];
    if (c.email) identityLines.push(`<div><i class="fa-solid fa-envelope"></i> ${escapeHTML(c.email)}</div>`);
    if (c.phone) identityLines.push(`<div><i class="fa-solid fa-phone"></i> ${escapeHTML(c.phone)}</div>`);
    if (identityLines.length === 0) identityLines.push('<div>Sin identidad canónica (email / teléfono)</div>');

    card.innerHTML = `
      <div class="account-card-header">
        <div class="conv-avatar">${avatarContent}</div>
        <div>
          <h4>${escapeHTML(c.display_name || 'Contacto')}</h4>
          <span style="font-size:11px;color:var(--text-dim);">${channels.length} canal(es) unificado(s)</span>
        </div>
      </div>
      <div style="margin-top:8px;font-size:11px;color:var(--text-dim);display:flex;flex-direction:column;gap:2px;">
        ${identityLines.join('')}
      </div>
      <div style="margin-top:8px;">
        <p style="font-size:11px;font-weight:600;color:var(--text-muted);text-transform:uppercase;">Canales Vinculados:</p>
        ${channelsBadges || '<p style="font-size:12px;color:var(--text-dim)">Sin canales activos</p>'}
      </div>
      <div style="display:flex;gap:8px;margin-top:auto;padding-top:12px;">
        <button class="btn-card btn-primary" onclick="openChatWithContact('${c.id}')" style="width:100%;">
          <i class="fa-solid fa-comment"></i> Abrir Chat
        </button>
        <button class="btn-card" style="background:#3f4147;color:white;" onclick="editContactIdentity('${c.id}')" title="Definir email y teléfono">
          <i class="fa-solid fa-id-card"></i>
        </button>
      </div>
    `;
    el.contactsGrid.appendChild(card);
  });
}

function populateMergeSelects() {
  if (!el.mergePrimarySelect || !el.mergeSecondarySelect) return;
  el.mergePrimarySelect.innerHTML = '';
  el.mergeSecondarySelect.innerHTML = '';

  state.contacts.forEach(c => {
    const opt1 = document.createElement('option');
    opt1.value = c.id;
    opt1.textContent = `${c.display_name} (${(c.channels || []).length} canales)`;
    el.mergePrimarySelect.appendChild(opt1);

    const opt2 = document.createElement('option');
    opt2.value = c.id;
    opt2.textContent = `${c.display_name} (${(c.channels || []).length} canales)`;
    el.mergeSecondarySelect.appendChild(opt2);
  });
}

// Rellena los selectores de identidad canónica y de vinculación manual.
function populateIdentitySelects() {
  [el.identityContactSelect, el.identityTargetSelect].forEach(select => {
    if (!select) return;
    select.innerHTML = '';
    state.contacts.forEach(c => {
      const option = document.createElement('option');
      option.value = c.id;
      const identity = c.email || c.phone || `${(c.channels || []).length} canales`;
      option.textContent = `${c.display_name || 'Contacto'} — ${identity}`;
      select.appendChild(option);
    });
  });

  if (el.identityContactSelect) {
    const current = state.contacts.find(x => x.id === el.identityContactSelect.value);
    if (el.identityEmailInput) el.identityEmailInput.value = (current && current.email) || '';
    if (el.identityPhoneInput) el.identityPhoneInput.value = (current && current.phone) || '';
  }
}

// Abre el formulario de identidad canónica con los valores actuales del contacto.
window.editContactIdentity = (contactId) => {
  if (!el.identitySetBox) return;
  populateIdentitySelects();
  if (el.identityContactSelect) el.identityContactSelect.value = contactId;
  const contact = state.contacts.find(x => x.id === contactId);
  if (el.identityEmailInput) el.identityEmailInput.value = (contact && contact.email) || '';
  if (el.identityPhoneInput) el.identityPhoneInput.value = (contact && contact.phone) || '';
  el.identitySetBox.classList.remove('hidden');
  if (el.identitySetBox.scrollIntoView) el.identitySetBox.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
};

// Busca contactos por email, teléfono o identificador y ofrece fusionarlos dentro del contacto principal.
async function searchContactsByIdentity() {
  if (!el.identityQueryInput || !el.identityResults) return;

  const query = el.identityQueryInput.value.trim();
  if (!query) {
    alert('Ingresa un email, un teléfono o un identificador para buscar.');
    return;
  }

  const targetId = el.identityTargetSelect ? el.identityTargetSelect.value : '';
  const target = state.contacts.find(c => c.id === targetId);
  el.identityResults.innerHTML = '<div class="loading-state"><i class="fa-solid fa-spinner fa-spin"></i> Buscando...</div>';

  let matches = [];
  try {
    matches = await window.conduitAPI.findContactsByIdentity(query) || [];
  } catch (e) {
    el.identityResults.innerHTML = `<p style="font-size:12px;color:var(--danger)">Error buscando identidad: ${escapeHTML(e.message)}</p>`;
    return;
  }

  if (matches.length === 0) {
    el.identityResults.innerHTML = `<p style="font-size:12px;color:var(--text-muted)">Ningún contacto coincide con "${escapeHTML(query)}".</p>`;
    return;
  }

  const targetName = target ? target.display_name : 'el contacto principal';
  el.identityResults.innerHTML = matches.map(match => {
    const channels = (match.channels || []).length;
    const identity = [match.email, match.phone].filter(Boolean).join(' · ') || 'sin identidad canónica';
    const isTarget = match.id === targetId;

    return `
      <div style="display:flex;align-items:center;justify-content:space-between;background:rgba(255,255,255,0.04);padding:8px 12px;border-radius:6px;margin-top:6px;gap:12px;">
        <div>
          <div style="font-size:13px;font-weight:600;">${escapeHTML(match.display_name || 'Contacto')}</div>
          <div style="font-size:11px;color:var(--text-dim);">${escapeHTML(identity)} · ${channels} canal(es)</div>
        </div>
        ${isTarget
          ? '<span class="text-muted" style="font-size:11px;">Ya es el contacto principal</span>'
          : `<button class="btn-card btn-primary" onclick="mergeIdentityMatch('${match.id}')">Fusionar en ${escapeHTML(targetName)}</button>`}
      </div>
    `;
  }).join('');
}

// Fusiona un resultado de búsqueda de identidad dentro del contacto principal elegido.
window.mergeIdentityMatch = async (secondaryId) => {
  const primaryId = el.identityTargetSelect ? el.identityTargetSelect.value : '';
  if (!primaryId) {
    alert('Selecciona un contacto principal.');
    return;
  }
  if (primaryId === secondaryId) {
    alert('Ese contacto ya es el principal.');
    return;
  }

  try {
    await window.conduitAPI.mergeContacts(primaryId, secondaryId);
    await loadContacts();
    await loadConversations();
    if (el.identityResults) el.identityResults.innerHTML = '<p style="font-size:12px;color:var(--success,#3ba55d)">Contactos fusionados.</p>';
  } catch (e) {
    alert('Error fusionando contactos: ' + e.message);
  }
};

window.splitContactChannel = async (channelId, handle) => {
  const newName = prompt(`Ingresa el nombre del nuevo contacto individual para el canal (${handle}):`, handle);
  if (!newName) return;

  try {
    await window.conduitAPI.splitChannel(channelId, newName);
    await loadContacts();
    await loadConversations();
  } catch (e) {
    alert('Error desacoplando canal: ' + e.message);
  }
};

window.openChatWithContact = (contactId) => {
  selectConversation(contactId);
};

// Cargar Plugins
async function loadPlugins() {
  try {
    const plugins = await window.conduitAPI.listPlugins();
    state.plugins = plugins || [];
    rebuildProviderRegistry(state.plugins);
    renderPlugins();
    refreshProviderViews();
  } catch (err) {
    console.error('Error loadPlugins:', err);
  }
}

// Vuelve a pintar lo que depende de los manifiestos (selector de alta, cuentas, badges).
function refreshProviderViews() {
  populateAccountProviderOptions();
  syncAccountCredentialFields();
  if (state.accounts && state.accounts.length > 0) {
    renderAccountsSidebar();
    renderAccountsGrid();
  }
  if (state.conversations && state.conversations.length > 0) renderConversations();
}

function renderPlugins() {
  el.pluginsList.innerHTML = '';
  if (state.plugins.length === 0) {
    el.pluginsList.innerHTML = '<p class="text-muted">No se detectaron plugins en conduitd.</p>';
    return;
  }

  state.plugins.forEach(p => {
    const isEnabled = p.is_enabled !== undefined ? p.is_enabled : (p.enabled || false);
    const isInstalled = p.is_installed !== undefined ? p.is_installed : true;
    const div = document.createElement('div');
    div.className = 'account-card';
    div.innerHTML = `
      <div style="display:flex;justify-content:space-between;align-items:flex-start;">
        <div>
          <h4>${escapeHTML(p.name || p.id)} (v${escapeHTML(p.version || '1.0')})</h4>
          <p style="font-size:13px;color:var(--text-muted)">${escapeHTML(p.description || 'Plugin oficial de canal')}</p>
          ${p.author ? `<p style="font-size:11px;color:var(--text-dim);margin-top:2px;">Autor: ${escapeHTML(p.author)}</p>` : ''}
          ${isInstalled && p.manifest && p.manifest.version ? `<p style="font-size:11px;color:var(--text-dim);margin-top:2px;">Versión instalada: ${escapeHTML(p.manifest.version)}</p>` : ''}
          ${p.repository ? `<p style="font-size:11px;color:var(--text-dim);margin-top:2px;">Repositorio: ${escapeHTML(p.repository)}</p>` : ''}
          ${isInstalled && p.path ? `<p style="font-size:11px;color:var(--text-dim);margin-top:2px;word-break:break-all;">Ruta: ${escapeHTML(p.path)}</p>` : ''}
        </div>
        <div style="display:flex;gap:8px;align-items:center;">
          <span class="account-status-badge ${isEnabled ? 'status-badge-connected' : 'status-badge-disconnected'}">
            ${isEnabled ? 'Habilitado' : 'Deshabilitado'}
          </span>
        </div>
      </div>
    `;

    const actionsDiv = document.createElement('div');
    actionsDiv.style.marginTop = '12px';
    actionsDiv.style.display = 'flex';
    actionsDiv.style.gap = '8px';

    if (!isInstalled) {
      const btnInstall = document.createElement('button');
      btnInstall.className = 'btn-card btn-primary btn-install-plugin';
      btnInstall.innerHTML = '<i class="fa-solid fa-download"></i> Instalar';
      btnInstall.addEventListener('click', async () => {
        try {
          await window.conduitAPI.installPlugin(p.id);
          await loadPlugins();
        } catch (e) {
          alert('Error al instalar plugin: ' + e.message);
        }
      });
      actionsDiv.appendChild(btnInstall);
    } else {
      const btnToggle = document.createElement('button');
      btnToggle.className = `btn-card ${isEnabled ? 'btn-danger' : 'btn-primary'} btn-toggle-plugin`;
      btnToggle.innerHTML = `<i class="fa-solid ${isEnabled ? 'fa-pause' : 'fa-play'}"></i> ${isEnabled ? 'Deshabilitar' : 'Habilitar'}`;
      btnToggle.addEventListener('click', async () => {
        try {
          await window.conduitAPI.togglePlugin(p.id, !isEnabled);
          await loadPlugins();
        } catch (e) {
          alert('Error al conmutar plugin: ' + e.message);
        }
      });
      actionsDiv.appendChild(btnToggle);
    }

    div.appendChild(actionsDiv);
    el.pluginsList.appendChild(div);
  });
}

// ================= CONFIGURACIONES (Telegram, Discord, ZapZap) =================
async function loadSettings() {
  try {
    const s = await window.conduitAPI.getSettings();
    state.settings = s || {};
    applySettingsToForm(state.settings);
    applySettingsUI(state.settings);
  } catch (err) {
    console.error('Error loadSettings:', err);
  }
}

function applySettingsToForm(s) {
  if (!s) return;
  if (s.language) document.getElementById('setting-language').value = s.language;
  if (s.timezone) document.getElementById('setting-timezone').value = s.timezone;
  if (s.theme) document.getElementById('setting-theme').value = s.theme;
  if (s.message_font_size) document.getElementById('setting-font-size').value = s.message_font_size;
  if (s.download_directory) document.getElementById('setting-download-dir').value = s.download_directory;
  if (s.spellcheck_language) document.getElementById('setting-spellcheck-language').value = s.spellcheck_language;

  document.getElementById('setting-spellcheck-enabled').checked = s.spellcheck_enabled !== false;
  document.getElementById('setting-notifications-enabled').checked = s.notifications_enabled !== false;
  document.getElementById('setting-notify-preview').checked = s.notify_preview !== false;
  document.getElementById('setting-notify-sound').checked = s.notify_sound !== false;
  document.getElementById('setting-notify-flash').checked = s.notify_flash_taskbar !== false;
  document.getElementById('setting-show-badge').checked = s.show_unread_badge !== false;
  document.getElementById('setting-enable-markdown').checked = s.enable_markdown !== false;
  document.getElementById('setting-auto-media').checked = s.auto_download_media !== false;
  document.getElementById('setting-auto-docs').checked = s.auto_download_documents === true;
  document.getElementById('setting-minimize-tray').checked = s.minimize_to_tray !== false;
  document.getElementById('setting-hardware-accel').checked = s.hardware_acceleration !== false;
  const compactEl = document.getElementById('setting-compact-mode');
  if (compactEl) compactEl.checked = s.compact_message_mode === true;
  const startMinEl = document.getElementById('setting-start-minimized');
  if (startMinEl) startMinEl.checked = s.start_minimized === true;
}

function applySettingsUI(s) {
  if (!s) return;
  
  // 1. Aplicar tema visual en body y clase compact-mode si corresponde
  document.body.className = `theme-${s.theme || 'dark'}`;
  if (s.compact_message_mode) {
    document.body.classList.add('compact-mode');
  }

  // 2. Aplicar tamaño de fuente en mensajes y composer
  const fontSize = s.message_font_size || 15;
  if (document.documentElement && document.documentElement.style && typeof document.documentElement.style.setProperty === 'function') {
    document.documentElement.style.setProperty('--font-size-msg', `${fontSize}px`);
  }
  document.querySelectorAll('.msg-body').forEach(m => {
    m.style.fontSize = `${fontSize}px`;
  });
  const inputEl = document.getElementById('message-input');
  if (inputEl) {
    inputEl.style.fontSize = `${fontSize}px`;
  }

  // 3. Configuración del corrector ortográfico en textarea
  const msgInput = document.getElementById('message-input');
  if (msgInput) {
    msgInput.spellcheck = s.spellcheck_enabled !== false;
    msgInput.lang = s.spellcheck_language || 'es-ES';
  }
}

async function saveSettingsFromForm() {
  const updated = {
    language: document.getElementById('setting-language').value,
    timezone: document.getElementById('setting-timezone').value,
    theme: document.getElementById('setting-theme').value,
    message_font_size: parseInt(document.getElementById('setting-font-size').value, 10) || 15,
    download_directory: document.getElementById('setting-download-dir').value,
    spellcheck_enabled: document.getElementById('setting-spellcheck-enabled').checked,
    spellcheck_language: document.getElementById('setting-spellcheck-language').value,
    notifications_enabled: document.getElementById('setting-notifications-enabled').checked,
    notify_preview: document.getElementById('setting-notify-preview').checked,
    notify_sound: document.getElementById('setting-notify-sound').checked,
    notify_flash_taskbar: document.getElementById('setting-notify-flash').checked,
    show_unread_badge: document.getElementById('setting-show-badge').checked,
    enable_markdown: document.getElementById('setting-enable-markdown').checked,
    compact_message_mode: document.getElementById('setting-compact-mode') ? document.getElementById('setting-compact-mode').checked : false,
    start_minimized: document.getElementById('setting-start-minimized') ? document.getElementById('setting-start-minimized').checked : false,
    auto_download_media: document.getElementById('setting-auto-media').checked,
    auto_download_documents: document.getElementById('setting-auto-docs').checked,
    minimize_to_tray: document.getElementById('setting-minimize-tray').checked,
    hardware_acceleration: document.getElementById('setting-hardware-accel').checked
  };

  try {
    const saved = await window.conduitAPI.updateSettings(updated);
    state.settings = saved || updated;
    applySettingsUI(state.settings);
    alert('✅ Configuraciones guardadas y aplicadas.');
  } catch (e) {
    alert('Error guardando configuraciones: ' + e.message);
  }
}

// Re-render diferido del chat activo: agrupa una ráfaga de historial en un solo
// render y conserva la posición visual del usuario.
let activeRenderTimer = null;
function scheduleActiveConversationRender() {
  if (activeRenderTimer) return;
  activeRenderTimer = setTimeout(() => {
    activeRenderTimer = null;
    const cached = state.messageCache.get(state.activeConversationId);
    if (!cached || !cached.messages) return;

    const prevHeight = el.messagesContainer.scrollHeight;
    const prevTop = el.messagesContainer.scrollTop;
    renderMessages(cached.messages, false);
    el.messagesContainer.scrollTop = prevTop + (el.messagesContainer.scrollHeight - prevHeight);
    updateScrollBottomButton();
  }, 200);
}

// Manejador de eventos en tiempo real gRPC
function setupRealtimeEvents() {
  window.conduitAPI.onEvent((envelope) => {
    console.log('Evento gRPC recibido:', envelope);
    if (envelope.channel_status_update) {
      checkDaemonStatus();
      loadAccounts();
    }
    if (envelope.message_deleted) {
      removeMessageFromDOM(envelope.message_deleted);
      if (state.messageCache && state.messageCache.has(state.activeConversationId)) {
        const cached = state.messageCache.get(state.activeConversationId);
        if (cached && cached.messages) {
          cached.messages = cached.messages.filter(m => m.id !== envelope.message_deleted && m.remote_message_id !== envelope.message_deleted);
        }
      }
      if (state.conversations && state.messageCache) {
        let convUpdated = false;
        state.conversations.forEach(conv => {
          if (state.messageCache.has(conv.id)) {
            const cached = state.messageCache.get(conv.id);
            if (cached && cached.messages) {
              const prevLen = cached.messages.length;
              cached.messages = cached.messages.filter(m => m.id !== envelope.message_deleted && m.remote_message_id !== envelope.message_deleted);
              if (cached.messages.length !== prevLen) {
                const lastMsg = cached.messages.length > 0 ? cached.messages[cached.messages.length - 1] : null;
                conv.last_message_preview = lastMsg ? (lastMsg.body || 'Adjunto') : 'Sin mensajes aún';
                convUpdated = true;
              }
            }
          }
        });
        if (convUpdated) {
          renderConversations();
        }
      }
      if (state.pinnedMessages.has(state.activeConversationId)) {
        let list = state.pinnedMessages.get(state.activeConversationId) || [];
        state.pinnedMessages.set(state.activeConversationId, list.filter(m => m.id !== envelope.message_deleted && m.remote_message_id !== envelope.message_deleted));
      }
      updatePinnedBanner();
      return;
    }
    if (envelope.conversation_read) {
      const readEv = envelope.conversation_read;
      const conv = state.conversations.find(c => c.id === readEv.conversation_id);
      if (conv) {
        // Conteo restante informado por el core; si falta (proto3 omite el 0) es 0
        conv.unread_count = Number(readEv.unread_count) || 0;
        renderConversations();
      }
      return;
    }
    const msg = envelope.message_received || envelope.message_sent || envelope.message_updated;
    if (msg) {
      if (envelope.message_updated) {
        if (state.messageCache.has(msg.conversation_id)) {
          const cached = state.messageCache.get(msg.conversation_id);
          if (cached && cached.messages) {
            const idx = cached.messages.findIndex(m => (m.id && msg.id && m.id === msg.id) || (m.remote_message_id && msg.remote_message_id && m.remote_message_id === msg.remote_message_id));
            if (idx >= 0) {
              cached.messages[idx] = msg;
              if (idx === cached.messages.length - 1 && state.conversations) {
                const conv = state.conversations.find(c => c.id === msg.conversation_id);
                if (conv) {
                  conv.last_message_preview = msg.body;
                  renderConversations();
                }
              }
            }
          }
        }
        if (msg.is_pinned !== undefined) {
          let list = state.pinnedMessages.get(msg.conversation_id) || [];
          if (msg.is_pinned) {
            const idx = list.findIndex(x => (x.id && msg.id && x.id === msg.id) || (x.remote_message_id && msg.remote_message_id && x.remote_message_id === msg.remote_message_id));
            if (idx >= 0) {
              list[idx] = msg;
            } else {
              list.push(msg);
            }
          } else {
            list = list.filter(x => (x.id && msg.id ? x.id !== msg.id : true) && (x.remote_message_id && msg.remote_message_id ? x.remote_message_id !== msg.remote_message_id : true));
          }
          state.pinnedMessages.set(msg.conversation_id, list);
        }
        if (msg.conversation_id === state.activeConversationId) {
          updateMessageReactionsInDOM(msg.id, msg.reactions);
          updateMessageInDOM(msg);
          updateMessagePinnedInDOM(msg.id, msg.is_pinned);
          updatePinnedBanner();
        }
        return;
      }
      // Un mensaje viejo que llega del proveedor no es un mensaje nuevo:
      // debe insertarse en orden, no agregarse al final del chat.
      const cachedBefore = state.messageCache.get(msg.conversation_id);
      const newestBefore = cachedBefore && Array.isArray(cachedBefore.messages) && cachedBefore.messages.length
        ? cachedBefore.messages.reduce((max, m) => Math.max(max, Number(m.sent_at_unix) || 0), 0)
        : 0;
      const isHistorical = newestBefore > 0 && (Number(msg.sent_at_unix) || 0) < newestBefore;

      // 1. Actualizar el caché local del chat si existe
      if (state.messageCache.has(msg.conversation_id)) {
        const cached = state.messageCache.get(msg.conversation_id);
        if (cached && cached.messages) {
          // Evitar duplicados por id o remote_message_id o coincidencia de contenido saliente reciente
          const idx = cached.messages.findIndex(m => 
            (m.id && msg.id && m.id === msg.id) || 
            (m.remote_message_id && msg.remote_message_id && m.remote_message_id === msg.remote_message_id) ||
            (m.sender_type === msg.sender_type && m.body === msg.body && Math.abs((Number(m.sent_at_unix) || 0) - (Number(msg.sent_at_unix) || 0)) <= 5)
          );
          if (idx >= 0) {
            cached.messages[idx] = msg;
          } else {
            cached.messages.push(msg);
          }
        }
      }

      // 2. Si es el chat activo, agregarlo visualmente y gestionar scroll / leídos
      if (msg.conversation_id === state.activeConversationId && isHistorical) {
        // Historial que llega tarde: re-render en orden cronológico sin saltar la vista.
        scheduleActiveConversationRender();
        return;
      }

      if (msg.conversation_id === state.activeConversationId) {
        const wasAtBottom = isNearBottom(140);
        appendMessageBubble(msg);

        if (wasAtBottom) {
          // Si el usuario ya estaba al fondo, seguirlo automáticamente al nuevo mensaje
          scrollMessagesToBottom(true);
        } else {
          // Si el usuario estaba leyendo mensajes históricos más arriba, no mover la vista bruscamente
          // pero incrementar contador en el botón flotante
          const isOutgoing = msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me';
          if (!isOutgoing) {
            unreadBelowCount++;
          }
          updateScrollBottomButton();
        }

        // Solo se marca leído de inmediato si el usuario realmente está viendo la app;
        // si no, el mensaje entrante queda como no leído hasta recuperar foco/visibilidad.
        const isIncomingMsg = !(msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me');
        if (isIncomingMsg && !isUserViewingApp()) {
          const activeConv = state.conversations.find(c => c.id === msg.conversation_id);
          if (activeConv) activeConv.unread_count = (activeConv.unread_count || 0) + 1;
        } else {
          markActiveConversationAsRead(msg.conversation_id);
        }
      }

      // 3. Cuando alguien escribe o se envía un mensaje, actualizar conversación en memoria y refrescar lista para ir al top
      const conv = state.conversations.find(c => c.id === msg.conversation_id);
      if (conv) {
        conv.last_message_at_unix = msg.sent_at_unix || Math.floor(Date.now() / 1000);
        conv.last_message_preview = msg.body;
        renderConversations();
      }
      loadConversations();

      // 4. Disparar notificación nativa de escritorio y destello en barra de tareas si corresponde
      handleDesktopNotification(msg, isHistorical);
    }
  });

  // Escuchar clic en notificación del sistema operativo para abrir el chat directamente
  if (window.conduitAPI && typeof window.conduitAPI.onOpenConversation === 'function') {
    window.conduitAPI.onOpenConversation((conversationId) => {
      if (conversationId) {
        selectConversation(conversationId);
      }
    });
  }

  // Polling automático continuo de fondo para sincronizar estado de cuentas y nuevos chats
  setInterval(() => {
    if (!state.isSearching) {
      loadConversations();
    }
    // Si hay cuentas en proceso de vinculación (esperando QR), refrescarlas para actualizar imagen o estado connected
    const needsAccountRefresh = state.accounts.some(a => a.status === 'pairing' || (a.status !== 'connected' && a.qr_code));
    if (needsAccountRefresh) {
      loadAccounts();
    }
  }, 4000);
}

// Determina si un mensaje entrante debe disparar notificación de escritorio
function shouldNotifyMessage(msg, isHistorical) {
  if (!msg || isHistorical) return false;

  const isIncoming = !(msg.sender_type === 'USER' || msg.sender_type === 'OUTGOING' || msg.sender_type === 'me');
  if (!isIncoming) return false;

  const settings = state.settings || {};
  if (settings.notifications_enabled === false) return false;

  // Descartar mensajes si son anteriores al arranque de la app (catch-up / historial al iniciar)
  const msgSentAt = Number(msg.sent_at_unix) || 0;
  if (msgSentAt > 0 && state.appStartTime > 0 && msgSentAt < state.appStartTime - 10) {
    return false;
  }

  // Si la ventana no tiene foco o el documento está oculto, siempre notificar
  if (!isUserViewingApp()) return true;

  // Si la ventana está visible pero el mensaje es de otra conversación
  if (msg.conversation_id && msg.conversation_id !== state.activeConversationId) {
    return true;
  }

  return false;
}

// Construye el payload para la notificación respetando el modo de privacidad
function buildNotificationPayload(msg) {
  const settings = state.settings || {};
  const conv = (state.conversations || []).find(c => c.id === msg.conversation_id);

  if (settings.notify_preview === false) {
    return {
      title: 'OmniChat - Nuevo mensaje',
      body: 'Has recibido un nuevo mensaje',
      conversationId: msg.conversation_id,
      silent: settings.notify_sound === false,
    };
  }

  // Modo con vista previa completa
  let title = 'OmniChat';
  const convTitle = (conv && conv.title) ? conv.title.trim() : '';
  const senderName = (msg.sender_name || '').trim();

  if (convTitle && senderName && convTitle !== senderName) {
    title = `${senderName} en ${convTitle}`;
  } else if (senderName) {
    title = senderName;
  } else if (convTitle) {
    title = convTitle;
  }

  let body = (msg.body || '').trim();
  if (!body && Array.isArray(msg.attachments) && msg.attachments.length > 0) {
    const firstAtt = msg.attachments[0];
    const mime = (firstAtt.mime_type || '').toLowerCase();
    const name = firstAtt.file_name || firstAtt.name || '';
    if (mime.startsWith('image/')) {
      body = '📷 Foto';
    } else if (mime.startsWith('audio/') || mime.includes('ogg') || mime.includes('opus')) {
      body = '🎤 Nota de voz';
    } else if (mime.startsWith('video/')) {
      body = '🎥 Video';
    } else {
      body = `📁 Archivo: ${name || 'adjunto'}`;
    }
  }
  if (!body) {
    body = 'Nuevo mensaje';
  }

  return {
    title,
    body,
    conversationId: msg.conversation_id,
    silent: settings.notify_sound === false,
  };
}

// Dispara la notificación y el destello en la barra de tareas
function handleDesktopNotification(msg, isHistorical) {
  if (!shouldNotifyMessage(msg, isHistorical)) return;

  const payload = buildNotificationPayload(msg);

  // Destello en barra de tareas si la ventana no tiene foco
  const settings = state.settings || {};
  if (!isUserViewingApp() && settings.notify_flash_taskbar !== false) {
    if (window.conduitAPI && typeof window.conduitAPI.flashFrame === 'function') {
      window.conduitAPI.flashFrame(true).catch(() => {});
    }
  }

  // Notificación nativa de escritorio vía Electron
  if (window.conduitAPI && typeof window.conduitAPI.showNotification === 'function') {
    window.conduitAPI.showNotification(payload).catch(() => {});
  }
}

// Búsqueda unificada: contactos/grupos arriba y mensajes abajo con nombre real
let searchDebounce = null;
function handleSearch(q) {
  clearTimeout(searchDebounce);
  const query = (q || '').trim();
  if (!query) {
    el.searchClearBtn.classList.add('hidden');
    state.lastConversationsHash = '';
    loadConversations();
    return;
  }
  el.searchClearBtn.classList.remove('hidden');

  searchDebounce = setTimeout(async () => {
    try {
      const qLower = query.toLowerCase();
      // 1. Contactos y grupos coincidentes por título o servidor
      const matchedChats = (state.conversations || []).filter(c => {
        const title = (c.title || '').toLowerCase();
        const serverName = (c.server_name || '').toLowerCase();
        return title.includes(qLower) || serverName.includes(qLower);
      });

      // 2. Mensajes encontrados vía RPC FTS5
      let matchedMsgs = [];
      try {
        matchedMsgs = await window.conduitAPI.searchMessages(query);
      } catch (err) {
        console.error('Error buscando mensajes:', err);
      }

      renderSearchResults(matchedChats, matchedMsgs, query);
    } catch (e) {
      console.error('Error en búsqueda:', e);
    }
  }, 300);
}

function renderSearchResults(matchedChats, matchedMsgs, query) {
  el.conversationList.innerHTML = '';
  const chats = matchedChats || [];
  const msgs = matchedMsgs || [];

  if (chats.length === 0 && msgs.length === 0) {
    el.conversationList.innerHTML = `<div class="loading-state">No se encontraron resultados para "${escapeHTML(query || '')}".</div>`;
    return;
  }

  // 1. Sección superior: Chats y contactos coincidentes
  if (chats.length > 0) {
    const chatHeader = document.createElement('div');
    chatHeader.className = 'search-section-header';
    chatHeader.innerHTML = `
      <span><i class="fa-solid fa-users"></i> Chats y contactos</span>
      <span class="search-section-count">${chats.length}</span>
    `;
    el.conversationList.appendChild(chatHeader);

    chats.forEach(conv => {
      renderChannelItem(conv, false);
    });
  }

  // 2. Sección inferior: Mensajes coincidentes
  if (msgs.length > 0) {
    const msgHeader = document.createElement('div');
    msgHeader.className = 'search-section-header';
    msgHeader.innerHTML = `
      <span><i class="fa-solid fa-message"></i> Mensajes</span>
      <span class="search-section-count">${msgs.length}</span>
    `;
    el.conversationList.appendChild(msgHeader);

    msgs.forEach(m => {
      const item = document.createElement('div');
      item.className = 'conversation-item search-msg-item';
      item.onclick = () => selectConversation(m.conversation_id);

      const conv = (state.conversations || []).find(c => c.id === m.conversation_id);
      const convTitle = (conv && (conv.title || conv.server_name)) || m.sender_name || 'Conversación';

      const timeStr = m.sent_at_unix 
        ? new Date(m.sent_at_unix * 1000).toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
          new Date(m.sent_at_unix * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) 
        : '';

      const provInfo = conv ? providerInfoForConversation(conv) : defaultProvider();
      const provBadge = provInfo.badge;
      const provIcon = provInfo.icon;

      item.innerHTML = `
        <div class="conv-avatar">
          <div class="conv-avatar-initial"><i class="fa-solid fa-magnifying-glass"></i></div>
          <div class="conv-badge-provider ${provBadge}"${providerBadgeStyleAttr(provInfo)}><i class="${provIcon}"></i></div>
        </div>
        <div class="conv-content">
          <div class="conv-header-row">
            <div class="conv-title">${escapeHTML(convTitle)}</div>
            <div class="conv-time">${timeStr}</div>
          </div>
          <div class="conv-snippet">${escapeHTML(m.body || '')}</div>
        </div>
      `;
      el.conversationList.appendChild(item);
    });
  }
}

// Redimensionamiento automático del input de mensaje (hasta 5 renglones)
function resizeMessageInput() {
  if (!el.messageInput) return;
  // Reseteamos altura en línea para obtener el scrollHeight intrínseco del texto
  el.messageInput.style.height = 'auto';
  
  const minHeight = 20; // 1 renglón
  const maxHeight = 100; // 5 renglones (5 * 20px)
  const currentScrollHeight = el.messageInput.scrollHeight;
  
  if (currentScrollHeight > maxHeight) {
    el.messageInput.style.height = `${maxHeight}px`;
    el.messageInput.classList.add('has-overflow');
    el.messageInput.style.overflowY = 'auto';
  } else {
    const finalHeight = Math.max(minHeight, currentScrollHeight);
    el.messageInput.style.height = `${finalHeight}px`;
    el.messageInput.classList.remove('has-overflow');
    el.messageInput.style.overflowY = 'hidden';
  }
}

// ==========================================================================
// Emoji Picker (offline curated grid, inserts at the caret position)
// ==========================================================================

const EMOJI_GROUPS = [
  {
    label: 'Faces',
    emojis: ['😀', '😃', '😄', '😁', '😆', '😅', '😂', '🤣', '😊', '🙂', '😉', '😍', '🥰', '😘', '😎', '🤩', '🥳', '🤔', '🤨', '😐', '😴', '😢', '😭', '😤', '😡', '🤯', '😱', '🤗', '🤫', '🙄']
  },
  {
    label: 'Gestures',
    emojis: ['👍', '👎', '👌', '🤌', '✌️', '🤞', '🤝', '👏', '🙌', '🙏', '💪', '👋', '🤙', '👀', '🫶', '💅']
  },
  {
    label: 'Hearts & Symbols',
    emojis: ['❤️', '🧡', '💛', '💚', '💙', '💜', '🖤', '🤍', '💔', '💯', '🔥', '✨', '⭐', '🎉', '🎊', '✅', '❌', '⚠️', '❓', '❗', '💡', '📌', '🔗', '⏰']
  },
  {
    label: 'Nature',
    emojis: ['🌞', '🌙', '🌈', '☁️', '🌧️', '❄️', '🌊', '🌸', '🌹', '🌻', '🌳', '🌵', '🍀', '🐶', '🐱', '🦊', '🐻', '🐼', '🦁', '🐸', '🐝', '🦋']
  },
  {
    label: 'Objects',
    emojis: ['📎', '📁', '📄', '📷', '🎥', '🎧', '🎵', '🎸', '📱', '💻', '⌨️', '🖥️', '📚', '✏️', '📝', '🍕', '🍔', '☕', '🍺', '🍰', '🚀', '🏆']
  }
];

function buildEmojiPanel() {
  const panel = el.emojiPanel;
  if (!panel || panel.dataset.built === 'true') return;

  EMOJI_GROUPS.forEach(group => {
    const section = document.createElement('div');
    section.className = 'emoji-group';

    const label = document.createElement('div');
    label.className = 'emoji-group-label';
    label.textContent = group.label;
    section.appendChild(label);

    const grid = document.createElement('div');
    grid.className = 'emoji-grid';
    group.emojis.forEach(emoji => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'emoji-item';
      button.textContent = emoji;
      button.title = emoji;
      button.addEventListener('click', () => insertEmojiAtCursor(emoji));
      grid.appendChild(button);
    });

    section.appendChild(grid);
    panel.appendChild(section);
  });

  panel.dataset.built = 'true';
}

function toggleEmojiPanel(force) {
  const panel = el.emojiPanel;
  if (!panel) return;
  buildEmojiPanel();
  const isOpen = !panel.classList.contains('hidden');
  const shouldOpen = force === undefined ? !isOpen : Boolean(force);
  panel.classList.toggle('hidden', !shouldOpen);
}

// Inserta el emoji en la posición del cursor (o reemplazando la selección activa).
function insertEmojiAtCursor(emoji) {
  const input = el.messageInput;
  if (!input) return;
  const value = input.value || '';
  const hasSelection = typeof input.selectionStart === 'number' && typeof input.selectionEnd === 'number';
  const start = hasSelection ? input.selectionStart : value.length;
  const end = hasSelection ? input.selectionEnd : start;

  input.value = value.slice(0, start) + emoji + value.slice(end);
  const caret = start + emoji.length;
  if (hasSelection) {
    input.selectionStart = caret;
    input.selectionEnd = caret;
  }
  if (typeof input.focus === 'function') input.focus();
  resizeMessageInput();
}

function setupComposerMediaListeners() {
  if (el.btnAttachFile) {
    el.btnAttachFile.addEventListener('click', pickAttachmentsWithDialog);
  }

  if (el.btnEmojiPicker) {
    el.btnEmojiPicker.addEventListener('click', (e) => {
      if (e && typeof e.stopPropagation === 'function') e.stopPropagation();
      toggleEmojiPanel();
    });
  }

  if (el.btnRecordAudio) {
    el.btnRecordAudio.addEventListener('click', () => {
      if (state.audioRecorder && state.audioRecorder.isRecording) {
        cancelVoiceRecording();
      } else {
        startVoiceRecording();
      }
    });
  }

  if (el.btnVoiceCancel) {
    el.btnVoiceCancel.addEventListener('click', cancelVoiceRecording);
  }

  if (el.btnVoiceSend) {
    el.btnVoiceSend.addEventListener('click', stopAndSendVoiceRecording);
  }

  if (el.voiceRecorderBar) {
    el.voiceRecorderBar.classList.add('hidden');
  }

  if (el.composer) {
    el.composer.addEventListener('dragover', (e) => {
      e.preventDefault();
    });
    el.composer.addEventListener('drop', (e) => {
      e.preventDefault();
      if (e.dataTransfer && e.dataTransfer.files) {
        addFilesFromFileList(e.dataTransfer.files);
      }
    });
  }

  renderPendingAttachments();
}

// ==========================================================================
// Grabador de Notas de Voz (Voice Recorder)
// ==========================================================================

function formatRecordingTime(totalSeconds) {
  const secs = Math.max(0, Math.floor(totalSeconds));
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

function getSupportedAudioMimeType() {
  if (typeof window === 'undefined' || !window.MediaRecorder || typeof window.MediaRecorder.isTypeSupported !== 'function') {
    return 'audio/webm';
  }
  const candidates = [
    'audio/webm;codecs=opus',
    'audio/webm',
    'audio/ogg;codecs=opus',
    'audio/ogg'
  ];
  for (const mime of candidates) {
    if (window.MediaRecorder.isTypeSupported(mime)) {
      return mime;
    }
  }
  return '';
}

async function startVoiceRecording() {
  if (!state.activeConversationId) {
    alert('Selecciona una conversación para grabar una nota de voz.');
    return;
  }
  if (state.audioRecorder && state.audioRecorder.isRecording) {
    return;
  }
  if (typeof navigator === 'undefined' || !navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
    alert('Tu sistema o navegador no soporta grabación de audio.');
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const mimeType = getSupportedAudioMimeType();
    const options = mimeType ? { mimeType } : undefined;
    const recorder = new MediaRecorder(stream, options);

    state.audioRecorder = {
      stream,
      mediaRecorder: recorder,
      chunks: [],
      timerInterval: null,
      startTime: Date.now(),
      isRecording: true
    };

    recorder.ondataavailable = (e) => {
      if (e && e.data && e.data.size > 0) {
        state.audioRecorder.chunks.push(e.data);
      }
    };

    recorder.start(100);

    // Ajustes visuales en el compositor
    if (el.messageInput) el.messageInput.style.display = 'none';
    if (el.voiceRecorderBar) el.voiceRecorderBar.classList.remove('hidden');
    if (el.btnRecordAudio) el.btnRecordAudio.classList.add('recording');
    if (el.btnSend) el.btnSend.style.display = 'none';
    if (el.voiceRecorderTimer) el.voiceRecorderTimer.textContent = '00:00';

    state.audioRecorder.timerInterval = setInterval(() => {
      if (!state.audioRecorder || !state.audioRecorder.isRecording) return;
      const elapsed = Math.floor((Date.now() - state.audioRecorder.startTime) / 1000);
      if (el.voiceRecorderTimer) {
        el.voiceRecorderTimer.textContent = formatRecordingTime(elapsed);
      }
    }, 500);
  } catch (err) {
    console.error('Error al iniciar grabación de audio:', err);
    alert(`No se pudo acceder al micrófono: ${err.message || err.name}`);
  }
}

function cancelVoiceRecording() {
  if (!state.audioRecorder || !state.audioRecorder.isRecording) return;

  if (state.audioRecorder.timerInterval) {
    clearInterval(state.audioRecorder.timerInterval);
  }

  const recorder = state.audioRecorder.mediaRecorder;
  if (recorder && recorder.state !== 'inactive') {
    recorder.onstop = null;
    try { recorder.stop(); } catch (_) {}
  }

  if (state.audioRecorder.stream) {
    try {
      state.audioRecorder.stream.getTracks().forEach(t => t.stop());
    } catch (_) {}
  }

  state.audioRecorder = {
    stream: null,
    mediaRecorder: null,
    chunks: [],
    timerInterval: null,
    startTime: 0,
    isRecording: false
  };

  // Restaurar UI del compositor
  if (el.voiceRecorderBar) el.voiceRecorderBar.classList.add('hidden');
  if (el.messageInput) el.messageInput.style.display = '';
  if (el.btnSend) el.btnSend.style.display = '';
  if (el.btnRecordAudio) el.btnRecordAudio.classList.remove('recording');
}

async function stopAndSendVoiceRecording() {
  if (!state.audioRecorder || !state.audioRecorder.isRecording) return;

  if (state.audioRecorder.timerInterval) {
    clearInterval(state.audioRecorder.timerInterval);
  }

  const stream = state.audioRecorder.stream;
  const recorder = state.audioRecorder.mediaRecorder;
  const chunks = state.audioRecorder.chunks;

  // Restaurar UI inmediatamente
  state.audioRecorder.isRecording = false;
  if (el.voiceRecorderBar) el.voiceRecorderBar.classList.add('hidden');
  if (el.messageInput) el.messageInput.style.display = '';
  if (el.btnSend) el.btnSend.style.display = '';
  if (el.btnRecordAudio) el.btnRecordAudio.classList.remove('recording');

  return new Promise((resolve) => {
    const finish = async () => {
      if (stream) {
        try { stream.getTracks().forEach(t => t.stop()); } catch (_) {}
      }

      state.audioRecorder = {
        stream: null,
        mediaRecorder: null,
        chunks: [],
        timerInterval: null,
        startTime: 0,
        isRecording: false
      };

      if (!chunks || chunks.length === 0) {
        resolve(null);
        return;
      }

      const mimeType = (recorder && recorder.mimeType) || 'audio/webm';
      const audioBlob = new Blob(chunks, { type: mimeType });
      if (audioBlob.size === 0) {
        resolve(null);
        return;
      }

      try {
        const buffer = await audioBlob.arrayBuffer();
        let voiceFile = null;
        if (window.conduitAPI && typeof window.conduitAPI.saveVoiceNote === 'function') {
          voiceFile = await window.conduitAPI.saveVoiceNote(buffer, mimeType);
        }

        if (!voiceFile || !voiceFile.path) {
          const ext = mimeType.includes('ogg') ? '.ogg' : '.webm';
          const fileName = `voice-note-${Date.now()}${ext}`;
          const filePath = await window.conduitAPI.saveTempAttachment(buffer, fileName);
          if (!filePath) {
            alert('Error al guardar el archivo temporal de audio.');
            resolve(null);
            return;
          }
          voiceFile = {
            path: filePath,
            name: fileName,
            mimeType: mimeType,
            size: audioBlob.size
          };
        }

        const voiceAttachment = {
          file_path: voiceFile.path,
          file_name: voiceFile.name,
          mime_type: voiceFile.mimeType || mimeType,
          file_size: voiceFile.size || audioBlob.size,
          path: voiceFile.path,
          name: voiceFile.name,
          size: voiceFile.size || audioBlob.size,
          mime: voiceFile.mimeType || mimeType
        };

        const body = el.messageInput.value.trim();
        const targetChannel = el.channelSelect.value;
        const replyToId = state.activeReply ? state.activeReply.id : '';
        el.messageInput.value = '';
        resizeMessageInput();
        clearReply();

        const sentMsg = await window.conduitAPI.sendMessage(
          state.activeConversationId,
          body,
          targetChannel,
          replyToId,
          [voiceAttachment]
        );

        if (sentMsg) {
          if (state.messageCache.has(state.activeConversationId)) {
            const cached = state.messageCache.get(state.activeConversationId);
            if (cached && cached.messages && !cached.messages.some(m => m.id === sentMsg.id)) {
              cached.messages.push(sentMsg);
            }
          }
          appendMessageBubble(sentMsg);
          scrollMessagesToBottom();

          const conv = state.conversations.find(c => c.id === state.activeConversationId);
          if (conv) {
            conv.last_message_at_unix = sentMsg.sent_at_unix || Math.floor(Date.now() / 1000);
            conv.last_message_preview = sentMsg.body || '🎤 Nota de voz';
            if (sentMsg.channel_id) {
              conv.active_channel_id = sentMsg.channel_id;
              updateChannelSelector(conv);
            }
            renderConversations();
          }
          loadConversations();
        }
        resolve(sentMsg);
      } catch (err) {
        console.error('Error enviando nota de voz:', err);
        alert(`Error enviando nota de voz: ${err.message}`);
        resolve(null);
      }
    };

    if (recorder && recorder.state !== 'inactive') {
      recorder.onstop = finish;
      try {
        recorder.stop();
      } catch (err) {
        finish();
      }
    } else {
      finish();
    }
  });
}

function setupEmojiPicker() {
  buildEmojiPanel();
  if (el.emojiPanel) el.emojiPanel.classList.add('hidden');

  document.addEventListener('click', (e) => {
    if (el.emojiPanel && !el.emojiPanel.classList.contains('hidden')) {
      if (!el.emojiPanel.contains || (!el.emojiPanel.contains(e.target) && (!el.btnEmojiPicker || e.target !== el.btnEmojiPicker))) {
        toggleEmojiPanel(false);
      }
    }
    // Cerrar popups de reacciones rápidas si se hace clic afuera
    if (el.messagesContainer) {
      el.messagesContainer.querySelectorAll('.quick-reaction-popup:not(.hidden)').forEach(p => {
        if (p.contains && !p.contains(e.target) && (!e.target.closest || !e.target.closest('.btn-action-react'))) {
          p.classList.add('hidden');
        }
      });
    }
  });

  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      if (state.audioRecorder && state.audioRecorder.isRecording) {
        cancelVoiceRecording();
      }
      if (el.emojiPanel && !el.emojiPanel.classList.contains('hidden')) {
        toggleEmojiPanel(false);
      }
      if (el.forwardModal && !el.forwardModal.classList.contains('hidden')) {
        closeForwardModal();
      }
      if (el.messagesContainer) {
        el.messagesContainer.querySelectorAll('.quick-reaction-popup:not(.hidden)').forEach(p => {
          p.classList.add('hidden');
        });
      }
    }
  });
}

// Event Listeners y Switch de Vistas
function setupEventListeners() {
  el.btnSend.addEventListener('click', handleSendMessage);
  if (el.btnCancelReply) {
    el.btnCancelReply.addEventListener('click', clearReply);
  }
  if (el.btnCancelEdit) {
    el.btnCancelEdit.addEventListener('click', cancelEditMessage);
  }
  
  // Eventos para auto-expandir dinámicamente el cuadro de texto
  el.messageInput.addEventListener('input', resizeMessageInput);
  // Pegar: además de redimensionar, adjunta los archivos del portapapeles.
  el.messageInput.addEventListener('paste', (e) => {
    setTimeout(resizeMessageInput, 0);
    const files = e && e.clipboardData ? e.clipboardData.files : null;
    if (files && files.length > 0) {
      e.preventDefault();
      addFilesFromFileList(files);
    }
  });
  el.messageInput.addEventListener('cut', () => setTimeout(resizeMessageInput, 0));

  setupComposerMediaListeners();
  setupEmojiPicker();

  // Listeners para el modal de reenvío
  if (el.forwardBtnClose) {
    el.forwardBtnClose.addEventListener('click', closeForwardModal);
  }
  if (el.forwardModal) {
    el.forwardModal.addEventListener('click', (e) => {
      if (e.target === el.forwardModal) {
        closeForwardModal();
      }
    });
  }
  if (el.forwardSearchInput) {
    el.forwardSearchInput.addEventListener('input', (e) => {
      filterForwardConversations(e.target.value);
    });
  }
  
  el.messageInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSendMessage();
    }
  });

  // Paginación infinita hacia arriba y actualización de botón de scroll
  el.messagesContainer.addEventListener('scroll', () => {
    // 1. Paginación infinita: detectar cuando el usuario hace scroll hacia arriba (tope superior)
    if (el.messagesContainer.scrollTop <= 80) {
      loadOlderMessages();
    }
    // 2. Control dinámico del botón flotante para volver al último mensaje
    updateScrollBottomButton();
    // 3. Si el usuario scrolleó de vuelta al fondo, marcar leídos en conduitd
    if (isNearBottom(60) && state.activeConversationId) {
      markActiveConversationAsRead(state.activeConversationId);
    }
  });

  // Al recuperar foco o visibilidad, apagar destello de barra de tareas y marcar leído lo que quedó pendiente en la conversación activa
  const markIfViewingAtBottom = () => {
    if (window.conduitAPI && typeof window.conduitAPI.flashFrame === 'function') {
      window.conduitAPI.flashFrame(false).catch(() => {});
    }
    if (state.activeConversationId && isUserViewingApp() && isNearBottom(60)) {
      markActiveConversationAsRead(state.activeConversationId);
    }
  };
  window.addEventListener('focus', markIfViewingAtBottom);
  document.addEventListener('visibilitychange', markIfViewingAtBottom);

  // Botón flotante para descender al último mensaje con animación suave
  if (el.btnScrollBottom) {
    el.btnScrollBottom.addEventListener('click', () => {
      scrollMessagesToBottom(true);
      if (state.activeConversationId) {
        markActiveConversationAsRead(state.activeConversationId);
      }
    });
  }

  el.chatSearchInput.addEventListener('input', (e) => handleSearch(e.target.value.trim()));
  el.searchClearBtn.addEventListener('click', () => {
    el.chatSearchInput.value = '';
    handleSearch('');
  });

  el.btnRefreshChats.addEventListener('click', loadConversations);

  if (el.btnRestartApp) {
    el.btnRestartApp.addEventListener('click', async () => {
      const ok = confirm('¿Deseas reiniciar la aplicación y el demonio conduitd?');
      if (ok && window.conduitAPI.restartApp) {
        await window.conduitAPI.restartApp();
      }
    });
  }

  if (el.btnSettingRestartApp) {
    el.btnSettingRestartApp.addEventListener('click', async () => {
      const ok = confirm('¿Deseas reiniciar la aplicación y el demonio conduitd?');
      if (ok && window.conduitAPI.restartApp) {
        await window.conduitAPI.restartApp();
      }
    });
  }

  if (el.btnSettingRestartDaemon) {
    el.btnSettingRestartDaemon.addEventListener('click', async () => {
      if (!window.conduitAPI.restartDaemon) return;
      try {
        el.btnSettingRestartDaemon.disabled = true;
        el.btnSettingRestartDaemon.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Reiniciando...';
        const res = await window.conduitAPI.restartDaemon();
        if (res && res.success) {
          await checkDaemonStatus();
          await loadConversations();
          alert('Demonio conduitd reiniciado exitosamente.');
        } else {
          alert('Error al reiniciar conduitd: ' + (res ? res.error : 'desconocido'));
        }
      } catch (err) {
        alert('Error al reiniciar conduitd: ' + err.message);
      } finally {
        el.btnSettingRestartDaemon.disabled = false;
        el.btnSettingRestartDaemon.innerHTML = '<i class="fa-solid fa-rotate"></i> Reiniciar Demonio';
      }
    });
  }

  el.filterChips.forEach(chip => {
    chip.addEventListener('click', () => {
      el.filterChips.forEach(c => c.classList.remove('active'));
      chip.classList.add('active');
      state.activeFilter = chip.dataset.filter;
      renderConversations();
    });
  });

  // Vistas
  el.tabAllConvs.addEventListener('click', () => {
    setActiveSidebarPill(el.tabAllConvs);
    showView('view-chat');
  });

  el.btnOpenAccounts.addEventListener('click', () => {
    setActiveSidebarPill(el.btnOpenAccounts);
    showView('view-accounts');
  });

  const btnShowAddAcc = document.getElementById('btn-show-add-account-form');
  const addAccBox = document.getElementById('add-account-box');
  const btnCancelAddAcc = document.getElementById('btn-cancel-add-account');
  const btnSubmitAddAcc = document.getElementById('btn-submit-add-account');

  if (btnShowAddAcc && addAccBox) {
    btnShowAddAcc.addEventListener('click', () => {
      addAccBox.classList.remove('hidden');
      syncAccountCredentialFields();
    });
    btnCancelAddAcc.addEventListener('click', () => addAccBox.classList.add('hidden'));
    if (el.newAccountProvider) {
      el.newAccountProvider.addEventListener('change', syncAccountCredentialFields);
    }
    syncAccountCredentialFields();
    btnSubmitAddAcc.addEventListener('click', async () => {
      // El valor es el nombre del enum del manifiesto (o un número en entornos sin manifiestos).
      const rawProvider = el.newAccountProvider.value;
      const provVal = /^\d+$/.test(rawProvider) ? parseInt(rawProvider, 10) : rawProvider;
      const labelVal = el.newAccountLabel.value.trim();
      const identVal = el.newAccountIdentifier.value.trim() || 'default';
      const credentials = buildAccountCredentials();

      if (!labelVal) {
        alert('Ingresa una etiqueta para identificar la cuenta.');
        return;
      }
      const missingField = missingRequiredCredential();
      if (missingField) {
        alert(`Completa el campo obligatorio: ${missingField}`);
        return;
      }

      try {
        const createdAcc = await window.conduitAPI.addAccount(provVal, labelVal, identVal, credentials);
        if (createdAcc) {
          // Intentar conectar inmediatamente: el core reporta error o modo demostración en la cuenta
          await window.conduitAPI.connectAccount(createdAcc.id);
        }
        addAccBox.classList.add('hidden');
        el.newAccountLabel.value = '';
        el.newAccountIdentifier.value = '';
        resetAccountCredentialFields();
        await loadAccounts();
        await loadConversations();
      } catch (err) {
        alert('Error registrando cuenta: ' + err.message);
      }
    });
  }

  // Vistas de Contactos
  if (el.btnContacts) {
    el.btnContacts.addEventListener('click', () => {
      setActiveSidebarPill(el.btnContacts);
      loadContacts();
      showView('view-contacts');
    });
  }

  if (el.btnShowMergeModal && el.mergeContactsBox) {
    el.btnShowMergeModal.addEventListener('click', () => {
      populateMergeSelects();
      el.mergeContactsBox.classList.remove('hidden');
    });

    el.btnCancelMerge.addEventListener('click', () => {
      el.mergeContactsBox.classList.add('hidden');
    });

    el.btnSubmitMerge.addEventListener('click', async () => {
      const primaryId = el.mergePrimarySelect.value;
      const secondaryId = el.mergeSecondarySelect.value;

      if (!primaryId || !secondaryId) {
        alert('Selecciona dos contactos para unificar.');
        return;
      }
      if (primaryId === secondaryId) {
        alert('El contacto principal y secundario no pueden ser el mismo.');
        return;
      }

      try {
        await window.conduitAPI.mergeContacts(primaryId, secondaryId);
        el.mergeContactsBox.classList.add('hidden');
        await loadContacts();
        await loadConversations();
        alert('✅ Contactos unificados exitosamente.');
      } catch (e) {
        alert('Error unificando contactos: ' + e.message);
      }
    });
  }

  // Identidad canónica: definir email/teléfono y vincular manualmente por identidad
  if (el.btnShowIdentityModal && el.identitySetBox) {
    el.btnShowIdentityModal.addEventListener('click', () => {
      populateIdentitySelects();
      el.identitySetBox.classList.remove('hidden');
    });

    el.btnCancelIdentity.addEventListener('click', () => {
      el.identitySetBox.classList.add('hidden');
    });

    el.btnSaveIdentity.addEventListener('click', async () => {
      const contactId = el.identityContactSelect ? el.identityContactSelect.value : '';
      const email = el.identityEmailInput ? el.identityEmailInput.value.trim() : '';
      const phone = el.identityPhoneInput ? el.identityPhoneInput.value.trim() : '';

      if (!contactId) {
        alert('Selecciona un contacto.');
        return;
      }
      if (!email && !phone) {
        alert('Ingresa un email o un teléfono para guardar la identidad.');
        return;
      }

      try {
        await window.conduitAPI.setContactIdentity(contactId, email, phone);
        el.identitySetBox.classList.add('hidden');
        await loadContacts();
        await loadConversations();
      } catch (e) {
        alert('No se pudo guardar la identidad: ' + e.message);
      }
    });

    if (el.identityContactSelect) {
      el.identityContactSelect.addEventListener('change', () => {
        const c = state.contacts.find(x => x.id === el.identityContactSelect.value);
        if (el.identityEmailInput) el.identityEmailInput.value = (c && c.email) || '';
        if (el.identityPhoneInput) el.identityPhoneInput.value = (c && c.phone) || '';
      });
    }
  }

  if (el.btnShowLinkIdentityModal && el.identityLinkBox) {
    el.btnShowLinkIdentityModal.addEventListener('click', () => {
      populateIdentitySelects();
      if (el.identityResults) el.identityResults.innerHTML = '';
      el.identityLinkBox.classList.remove('hidden');
    });

    el.btnCancelLinkIdentity.addEventListener('click', () => {
      el.identityLinkBox.classList.add('hidden');
    });

    el.btnFindIdentity.addEventListener('click', () => {
      searchContactsByIdentity();
    });

    if (el.identityQueryInput) {
      el.identityQueryInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          searchContactsByIdentity();
        }
      });
    }
  }

  el.btnPlugins.addEventListener('click', () => {
    setActiveSidebarPill(el.btnPlugins);
    showView('view-plugins');
  });

  el.btnSettings.addEventListener('click', () => {
    setActiveSidebarPill(el.btnSettings);
    loadSettings();
    showView('view-settings');
  });

  const btnSaveSettings = document.getElementById('btn-save-settings');
  if (btnSaveSettings) {
    btnSaveSettings.addEventListener('click', saveSettingsFromForm);
  }

  document.querySelectorAll('.btn-close-panel').forEach(btn => {
    btn.addEventListener('click', () => {
      setActiveSidebarPill(el.tabAllConvs);
      showView('view-chat');
    });
  });
}

function setActiveSidebarPill(pill) {
  document.querySelectorAll('.provider-pill').forEach(p => p.classList.remove('active'));
  pill.classList.add('active');
}

function showView(viewId) {
  document.querySelectorAll('.view-panel').forEach(p => p.classList.remove('active'));
  const target = document.getElementById(viewId);
  if (target) target.classList.add('active');
}

function escapeHTML(str) {
  return str.replace(/[&<>'"]/g, 
    tag => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[tag] || tag)
  );
}

// Obtener iniciales de dos letras (nombre y apellido) o fallback
function getInitials(name) {
  if (!name || typeof name !== 'string') return '??';
  // Limpiar caracteres especiales de prefijos (ej. emojis, arrobas, números) si los hay
  const cleanName = name.trim();
  if (!cleanName) return '??';
  const parts = cleanName.split(/\s+/).filter(p => p.length > 0);
  if (parts.length >= 2) {
    const first = parts[0].charAt(0).toUpperCase();
    const second = parts[parts.length - 1].charAt(0).toUpperCase();
    return first + second;
  }
  if (parts.length === 1 && parts[0].length >= 2) {
    return parts[0].substring(0, 2).toUpperCase();
  }
  return cleanName.substring(0, 1).toUpperCase();
}

// Genera el contenido interno de un avatar: imagen si existe, o iniciales con soporte onerror
function getAvatarHTML(avatarUrl, name, fallbackIcon = '') {
  const initials = getInitials(name);
  if (avatarUrl && avatarUrl.trim() !== '') {
    const safeUrl = escapeHTML(avatarUrl.trim());
    const safeInitials = escapeHTML(initials);
    return `<img src="${safeUrl}" alt="${safeInitials}" onerror="this.onerror=null; this.parentElement.innerHTML='<span>${safeInitials}</span>';">`;
  }
  if (fallbackIcon) {
    return fallbackIcon;
  }
  return `<span>${escapeHTML(initials)}</span>`;
}

// ==========================================================================
// Multimedia Rendering & Lightbox Viewer (Fotos, Audios, Documentos, GIFs, Videos)
// ==========================================================================

function formatFileSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function getMediaSourceURL(att) {
  if (att.local_path) {
    return `local-media://${encodeURIComponent(att.local_path)}`;
  }
  if (att.url) {
    return att.url;
  }
  return '';
}

// Índice de adjuntos renderizados (id -> adjunto) para resolver acciones desde los handlers inline.
const attachmentIndex = new Map();
// Firma del último render de adjuntos por mensaje, para no reconstruir el DOM (y cortar audio/video) sin cambios.
const attachmentSigByMessage = new Map();
// Adjuntos vigentes por mensaje (los mismos objetos que usa el render), para refrescar una tarjeta.
const attachmentsByMessage = new Map();

function attachmentsSignature(attachments) {
  return (attachments || []).map(a => `${a.id}|${a.download_status || ''}|${a.local_path || ''}`).join(';');
}

function attachmentTypeIcon(att) {
  const mime = (att.mime_type || '').toLowerCase();
  const name = (att.file_name || '').toLowerCase();
  if (mime.startsWith('image/')) return 'fa-solid fa-image';
  if (mime.startsWith('video/')) return 'fa-solid fa-film';
  if (mime.startsWith('audio/')) return 'fa-solid fa-microphone-lines';
  if (mime.includes('pdf') || name.endsWith('.pdf')) return 'fa-solid fa-file-pdf';
  if (mime.includes('zip') || mime.includes('rar') || mime.includes('tar') || mime.includes('compressed')) return 'fa-solid fa-file-zipper';
  return 'fa-solid fa-file';
}

function saveButtonHTML(att) {
  return `<button class="attachment-save-btn" title="Guardar como…" onclick="event.stopPropagation(); saveAttachmentFromUI('${escapeHTML(att.id || '')}')"><i class="fa-solid fa-floppy-disk"></i> Guardar como…</button>`;
}

// Punto de entrada: elige la representación según el estado de descarga del adjunto.
function renderAttachmentHTML(att) {
  if (!att) return '';
  if (att.id) attachmentIndex.set(att.id, att);
  const status = att.download_status || (att.local_path ? 'cached' : '');
  if (status !== 'remote_only' && status !== 'downloading' && status !== 'failed') {
    return `<div class="attachment-item">${renderCachedAttachmentHTML(att)}${saveButtonHTML(att)}</div>`;
  }
  const id = escapeHTML(att.id || '');
  const sizeStr = formatFileSize(att.file_size);
  let stateHTML;
  if (status === 'downloading') {
    stateHTML = `<div class="attachment-spinner" role="progressbar" aria-label="Descargando"><i class="fa-solid fa-circle-notch fa-spin"></i></div>`;
  } else if (status === 'failed') {
    stateHTML = `<button class="attachment-retry-btn" title="Reintentar descarga" onclick="event.stopPropagation(); downloadAttachmentFromCard('${id}')"><i class="fa-solid fa-rotate-right"></i></button>`;
  } else {
    stateHTML = `<button class="attachment-download-btn" title="Descargar" onclick="event.stopPropagation(); downloadAttachmentFromCard('${id}')"><i class="fa-solid fa-download"></i></button>`;
  }
  const meta = status === 'failed'
    ? `Falló la descarga${sizeStr ? ` • ${sizeStr}` : ''}`
    : (status === 'downloading' ? `Descargando…${sizeStr ? ` • ${sizeStr}` : ''}` : sizeStr);
  return `
    <div class="attachment-item">
      <div class="attachment-remote-card status-${status}">
        <div class="media-document-icon"><i class="${attachmentTypeIcon(att)}"></i></div>
        <div class="media-document-info">
          <div class="media-document-name">${escapeHTML(att.file_name || 'Archivo')}</div>
          <div class="media-document-meta">${escapeHTML(meta)}</div>
        </div>
        <div class="attachment-state">${stateHTML}</div>
      </div>
      ${saveButtonHTML(att)}
    </div>
  `;
}

// Reemplaza los adjuntos de la fila del mensaje (si cambió algo) con su representación actual.
function applyAttachmentsToRow(msgId, attachments) {
  if (!el.messagesContainer || !msgId || !attachments || attachments.length === 0) return;
  const sig = attachmentsSignature(attachments);
  if (attachmentSigByMessage.get(msgId) === sig) return;
  const row = el.messagesContainer.querySelector(`.message-row[data-msg-id="${msgId}"]`);
  if (!row) return;
  const container = row.querySelector('.msg-attachments');
  if (!container) return;
  container.innerHTML = attachments.map(a => renderAttachmentHTML(a)).join('');
  attachmentSigByMessage.set(msgId, sig);
  attachmentsByMessage.set(msgId, attachments);
}

function showNotice(text, isError) {
  const toast = document.createElement('div');
  toast.className = `notice-toast${isError ? ' error' : ''}`;
  toast.textContent = text;
  document.body.appendChild(toast);
  setTimeout(() => toast.remove(), 4000);
}

// Tocar para descargar / reintentar: muestra "descargando" de inmediato; el estado final llega por message_updated.
async function downloadAttachmentFromCard(attachmentId) {
  const att = attachmentIndex.get(attachmentId);
  if (!att) return;
  att.download_status = 'downloading';
  refreshMessageAttachments(att.message_id);
  try {
    await window.conduitAPI.downloadAttachment(attachmentId, false);
  } catch (err) {
    console.error('Error descargando adjunto:', err);
    att.download_status = 'failed';
    refreshMessageAttachments(att.message_id);
    showNotice('No se pudo iniciar la descarga', true);
  }
}

function refreshMessageAttachments(msgId) {
  applyAttachmentsToRow(msgId, attachmentsByMessage.get(msgId));
}

// Guardar como…: el diálogo y la copia viven en el proceso principal.
async function saveAttachmentFromUI(attachmentId) {
  const att = attachmentIndex.get(attachmentId);
  if (!att) return;
  try {
    const res = await window.conduitAPI.saveAttachmentAs(att);
    if (res && res.error) {
      showNotice(`No se pudo guardar: ${res.error}`, true);
    } else if (res && !res.canceled) {
      showNotice('Archivo guardado', false);
    }
  } catch (err) {
    console.error('Error guardando adjunto:', err);
    showNotice('No se pudo guardar el archivo', true);
  }
}

function renderCachedAttachmentHTML(att) {
  const src = getMediaSourceURL(att);
  const mime = (att.mime_type || '').toLowerCase();
  const name = att.file_name || 'Archivo';
  const sizeStr = formatFileSize(att.file_size);
  const safePath = escapeHTML(att.local_path || '');

  // 1. Fotos, GIFs y Stickers
  if (mime.startsWith('image/')) {
    const isGif = mime.includes('gif') || name.toLowerCase().endsWith('.gif');
    const isSticker = mime.includes('webp') || name.toLowerCase().startsWith('stk_');
    return `
      <div class="media-photo-wrapper" onclick="openMediaLightbox('${escapeHTML(src)}', 'image', '${escapeHTML(name)}', '${safePath}', '${escapeHTML(att.id || '')}')">
        <img class="media-photo-img" src="${escapeHTML(src)}" alt="${escapeHTML(name)}" loading="lazy" />
        ${isGif ? `<div class="media-photo-overlay"><i class="fa-solid fa-play"></i> GIF</div>` : ''}
        ${isSticker ? `<div class="media-photo-overlay"><i class="fa-solid fa-note-sticky"></i> Sticker</div>` : ''}
      </div>
    `;
  }

  // 2. Videos
  if (mime.startsWith('video/')) {
    return `
      <div class="media-video-wrapper">
        <button class="video-open-lightbox-btn" onclick="openMediaLightbox('${escapeHTML(src)}', 'video', '${escapeHTML(name)}', '${safePath}', '${escapeHTML(att.id || '')}')" title="Ver en ventana grande">
          <i class="fa-solid fa-expand"></i> Pantalla completa
        </button>
        <video class="media-video-el" controls preload="metadata">
          <source src="${escapeHTML(src)}" type="${escapeHTML(mime)}">
          Tu sistema no soporta la reproducción directa de este video.
        </video>
      </div>
    `;
  }

  // 3. Audios y Notas de Voz
  if (mime.startsWith('audio/')) {
    return `
      <div class="media-audio-card">
        <div class="audio-icon-badge"><i class="fa-solid fa-microphone-lines"></i></div>
        <audio controls preload="metadata">
          <source src="${escapeHTML(src)}" type="${escapeHTML(mime)}">
          Tu sistema no soporta audio HTML5.
        </audio>
      </div>
    `;
  }

  // 4. Documentos (PDF, DOCX, ZIP, etc.)
  let docIcon = 'fa-solid fa-file';
  if (mime.includes('pdf') || name.toLowerCase().endsWith('.pdf')) {
    docIcon = 'fa-solid fa-file-pdf';
  } else if (mime.includes('zip') || mime.includes('rar') || mime.includes('tar') || mime.includes('compressed')) {
    docIcon = 'fa-solid fa-file-zipper';
  } else if (mime.includes('word') || mime.includes('officedocument') || name.toLowerCase().endsWith('.docx') || name.toLowerCase().endsWith('.doc')) {
    docIcon = 'fa-solid fa-file-word';
  } else if (mime.includes('excel') || mime.includes('sheet') || name.toLowerCase().endsWith('.xlsx') || name.toLowerCase().endsWith('.csv')) {
    docIcon = 'fa-solid fa-file-excel';
  } else if (mime.includes('text') || name.toLowerCase().endsWith('.txt')) {
    docIcon = 'fa-solid fa-file-lines';
  }

  return `
    <div class="media-document-card" onclick="openMediaDocument('${safePath}')" title="Clic para abrir con la aplicación del sistema">
      <div class="media-document-icon"><i class="${docIcon}"></i></div>
      <div class="media-document-info">
        <div class="media-document-name">${escapeHTML(name)}</div>
        <div class="media-document-meta">${sizeStr ? `${sizeStr} • ` : ''}Clic para abrir</div>
      </div>
      <div class="media-document-action"><i class="fa-solid fa-arrow-up-right-from-square"></i></div>
    </div>
  `;
}

// Abrir documento con el visor de archivos nativo del SO (PDF, LibreOffice, etc.)
async function openMediaDocument(filePath) {
  if (!filePath) {
    alert('El archivo no está disponible localmente en el disco.');
    return;
  }
  try {
    const ok = await window.conduitAPI.openPath(filePath);
    if (!ok) {
      console.warn('No se pudo abrir automáticamente la ruta:', filePath);
    }
  } catch (err) {
    console.error('Error abriendo archivo del sistema:', err);
  }
}

// Variables activas del modal Lightbox y Carrusel
let activeLightboxPath = '';
let activeLightboxAttachmentId = '';
let activeLightboxOriginalUrl = '';
let lightboxPlaylist = [];
let lightboxCurrentIndex = -1;

// ==========================================
// Reenvío de Mensajes (S4, S5, S6)
// ==========================================
function openForwardModal(msg) {
  if (!msg) return;
  state.forwardTargetMessage = msg;

  if (el.forwardPreview) {
    let previewHTML = '';
    const sender = escapeHTML(msg.sender_name || (msg.sender_type === 'me' ? 'Tú' : 'Contacto'));
    previewHTML += `<div class="forward-preview-sender">${sender}</div>`;
    if (msg.body) {
      previewHTML += `<div class="forward-preview-body">${escapeHTML(msg.body)}</div>`;
    }
    if (msg.attachments && msg.attachments.length > 0) {
      previewHTML += `<div class="forward-preview-atts">` + msg.attachments.map(a => `
        <span class="forward-att-chip">
          <i class="fa-solid fa-paperclip"></i> ${escapeHTML(a.file_name || 'Archivo')}
        </span>
      `).join('') + `</div>`;
    }
    el.forwardPreview.innerHTML = previewHTML;
  }

  if (el.forwardSearchInput) {
    el.forwardSearchInput.value = '';
  }

  filterForwardConversations('');

  if (el.forwardModal) {
    el.forwardModal.classList.remove('hidden');
  }

  if (el.forwardSearchInput && typeof el.forwardSearchInput.focus === 'function') {
    el.forwardSearchInput.focus();
  }
}

function closeForwardModal() {
  state.forwardTargetMessage = null;
  if (el.forwardModal) {
    el.forwardModal.classList.add('hidden');
  }
  if (el.forwardSearchInput) {
    el.forwardSearchInput.value = '';
  }
}

function filterForwardConversations(query) {
  if (!el.forwardConvList) return;
  el.forwardConvList.innerHTML = '';
  const q = (query || '').trim().toLowerCase();

  const convs = (state.conversations || []).filter(c => {
    if (!q) return true;
    const title = (c.title || '').toLowerCase();
    const serverName = (c.server_name || '').toLowerCase();
    return title.includes(q) || serverName.includes(q);
  });

  if (convs.length === 0) {
    el.forwardConvList.innerHTML = `<div class="loading-state" style="padding:16px;">No se encontraron conversaciones</div>`;
    return;
  }

  convs.forEach(conv => {
    const item = document.createElement('div');
    item.className = 'forward-conv-item';
    item.dataset.convId = conv.id;
    item.onclick = () => forwardMessageToConversation(conv.id);

    const provInfo = providerInfoForConversation(conv);
    const provBadge = provInfo.badge;
    const provIcon = provInfo.icon;

    const fallbackIcon = conv.channel_type === 'voice' ? '🔊' : (conv.channel_type === 'text' ? '#' : '');
    const avatarContent = getAvatarHTML(conv.avatar_url, conv.title || 'Conversación', fallbackIcon);

    item.innerHTML = `
      <div class="conv-avatar">
        ${avatarContent}
        <div class="conv-badge-provider ${provBadge}"${providerBadgeStyleAttr(provInfo)}><i class="${provIcon}"></i></div>
      </div>
      <div class="conv-content">
        <div class="conv-header-row">
          <div class="conv-title">${escapeHTML(conv.title || 'Conversación')}</div>
        </div>
      </div>
    `;
    el.forwardConvList.appendChild(item);
  });
}

async function forwardMessageToConversation(targetConvId) {
  const msg = state.forwardTargetMessage;
  if (!msg || !targetConvId) return;

  try {
    const outgoingAttachments = [];
    if (msg.attachments && msg.attachments.length > 0) {
      for (const att of msg.attachments) {
        let filePath = att.local_path;
        if (!filePath && (att.download_status === 'remote_only' || !att.download_status)) {
          // Descargar a demanda con wait=true para resolver ruta local
          try {
            const res = await window.conduitAPI.downloadAttachment(att.id, true);
            filePath = (res && res.local_path) || att.local_path;
          } catch (e) {
            console.error('Error descargando adjunto para reenvío:', e);
          }
        }
        if (!filePath) {
          showNotice('No se pudo preparar el archivo adjunto para el reenvío', true);
          return;
        }
        outgoingAttachments.push({
          path: filePath,
          file_path: filePath,
          name: att.file_name || 'adjunto',
          file_name: att.file_name || 'adjunto',
          mime: att.mime_type || 'application/octet-stream',
          mime_type: att.mime_type || 'application/octet-stream',
          size: att.file_size || 0,
          file_size: att.file_size || 0,
        });
      }
    }

    await window.conduitAPI.sendMessage(
      targetConvId,
      msg.body || '',
      null,
      null,
      outgoingAttachments.length > 0 ? outgoingAttachments : null
    );

    closeForwardModal();
    showNotice('Mensaje reenviado');
    selectConversation(targetConvId);
  } catch (err) {
    console.error('Error reenviando mensaje:', err);
    alert('Error al reenviar mensaje: ' + (err.message || err));
  }
}

// Detección y extracción de videos sociales (YouTube, TikTok, Instagram)
function parseSocialVideoLink(text) {
  if (!text || typeof text !== 'string') return null;

  // 1. YouTube (watch?v=, youtu.be/, shorts/, embed/)
  const ytMatch = text.match(/(?:https?:\/\/)?(?:www\.)?(?:youtube\.com\/(?:watch\?v=|embed\/|shorts\/)|youtu\.be\/)([a-zA-Z0-9_-]{11})/i);
  if (ytMatch) {
    const videoId = ytMatch[1];
    return {
      provider: 'youtube',
      badgeClass: 'youtube',
      badgeText: 'YouTube',
      iconClass: 'fa-brands fa-youtube',
      id: videoId,
      url: ytMatch[0].startsWith('http') ? ytMatch[0] : `https://${ytMatch[0]}`,
      embedUrl: `https://www.youtube-nocookie.com/embed/${videoId}?autoplay=1`,
      thumbnailUrl: `https://img.youtube.com/vi/${videoId}/hqdefault.jpg`,
      title: 'Video de YouTube'
    };
  }

  // 2. TikTok (@user/video/1234567890)
  const ttMatch = text.match(/(?:https?:\/\/)?(?:www\.)?tiktok\.com\/@[^\/\s]+\/video\/(\d+)/i);
  if (ttMatch) {
    const videoId = ttMatch[1];
    return {
      provider: 'tiktok',
      badgeClass: 'tiktok',
      badgeText: 'TikTok',
      iconClass: 'fa-brands fa-tiktok',
      id: videoId,
      url: ttMatch[0].startsWith('http') ? ttMatch[0] : `https://${ttMatch[0]}`,
      embedUrl: `https://www.tiktok.com/embed/v2/${videoId}`,
      thumbnailUrl: '',
      title: 'Video de TikTok'
    };
  }

  // 3. Instagram Reels / Posts (/reel/xxx o /p/xxx)
  const igMatch = text.match(/(?:https?:\/\/)?(?:www\.)?instagram\.com\/(?:reel|p|tv)\/([a-zA-Z0-9_-]+)/i);
  if (igMatch) {
    const mediaId = igMatch[1];
    return {
      provider: 'instagram',
      badgeClass: 'instagram',
      badgeText: 'Instagram',
      iconClass: 'fa-brands fa-instagram',
      id: mediaId,
      url: igMatch[0].startsWith('http') ? igMatch[0] : `https://${igMatch[0]}`,
      embedUrl: `https://www.instagram.com/p/${mediaId}/embed/captioned/`,
      thumbnailUrl: '',
      title: 'Video de Instagram'
    };
  }

  return null;
}

function renderSocialVideoCardHTML(social) {
  if (!social) return '';
  const thumb = social.thumbnailUrl 
    ? `<img src="${escapeHTML(social.thumbnailUrl)}" alt="${escapeHTML(social.title)}" loading="lazy" />` 
    : `<i class="${escapeHTML(social.iconClass)}" style="font-size:48px;opacity:0.85;"></i>`;

  return `
    <div class="social-video-card" onclick="openSocialVideoLightbox('${escapeHTML(social.embedUrl)}', '${escapeHTML(social.title)}', '${escapeHTML(social.url)}')">
      <div class="social-video-preview">
        ${thumb}
        <div class="social-video-play-overlay">
          <i class="fa-solid fa-play"></i>
        </div>
      </div>
      <div class="social-video-info">
        <span class="social-video-badge ${escapeHTML(social.badgeClass)}">
          <i class="${escapeHTML(social.iconClass)}"></i> ${escapeHTML(social.badgeText)}
        </span>
        <span class="social-video-link" title="${escapeHTML(social.url)}">${escapeHTML(social.url)}</span>
      </div>
    </div>
  `;
}

// Genera la lista ordenada de elementos multimedia de la conversación activa para el carrusel
function buildConversationMediaPlaylist() {
  const items = [];
  if (!state.activeConversationId || !state.messageCache || !state.messageCache.has(state.activeConversationId)) {
    return items;
  }

  const cached = state.messageCache.get(state.activeConversationId);
  if (!cached || !cached.messages) return items;

  // Orden cronológico
  const sorted = [...cached.messages].sort((a, b) => (Number(a.sent_at_unix) || 0) - (Number(b.sent_at_unix) || 0));

  sorted.forEach(msg => {
    // 1. Adjuntos locales de imagen y video
    if (Array.isArray(msg.attachments)) {
      msg.attachments.forEach(att => {
        const mime = (att.mime_type || '').toLowerCase();
        const isImg = mime.startsWith('image/') || /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(att.file_name || att.local_path || '');
        const isVid = mime.startsWith('video/') || /\.(mp4|webm|mov|mkv|avi)$/i.test(att.file_name || att.local_path || '');

        if (isImg || isVid) {
          const src = getMediaSourceURL(att);
          const name = att.file_name || (isVid ? 'Video' : 'Foto');
          items.push({
            type: isVid ? 'video' : 'image',
            src: src,
            name: name,
            localPath: att.local_path || '',
            attachmentId: att.id || '',
            messageId: msg.id || ''
          });
        }
      });
    }

    // 2. Videos sociales (YouTube, TikTok, Instagram) en el cuerpo del mensaje
    if (msg.body && !msg.poll && !msg.location && !msg.shared_contact) {
      const social = parseSocialVideoLink(msg.body);
      if (social) {
        items.push({
          type: 'embed',
          src: social.embedUrl,
          name: `${social.badgeText}: ${social.title}`,
          localPath: '',
          attachmentId: '',
          embedUrl: social.embedUrl,
          originalUrl: social.url,
          messageId: msg.id || ''
        });
      }
    }
  });

  return items;
}

function renderLightboxCurrentItem() {
  const modal = document.getElementById('media-lightbox-modal');
  const content = document.getElementById('lightbox-content');
  const title = document.getElementById('lightbox-filename');
  const counter = document.getElementById('lightbox-counter');
  const btnPrev = document.getElementById('lightbox-btn-prev');
  const btnNext = document.getElementById('lightbox-btn-next');
  const btnSaveAs = document.getElementById('lightbox-btn-save-as');
  const btnOpenSys = document.getElementById('lightbox-btn-open-system');

  if (!modal || !content || lightboxCurrentIndex < 0 || lightboxCurrentIndex >= lightboxPlaylist.length) return;

  const item = lightboxPlaylist[lightboxCurrentIndex];
  activeLightboxPath = item.localPath || '';
  activeLightboxAttachmentId = item.attachmentId || '';
  activeLightboxOriginalUrl = item.originalUrl || '';

  if (title) title.textContent = item.name || 'Visualizador Multimedia';

  if (counter) {
    if (lightboxPlaylist.length > 1) {
      counter.textContent = `${lightboxCurrentIndex + 1} de ${lightboxPlaylist.length}`;
      counter.classList.remove('hidden');
    } else {
      counter.textContent = '';
      counter.classList.add('hidden');
    }
  }

  // Controles de navegación anterior / siguiente
  if (btnPrev && btnNext) {
    if (lightboxPlaylist.length > 1) {
      btnPrev.classList.remove('hidden');
      btnNext.classList.remove('hidden');
    } else {
      btnPrev.classList.add('hidden');
      btnNext.classList.add('hidden');
    }
  }

  // Botón guardar como
  if (btnSaveAs) {
    if (item.attachmentId) {
      btnSaveAs.classList.remove('hidden');
    } else {
      btnSaveAs.classList.add('hidden');
    }
  }

  // Botón abrir en sistema / abrir en navegador
  if (btnOpenSys) {
    if (item.localPath) {
      btnOpenSys.classList.remove('hidden');
      btnOpenSys.title = 'Abrir con visor predeterminado del sistema';
    } else if (item.originalUrl) {
      btnOpenSys.classList.remove('hidden');
      btnOpenSys.title = 'Abrir enlace en el navegador externo';
    } else {
      btnOpenSys.classList.add('hidden');
    }
  }

  // Render según tipo
  if (item.type === 'image') {
    content.innerHTML = `<img src="${escapeHTML(item.src)}" alt="${escapeHTML(item.name)}" />`;
  } else if (item.type === 'video') {
    content.innerHTML = `<video src="${escapeHTML(item.src)}" controls autoplay style="max-width:100%;max-height:80vh;border-radius:6px;outline:none;"></video>`;
  } else if (item.type === 'embed') {
    content.innerHTML = `<iframe src="${escapeHTML(item.embedUrl)}" allow="autoplay; encrypted-media; picture-in-picture; fullscreen" allowfullscreen></iframe>`;
  }

  modal.classList.remove('hidden');
}

function lightboxNext() {
  if (lightboxPlaylist.length <= 1) return;
  lightboxCurrentIndex = (lightboxCurrentIndex + 1) % lightboxPlaylist.length;
  renderLightboxCurrentItem();
}

function lightboxPrev() {
  if (lightboxPlaylist.length <= 1) return;
  lightboxCurrentIndex = (lightboxCurrentIndex - 1 + lightboxPlaylist.length) % lightboxPlaylist.length;
  renderLightboxCurrentItem();
}

function openMediaLightbox(mediaSrc, mediaType, fileName, localPath, attachmentId) {
  lightboxPlaylist = buildConversationMediaPlaylist();

  // Buscar el elemento en la playlist
  let matchIndex = lightboxPlaylist.findIndex(item => 
    (attachmentId && item.attachmentId === attachmentId) ||
    (localPath && item.localPath === localPath) ||
    (mediaSrc && item.src === mediaSrc)
  );

  if (matchIndex < 0) {
    const singleItem = {
      type: mediaType || 'image',
      src: mediaSrc,
      name: fileName || 'Archivo Multimedia',
      localPath: localPath || '',
      attachmentId: attachmentId || ''
    };
    lightboxPlaylist.push(singleItem);
    matchIndex = lightboxPlaylist.length - 1;
  }

  lightboxCurrentIndex = matchIndex;
  renderLightboxCurrentItem();
}

function openSocialVideoLightbox(embedUrl, title, originalUrl) {
  lightboxPlaylist = buildConversationMediaPlaylist();

  let matchIndex = lightboxPlaylist.findIndex(item => 
    (embedUrl && item.embedUrl === embedUrl) ||
    (originalUrl && item.originalUrl === originalUrl)
  );

  if (matchIndex < 0) {
    const singleItem = {
      type: 'embed',
      src: embedUrl,
      name: title || 'Video',
      localPath: '',
      attachmentId: '',
      embedUrl: embedUrl,
      originalUrl: originalUrl || ''
    };
    lightboxPlaylist.push(singleItem);
    matchIndex = lightboxPlaylist.length - 1;
  }

  lightboxCurrentIndex = matchIndex;
  renderLightboxCurrentItem();
}

function closeMediaLightbox() {
  const modal = document.getElementById('media-lightbox-modal');
  const content = document.getElementById('lightbox-content');
  if (!modal) return;
  modal.classList.add('hidden');
  if (content) content.innerHTML = '';
  activeLightboxPath = '';
  activeLightboxAttachmentId = '';
  activeLightboxOriginalUrl = '';
  lightboxCurrentIndex = -1;
  lightboxPlaylist = [];
}

// Configurar eventos del modal Lightbox y Carrusel
document.addEventListener('DOMContentLoaded', () => {
  const modal = document.getElementById('media-lightbox-modal');
  const btnClose = document.getElementById('lightbox-btn-close');
  const btnOpenSys = document.getElementById('lightbox-btn-open-system');
  const btnPrev = document.getElementById('lightbox-btn-prev');
  const btnNext = document.getElementById('lightbox-btn-next');

  if (btnClose) {
    btnClose.addEventListener('click', closeMediaLightbox);
  }

  if (btnPrev) {
    btnPrev.addEventListener('click', (e) => {
      e.stopPropagation();
      lightboxPrev();
    });
  }

  if (btnNext) {
    btnNext.addEventListener('click', (e) => {
      e.stopPropagation();
      lightboxNext();
    });
  }

  if (btnOpenSys) {
    btnOpenSys.addEventListener('click', () => {
      if (activeLightboxPath) {
        openMediaDocument(activeLightboxPath);
      } else if (activeLightboxOriginalUrl && window.conduitAPI && typeof window.conduitAPI.openExternal === 'function') {
        window.conduitAPI.openExternal(activeLightboxOriginalUrl);
      }
    });
  }

  const btnSaveAs = document.getElementById('lightbox-btn-save-as');
  if (btnSaveAs) {
    btnSaveAs.addEventListener('click', () => {
      if (activeLightboxAttachmentId) saveAttachmentFromUI(activeLightboxAttachmentId);
    });
  }

  if (modal) {
    modal.addEventListener('click', (e) => {
      if (e.target === modal) {
        closeMediaLightbox();
      }
    });
  }

  // Navegación drill-down: volver del servidor a la lista unificada
  if (el.btnServerBack) {
    el.btnServerBack.addEventListener('click', closeServer);
  }

  window.addEventListener('keydown', (e) => {
    const modal = document.getElementById('media-lightbox-modal');
    const isModalOpen = modal && !modal.classList.contains('hidden');

    if (e.key === 'Escape') {
      if (isModalOpen) {
        closeMediaLightbox();
        return;
      }
      if (el.forwardModal && !el.forwardModal.classList.contains('hidden')) {
        closeForwardModal();
        return;
      }
      if (state.activeServerId) {
        closeServer();
      }
      return;
    }

    if (isModalOpen) {
      if (e.key === 'ArrowLeft') {
        e.preventDefault();
        lightboxPrev();
      } else if (e.key === 'ArrowRight') {
        e.preventDefault();
        lightboxNext();
      }
    }
  });
});
