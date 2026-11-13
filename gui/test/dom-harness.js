// Harness compartido para probar la lógica real de gui/src/app.js sin navegador.
// Simula el DOM mínimo y expone los símbolos internos necesarios para inspeccionar el estado.

const fs = require('fs');
const path = require('path');
const vm = require('vm');

const APP_JS = path.join(__dirname, '..', 'src', 'app.js');

function makeEl(id) {
  const classes = new Set();
  const listeners = {};
  return {
    id,
    tagName: id,
    children: [],
    _html: '',
    get className() { return Array.from(classes).join(' '); },
    set className(val) {
      classes.clear();
      if (val) String(val).split(/\s+/).filter(Boolean).forEach(c => classes.add(c));
    },
    style: {
      setProperty(prop, val) { this[prop] = String(val); },
      getPropertyValue(prop) { return this[prop] || ''; },
    },
    dataset: {},
    value: '',
    textContent: '',
    scrollTop: 0,
    scrollHeight: 0,
    clientHeight: 0,
    classList: {
      add: (...c) => c.forEach(x => classes.add(x)),
      remove: (...c) => c.forEach(x => classes.delete(x)),
      contains: c => classes.has(c),
      // Replica la firma del DOM: toggle(token, force) respeta `force` cuando se pasa.
      toggle: (c, force) => {
        const shouldAdd = force === undefined ? !classes.has(c) : Boolean(force);
        if (shouldAdd) classes.add(c); else classes.delete(c);
        return shouldAdd;
      },
    },
    appendChild(child) { child.parentNode = this; this.children.push(child); return child; },
    prepend(child) { child.parentNode = this; this.children.unshift(child); return child; },
    insertBefore(child, ref) {
      child.parentNode = this;
      const idx = this.children.indexOf(ref);
      if (idx >= 0) this.children.splice(idx, 0, child);
      else this.children.push(child);
      return child;
    },
    remove() {
      if (this.parentNode && this.parentNode.children) {
        const i = this.parentNode.children.indexOf(this);
        if (i >= 0) this.parentNode.children.splice(i, 1);
      }
    },
    addEventListener(evt, cb) {
      if (!listeners[evt]) listeners[evt] = [];
      listeners[evt].push(cb);
    },
    removeEventListener(evt, cb) {
      if (!listeners[evt]) return;
      const idx = listeners[evt].indexOf(cb);
      if (idx >= 0) listeners[evt].splice(idx, 1);
    },
    click() {
      if (typeof this.onclick === 'function') this.onclick({ stopPropagation() {}, preventDefault() {} });
      if (listeners['click']) {
        listeners['click'].forEach(cb => cb({ stopPropagation() {}, preventDefault() {} }));
      }
    },
    setAttribute(k, v) { this[k] = v; },
    getAttribute(k) { return this[k]; },
    focus() {},
    scrollTo() {},
    querySelector(sel) {
      if (sel && sel.includes('[data-msg-id="')) {
        const m = sel.match(/\[data-msg-id="([^"]+)"\]/);
        if (m) return this.children.find(c => c.dataset && c.dataset.msgId === m[1]) || null;
      }
      if (sel && sel.includes('[data-remote-id="')) {
        const m = sel.match(/\[data-remote-id="([^"]+)"\]/);
        if (m) return this.children.find(c => c.dataset && c.dataset.remoteId === m[1]) || null;
      }
      if (this.id === 'daemon-status-indicator' && sel && sel.includes('status-dot')) {
        return makeEl('status-dot');
      }
      // Contenedor de adjuntos: devuelve una vista con escritura que reemplaza el bloque dentro del HTML de la fila.
      if (sel === '.msg-attachments' && this._html) {
        const view = attachmentsBlockView(this);
        if (view) return view;
      }
      if (sel && sel.startsWith('.')) {
        const cls = sel.slice(1);
        const found = this.children.find(c => c.classList && c.classList.contains(cls));
        if (found) return found;
        for (const child of this.children) {
          if (child.querySelector) {
            const nested = child.querySelector(sel);
            if (nested) return nested;
          }
        }
        if (this._html && !this._html.includes(cls)) return null;
        if (this._html && this._html.includes(cls)) {
          const el = makeEl(cls);
          el.innerHTML = this._html;
          return el;
        }
      }
      return makeEl('nested');
    },
    querySelectorAll(sel) {
      const results = [];
      function search(node) {
        if (!node) return;
        if (sel && sel.startsWith('.')) {
          const cls = sel.slice(1);
          if (node.classList && node.classList.contains(cls)) {
            results.push(node);
          }
        }
        if (Array.isArray(node.children)) {
          node.children.forEach(search);
        }
      }
      if (Array.isArray(this.children)) {
        this.children.forEach(search);
      }
      return results;
    },
    get innerHTML() {
      if (!this.children || this.children.length === 0) return this._html;
      return this._html + this.children.map(c => c.innerHTML || c._html || '').join('');
    },
    set innerHTML(v) { this._html = v; if (v === '') this.children = []; },
  };
}

