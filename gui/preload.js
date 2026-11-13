const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('conduitAPI', {
  // Conexión y Estado
  getStatus: () => ipcRenderer.invoke('conduit:getStatus'),
  getDaemonInfo: () => ipcRenderer.invoke('conduit:getDaemonInfo'),
  restartDaemon: () => ipcRenderer.invoke('conduit:restartDaemon'),
  restartApp: () => ipcRenderer.invoke('conduit:restartApp'),
  
  // Conversaciones y Mensajes
  listConversations: (limit, offset) => ipcRenderer.invoke('conduit:listConversations', limit, offset),
  getMessages: (conversationId, limit, beforeMessageId) => ipcRenderer.invoke('conduit:getMessages', conversationId, limit, beforeMessageId),
  markConversationRead: (conversationId) => ipcRenderer.invoke('conduit:markConversationRead', conversationId),
  sendMessage: (conversationId, body, targetChannelId, replyToMessageId, attachments) => 
    ipcRenderer.invoke('conduit:sendMessage', conversationId, body, targetChannelId, replyToMessageId, attachments),
  sendReaction: (messageId, emoji) => ipcRenderer.invoke('conduit:sendReaction', messageId, emoji),
  sendVote: (messageId, selectedOptions) => ipcRenderer.invoke('conduit:sendVote', messageId, selectedOptions),
  clickButton: (messageId, buttonId, buttonText) => ipcRenderer.invoke('conduit:clickButton', messageId, buttonId, buttonText),
  editMessage: (messageId, newBody) => ipcRenderer.invoke('conduit:editMessage', messageId, newBody),
  deleteMessage: (messageId) => ipcRenderer.invoke('conduit:deleteMessage', messageId),
  pinMessage: (messageId, pinned) => ipcRenderer.invoke('conduit:pinMessage', messageId, pinned),
  getPinnedMessages: (conversationId) => ipcRenderer.invoke('conduit:getPinnedMessages', conversationId),
  searchMessages: (query) => ipcRenderer.invoke('conduit:searchMessages', query),

  // Cuentas y Plugins
  listAccounts: () => ipcRenderer.invoke('conduit:listAccounts'),
  addAccount: (provider, label, identifier, credentials) => ipcRenderer.invoke('conduit:addAccount', provider, label, identifier, credentials),
  removeAccount: (accountId) => ipcRenderer.invoke('conduit:removeAccount', accountId),
  connectAccount: (accountId) => ipcRenderer.invoke('conduit:connectAccount', accountId),
  disconnectAccount: (accountId) => ipcRenderer.invoke('conduit:disconnectAccount', accountId),
  listPlugins: () => ipcRenderer.invoke('conduit:listPlugins'),
  installPlugin: (pluginId) => ipcRenderer.invoke('conduit:installPlugin', pluginId),
  togglePlugin: (pluginId, enabled) => ipcRenderer.invoke('conduit:togglePlugin', pluginId, enabled),
  listContacts: () => ipcRenderer.invoke('conduit:listContacts'),
  mergeContacts: (primaryId, secondaryId) => ipcRenderer.invoke('conduit:mergeContacts', primaryId, secondaryId),
  splitChannel: (channelId, newName) => ipcRenderer.invoke('conduit:splitChannel', channelId, newName),
  setContactIdentity: (contactId, email, phone) => ipcRenderer.invoke('conduit:setContactIdentity', contactId, email, phone),
  findContactsByIdentity: (query) => ipcRenderer.invoke('conduit:findContactsByIdentity', query),
  getSettings: () => ipcRenderer.invoke('conduit:getSettings'),
  updateSettings: (settings) => ipcRenderer.invoke('conduit:updateSettings', settings),

  // Multimedia
  openPath: (filePath) => ipcRenderer.invoke('conduit:openPath', filePath),
  openExternal: (url) => ipcRenderer.invoke('conduit:openExternal', url),
  downloadAttachment: (attachmentId, wait) => ipcRenderer.invoke('conduit:downloadAttachment', attachmentId, wait),
  saveAttachmentAs: (attachment) => ipcRenderer.invoke('conduit:saveAttachmentAs', attachment),
  pickFiles: () => ipcRenderer.invoke('conduit:pickFiles'),
  saveTempAttachment: (buffer, name) => ipcRenderer.invoke('conduit:saveTempAttachment', buffer, name),
  saveVoiceNote: (buffer, mimeType) => ipcRenderer.invoke('conduit:saveVoiceNote', buffer, mimeType),
  // Electron 33 removed File.path; the renderer resolves it through webUtils.
  getPathForFile: (file) => require('electron').webUtils.getPathForFile(file),

  // Eventos en Tiempo Real (gRPC Server Stream)
  onEvent: (callback) => {
    const handler = (_event, envelope) => callback(envelope);
    ipcRenderer.on('conduit:event', handler);
    return () => ipcRenderer.removeListener('conduit:event', handler);
  },

  // Notificaciones y Sistema
  showNotification: (options) => ipcRenderer.invoke('conduit:showNotification', options),
  flashFrame: (flag) => ipcRenderer.invoke('conduit:flashFrame', flag),
  onOpenConversation: (callback) => {
    const handler = (_event, conversationId) => callback(conversationId);
    ipcRenderer.on('conduit:openConversation', handler);
    return () => ipcRenderer.removeListener('conduit:openConversation', handler);
  },

  // Ventana y Control de Aplicación
  minimize: () => ipcRenderer.send('window:minimize'),
  maximize: () => ipcRenderer.send('window:maximize'),
  close: () => ipcRenderer.send('window:close')
});