// Ubica `<div class="msg-attachments">…</div>` (con balance de divs) en el HTML de `owner`
// y expone un elemento cuyo innerHTML lee/escribe solo ese bloque.
function attachmentsBlockView(owner) {
  const html = owner._html;
  const open = html.indexOf('<div class="msg-attachments">');
  if (open < 0) return null;
  const innerStart = open + '<div class="msg-attachments">'.length;
  let depth = 1;
  let i = innerStart;
  const re = /<div\b|<\/div>/g;
  re.lastIndex = innerStart;
  let m;
  while ((m = re.exec(html))) {
    depth += m[0] === '</div>' ? -1 : 1;
    if (depth === 0) { i = m.index; break; }
  }
  if (depth !== 0) return null;
  const view = makeEl('msg-attachments');
  Object.defineProperty(view, 'innerHTML', {
    get() { return owner._html.slice(innerStart, i); },
    set(v) { owner._html = owner._html.slice(0, innerStart) + v + owner._html.slice(i); },
  });
  return view;
}

// Crea un entorno aislado con app.js ejecutándose contra datos de prueba.
function createHarness(fixtures = {}) {
  const {
    conversations = [],
    contacts = [],
    accounts = [],
    plugins = [],
    identityResults = [],
    pinnedMessages = {},
  } = fixtures;

  const calls = { merge: [], identity: [], alerts: [], sendMessage: [], sendReaction: [], sendVote: [], clickButton: [], editMessage: [], deleteMessage: [], pinMessage: [], getPinnedMessages: [], installPlugin: [], togglePlugin: [], saveTemp: [], pickFiles: [], downloadAttachment: [], saveAttachmentAs: [], openExternal: [], openPath: [] };

  const registry = new Map();
  const docListeners = {};
  const winListeners = {};
  const documentStub = {
    _ready: [],
    getElementById(id) {
      if (!registry.has(id)) registry.set(id, makeEl(id));
      return registry.get(id);
    },
    querySelectorAll() { return []; },
    querySelector() { return makeEl('q'); },
    createElement(tag) { return makeEl(tag); },
    // Foco/visibilidad configurables desde los tests (por defecto: ventana visible y con foco).
    _focused: true,
    visibilityState: 'visible',
    hasFocus() { return documentStub._focused; },
    addEventListener(evt, cb) {
      if (evt === 'DOMContentLoaded') documentStub._ready.push(cb);
      (docListeners[evt] = docListeners[evt] || []).push(cb);
    },
    body: makeEl('body'),
    documentElement: makeEl('html'),
  };

  let eventListeners = [];
  let openConversationListeners = [];
  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => {},
    localStorage: { getItem: () => null, setItem: () => {} },
    document: documentStub,
    Blob,
    MediaRecorder: class MockMediaRecorder {
      constructor(stream, options = {}) {
        this.stream = stream;
        this.mimeType = options.mimeType || 'audio/webm;codecs=opus';
        this.state = 'inactive';
        this.ondataavailable = null;
        this.onstop = null;
      }
      static isTypeSupported(type) {
        return Boolean(type && (type.includes('webm') || type.includes('ogg')));
      }
      start(timeslice) {
        this.state = 'recording';
        if (this.ondataavailable) {
          this.ondataavailable({ data: new Blob(['mock-audio-chunk'], { type: this.mimeType }) });
        }
      }
      stop() {
        this.state = 'inactive';
        if (this.ondataavailable) {
          this.ondataavailable({ data: new Blob(['mock-audio-chunk-2'], { type: this.mimeType }) });
        }
        if (this.onstop) this.onstop();
      }
    },
    navigator: {
      clipboard: { writeText: async () => {} },
      mediaDevices: {
        getUserMedia: async () => ({
          getTracks: () => [{ stop: () => {} }]
        })
      }
    },
    alert: (msg) => { calls.alerts.push(String(msg)); },
    addEventListener(name, fn) {
      if (name === 'DOMContentLoaded') {
        documentStub._ready.push(fn);
      }
      (winListeners[name] = winListeners[name] || []).push(fn);
    },
    conduitAPI: {
      getStatus: async () => ({ version: 'test' }),
      getDaemonInfo: async () => ({ isOwned: true, isRunning: true, port: 50051 }),
      restartDaemon: async () => ({ success: true }),
      restartApp: async () => ({ success: true }),
      listConversations: async () => conversations,
      getMessages: async (convId, limit, beforeId) => {
        calls.getMessages = calls.getMessages || [];
        calls.getMessages.push([convId, limit, beforeId]);
        if (typeof fixtures.getMessages === 'function') {
          return fixtures.getMessages(convId, limit, beforeId);
        }
        if (fixtures.messages) {
          if (Array.isArray(fixtures.messages)) return fixtures.messages;
          if (fixtures.messages[convId]) return fixtures.messages[convId];
        }
        return [];
      },
      getPinnedMessages: async (convId) => {
        calls.getPinnedMessages.push(convId);
        if (typeof fixtures.getPinnedMessages === 'function') {
          return fixtures.getPinnedMessages(convId);
        }
        if (fixtures.pinnedMessages) {
          if (Array.isArray(fixtures.pinnedMessages)) return fixtures.pinnedMessages;
          if (fixtures.pinnedMessages[convId]) return fixtures.pinnedMessages[convId];
        }
        return [];
      },
      markConversationRead: async (conversationId) => {
        calls.markConversationRead = calls.markConversationRead || [];
        calls.markConversationRead.push(conversationId);
        return { success: true, marked_count: 0 };
      },
      listAccounts: async () => accounts,
      listPlugins: async () => plugins,
      listContacts: async () => contacts,
      getSettings: async () => ({}),
      onEvent: (cb) => {
        eventListeners.push(cb);
        return () => {
          eventListeners = eventListeners.filter(l => l !== cb);
        };
      },
      searchMessages: async (q) => {
        calls.searchMessages = calls.searchMessages || [];
        calls.searchMessages.push(q);
        if (typeof fixtures.searchMessages === 'function') {
          return fixtures.searchMessages(q);
        }
        return fixtures.searchedMessages || [];
      },
      findContactsByIdentity: async () => identityResults,
      mergeContacts: async (primaryId, secondaryId) => {
        calls.merge.push([primaryId, secondaryId]);
        return {};
      },
      setContactIdentity: async (contactId, email, phone) => {
        calls.identity.push([contactId, email, phone]);
        return {};
      },
      sendMessage: async (conversationId, body, targetChannelId, replyToMessageId, attachments) => {
        calls.sendMessage.push([conversationId, body, targetChannelId, replyToMessageId, attachments]);
        return {
          id: `sent-${calls.sendMessage.length}`,
          conversation_id: conversationId,
          body,
          sender_type: 'me',
          sent_at_unix: 1700000000,
          attachments: [],
        };
      },
      sendReaction: async (messageId, emoji) => {
        calls.sendReaction.push([messageId, emoji]);
        return { success: true };
      },
      sendVote: async (messageId, selectedOptions) => {
        calls.sendVote.push([messageId, selectedOptions]);
        return { success: true };
      },
      clickButton: async (messageId, buttonId, buttonText) => {
        calls.clickButton.push([messageId, buttonId, buttonText]);
        return { success: true, feedback_message: 'Confirmado' };
      },
      editMessage: async (messageId, newBody) => {
        calls.editMessage.push([messageId, newBody]);
        return { id: messageId, body: newBody, is_edited: true };
      },
      deleteMessage: async (messageId) => {
        calls.deleteMessage.push([messageId]);
        return { success: true };
      },
      pinMessage: async (messageId, pinned) => {
        calls.pinMessage.push([messageId, pinned]);
        return { success: true, is_pinned: pinned };
      },
      installPlugin: async (pluginId) => {
        calls.installPlugin.push([pluginId]);
        return { success: true, message: `Plugin ${pluginId} instalado` };
      },
      togglePlugin: async (pluginId, enabled) => {
        calls.togglePlugin.push([pluginId, enabled]);
        return { success: true };
      },
      openPath: async (filePath) => {
        calls.openPath.push(filePath);
        return true;
      },
      openExternal: async (url) => {
        calls.openExternal.push(url);
        return true;
      },
      downloadAttachment: async (attachmentId, wait) => {
        calls.downloadAttachment.push([attachmentId, wait]);
        if (wait) {
          return { id: attachmentId, download_status: 'cached', local_path: `/tmp/conduit-harness/${attachmentId}` };
        }
        return { id: attachmentId, download_status: 'downloading' };
      },
      saveAttachmentAs: async (attachment) => {
        calls.saveAttachmentAs.push(attachment);
        return fixtures.saveResult || { canceled: false, path: '/tmp/out/' + attachment.file_name };
      },
      pickFiles: async () => {
        calls.pickFiles.push(true);
        return [];
      },
      // Electron 33: File.path no existe, el path se resuelve con webUtils.getPathForFile.
      getPathForFile: (file) => (file && file.path) || '',
      saveTempAttachment: async (buffer, name) => {
        calls.saveTemp.push([buffer, name]);
        return `/tmp/conduit-harness/${name}`;
      },
      saveVoiceNote: async (buffer, mimeType) => {
        calls.saveVoiceNote = calls.saveVoiceNote || [];
        calls.saveVoiceNote.push([buffer, mimeType]);
        const name = `voice-note-${Date.now()}.ogg`;
        return {
          path: `/tmp/conduit-harness/${name}`,
          name,
          mimeType: 'audio/ogg; codecs=opus',
          size: (buffer && buffer.byteLength) || 1024
        };
      },
      showNotification: async (opts) => {
        calls.showNotification = calls.showNotification || [];
        calls.showNotification.push(opts);
        return { supported: true, shown: true };
      },
      flashFrame: async (flag) => {
        calls.flashFrame = calls.flashFrame || [];
        calls.flashFrame.push(flag);
        return true;
      },
      onOpenConversation: (cb) => {
        openConversationListeners.push(cb);
        return () => {
          openConversationListeners = openConversationListeners.filter(l => l !== cb);
        };
      },
    },
  };
  // En el renderer real `window` ES el objeto global; el sandbox replica esa identidad
  // para que las asignaciones `window.fn = ...` queden accesibles como funciones globales.
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;

  vm.createContext(sandbox);

  const exports = [
    'state', 'el',
    'openServer', 'closeServer', 'renderConversations',
    'renderContactsGrid', 'populateIdentitySelects', 'populateMergeSelects',
    'searchContactsByIdentity', 'loadContacts',
    'renderAccountsGrid', 'syncAccountCredentialFields', 'buildAccountCredentials', 'qrCopyFor',
    // Composer multimedios
    'handleSendMessage', 'addPendingAttachment', 'removePendingAttachment',
    'getPendingAttachments', 'clearPendingAttachments', 'renderPendingAttachments',
    'addFilesFromFileList', 'detectMimeType', 'classifyAttachmentType',
    'insertEmojiAtCursor', 'toggleEmojiPanel',
    // Citas y Reacciones
    'renderMessages', 'setReplyTarget', 'clearReply', 'toggleReaction', 'isOnlyEmojis',
    // Encuestas (Polls)
    'handleVotePoll',
    // Botones Interactivos
    'handleClickButton',
    // Edición, Borrado y Fijado
    'startEditMessage', 'cancelEditMessage', 'handleDeleteMessage', 'handlePinMessage', 'updatePinnedBanner',
    // Plugins y Ajustes
    'renderPlugins', 'loadPlugins', 'applySettingsUI', 'applySettingsToForm', 'saveSettingsFromForm',
    // Lecturas y sincronización
    'markActiveConversationAsRead', 'setupRealtimeEvents',
    // Adjuntos a demanda
    'renderAttachmentHTML', 'downloadAttachmentFromCard', 'saveAttachmentFromUI',
    // Búsqueda y reenvío
    'handleSearch', 'renderSearchResults',
    'openForwardModal', 'closeForwardModal', 'filterForwardConversations', 'forwardMessageToConversation',
    // Grabador de notas de voz
    'startVoiceRecording', 'cancelVoiceRecording', 'stopAndSendVoiceRecording', 'formatRecordingTime', 'getSupportedAudioMimeType',
    // Notificaciones de escritorio y navegación
    'shouldNotifyMessage', 'buildNotificationPayload', 'handleDesktopNotification', 'selectConversation',
  ];
  const source = fs.readFileSync(APP_JS, 'utf8')
    + '\n;' + exports.map(name => `globalThis.__${name} = ${name};`).join(' ');

  vm.runInContext(source, sandbox, { filename: 'app.js' });

  async function start() {
    await Promise.all(documentStub._ready.map(cb => cb()));
    await new Promise(r => setTimeout(r, 20));
  }

  const api = {
    documentStub,
    sandbox,
    calls,
    start,
    triggerOpenConversation: (convId) => {
      openConversationListeners.forEach(cb => cb(convId));
    },
    // Simula el estado de la ventana y dispara los eventos de foco/visibilidad.
    setWindowState: ({ focused, visibility }) => {
      if (focused !== undefined) documentStub._focused = focused;
      if (visibility !== undefined) documentStub.visibilityState = visibility;
    },
    fireWindowEvent: (name, extra = {}) => (winListeners[name] || []).forEach(fn => fn({ type: name, preventDefault() {}, stopPropagation() {}, ...extra })),
    fireDocumentEvent: (name, extra = {}) => (docListeners[name] || []).forEach(fn => fn({ type: name, preventDefault() {}, stopPropagation() {}, ...extra })),
    emitEvent: (envelope) => {
      eventListeners.forEach(fn => fn(envelope));
    }
  };
  exports.forEach(name => { api[name] = sandbox[`__${name}`]; });
  return api;
}

// Contador de aserciones con salida legible.
function createChecker() {
  let failures = 0;
  return {
    check(name, cond, detail) {
      if (cond) {
        console.log(`PASS  ${name}`);
      } else {
        console.log(`FAIL  ${name}${detail ? ' :: ' + detail : ''}`);
        failures++;
      }
    },
    finish() {
      console.log(failures === 0 ? '\nRESULT: ALL_CHECKS_PASSED' : `\nRESULT: ${failures} FAILURES`);
      process.exit(failures === 0 ? 0 : 1);
    },
  };
}

module.exports = { createHarness, createChecker, makeEl };
