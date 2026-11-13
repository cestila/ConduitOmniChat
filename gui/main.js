const { app, BrowserWindow, ipcMain, Tray, Menu, shell, protocol, dialog, Notification } = require('electron');
const path = require('path');
const fs = require('fs');
const os = require('os');
const { execFile } = require('child_process');
const ConduitRpcClient = require('./conduit-client');
const DaemonManager = require('./daemon-manager');
const { saveAttachmentAs } = require('./save-attachment');

let mainWindow = null;
let tray = null;
let rpcClient = null;
let daemonManager = null;
let isQuitting = false;

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    frame: true,
    backgroundColor: '#1E1F29',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false
    }
  });

  mainWindow.loadFile(path.join(__dirname, 'src/index.html'));

  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (url && (url.startsWith('http://') || url.startsWith('https://') || url.startsWith('mailto:'))) {
      shell.openExternal(url);
    }
    return { action: 'deny' };
  });

  if (process.env.NODE_ENV !== 'production' || process.env.DEBUG) {
    mainWindow.webContents.openDevTools({ mode: 'detach' });
  }

  // Atajos para recarga rápida de la UI durante desarrollo
  mainWindow.webContents.on('before-input-event', (event, input) => {
    if ((input.control && input.key.toLowerCase() === 'r') || input.key === 'F5') {
      mainWindow.reload();
      event.preventDefault();
    }
    if (input.key === 'F12') {
      mainWindow.webContents.toggleDevTools();
      event.preventDefault();
    }
  });

  mainWindow.webContents.on('console-message', (_e, level, message, line, sourceId) => {
    console.log(`[Renderer ${level}] (${sourceId}:${line}) ${message}`);
  });

  mainWindow.on('focus', () => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.flashFrame(false);
    }
  });

  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

function connectGrpc() {
  if (rpcClient) {
    try {
      rpcClient.stopEventStream();
    } catch (_) {}
  }
  rpcClient = new ConduitRpcClient('127.0.0.1:50051');

  // Iniciar Stream de Eventos en tiempo real hacia la ventana
  rpcClient.startEventStream(
    (envelope) => {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('conduit:event', envelope);
      }
    },
    (err) => {
      console.warn('gRPC Stream notice:', err.message);
    }
  );
}

function setupIpcHandlers() {
  // Configurar IPC Handlers
  ipcMain.handle('conduit:getStatus', async () => {
    try {
      if (!rpcClient) return { error: 'Cliente RPC no inicializado', healthy: false };
      return await rpcClient.getStatus();
    } catch (e) {
      return { error: e.message, healthy: false };
    }
  });

  ipcMain.handle('conduit:showNotification', async (_e, { title, body, conversationId, silent } = {}) => {
    try {
      if (!Notification.isSupported()) {
        return { supported: false };
      }
      const iconPath = path.join(__dirname, 'src/assets/icon.png');
      const notifOptions = {
        title: title || 'OmniChat',
        body: body || '',
        silent: !!silent
      };
      if (fs.existsSync(iconPath)) {
        notifOptions.icon = iconPath;
      }
      const notification = new Notification(notifOptions);

      notification.on('click', () => {
        if (mainWindow && !mainWindow.isDestroyed()) {
          if (mainWindow.isMinimized()) mainWindow.restore();
          mainWindow.show();
          mainWindow.focus();
          mainWindow.flashFrame(false);
          if (conversationId) {
            mainWindow.webContents.send('conduit:openConversation', conversationId);
          }
        }
      });

      notification.show();
      return { supported: true, shown: true };
    } catch (err) {
      console.error('Error al mostrar notificación:', err);
      return { supported: false, error: err.message };
    }
  });

  ipcMain.handle('conduit:flashFrame', async (_e, flag) => {
    try {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.flashFrame(!!flag);
        return true;
      }
      return false;
    } catch (err) {
      console.error('Error en flashFrame:', err);
      return false;
    }
  });

  // Ciclo de vida y gestión del Demonio Sidecar
  ipcMain.handle('conduit:getDaemonInfo', async () => {
    if (!daemonManager) return { isOwned: false, isRunning: false, port: 50051 };
    const status = daemonManager.getStatus();
    const isPortOpen = await daemonManager.isPortOpen();
    return { ...status, isRunning: isPortOpen };
  });

  ipcMain.handle('conduit:restartDaemon', async () => {
    try {
      console.log('[Main] Petición de reinicio de conduitd recibida');
      if (rpcClient) rpcClient.stopEventStream();
      if (!daemonManager) daemonManager = new DaemonManager();
      const res = await daemonManager.restart();
      connectGrpc();
      return { success: true, ...res };
    } catch (err) {
      console.error('Error al reiniciar conduitd:', err);
      return { success: false, error: err.message };
    }
  });

  ipcMain.handle('conduit:restartApp', async () => {
    try {
      console.log('[Main] Reiniciando aplicación completa y demonio...');
      await cleanupAndQuit();
      app.relaunch();
      app.exit(0);
      return { success: true };
    } catch (err) {
      console.error('Error al reiniciar aplicación:', err);
      app.exit(1);
    }
  });

  ipcMain.handle('conduit:listConversations', async (_e, limit, offset) => {
    try {
      return await rpcClient.listConversations(limit, offset);
    } catch (e) {
      console.error('Error listConversations:', e);
      return [];
    }
  });

  ipcMain.handle('conduit:getMessages', async (_e, convId, limit, beforeId) => {
    try {
      return await rpcClient.getMessages(convId, limit, beforeId);
    } catch (e) {
      console.error('Error getMessages:', e);
      return [];
    }
  });

  ipcMain.handle('conduit:markConversationRead', async (_e, convId) => {
    try {
      return await rpcClient.markConversationRead(convId);
    } catch (e) {
      if (e.code === 14 || (e.message && e.message.includes('ECONNREFUSED'))) {
        return { success: false };
      }
      console.error('Error markConversationRead:', e);
      return { success: false };
    }
  });

  ipcMain.handle('conduit:sendMessage', async (_e, convId, body, targetChannelId, replyToId, attachments) => {
    try {
      return await rpcClient.sendMessage(convId, body, targetChannelId, replyToId, attachments);
    } catch (e) {
      console.error('Error sendMessage:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:sendReaction', async (_e, messageId, emoji) => {
    try {
      return await rpcClient.sendReaction(messageId, emoji);
    } catch (e) {
      console.error('Error sendReaction:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:sendVote', async (_e, messageId, selectedOptions) => {
    try {
      return await rpcClient.sendVote(messageId, selectedOptions);
    } catch (e) {
      console.error('Error sendVote:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:clickButton', async (_e, messageId, buttonId, buttonText) => {
    try {
      return await rpcClient.clickButton(messageId, buttonId, buttonText);
    } catch (e) {
      console.error('Error clickButton:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:editMessage', async (_e, messageId, newBody) => {
    try {
      return await rpcClient.editMessage(messageId, newBody);
    } catch (e) {
      console.error('Error editMessage:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:deleteMessage', async (_e, messageId) => {
    try {
      return await rpcClient.deleteMessage(messageId);
    } catch (e) {
      console.error('Error deleteMessage:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:pinMessage', async (_e, messageId, pinned) => {
    try {
      return await rpcClient.pinMessage(messageId, pinned);
    } catch (e) {
      console.error('Error pinMessage:', e);
      throw e;
    }
  });

  ipcMain.handle('conduit:getPinnedMessages', async (_e, convId) => {
    try {
      return await rpcClient.getPinnedMessages(convId);
    } catch (e) {
      console.error('Error getPinnedMessages:', e);
      return [];
    }
  });

  ipcMain.handle('conduit:searchMessages', async (_e, query) => {
    try {
      return await rpcClient.searchMessages(query);
    } catch (e) {
      console.error('Error searchMessages:', e);
      return [];
    }
  });

  ipcMain.handle('conduit:listAccounts', async () => {
    try {
      const accs = await rpcClient.listAccounts();
      const QRCode = require('qrcode');
      for (const a of accs) {
        if (a.qr_code && !a.qr_code.startsWith('data:image')) {
          try {
            a.qr_data_url = await QRCode.toDataURL(a.qr_code, { width: 180, margin: 1 });
          } catch (err) {
            console.error('QR code generation error:', err);
          }
        }
      }
      return accs;
    } catch (e) {
      console.error('Error listAccounts:', e);
      return [];
    }
  });

  ipcMain.handle('conduit:addAccount', async (_e, provider, label, identifier, credentials) => {
    return await rpcClient.addAccount(provider, label, identifier, credentials);
  });

  ipcMain.handle('conduit:removeAccount', async (_e, accountId) => {
    return await rpcClient.removeAccount(accountId);
  });

  ipcMain.handle('conduit:connectAccount', async (_e, accountId) => {
    const res = await rpcClient.connectAccount(accountId);
    if (res && res.account && res.account.qr_code) {
      try {
        const QRCode = require('qrcode');
        res.account.qr_data_url = await QRCode.toDataURL(res.account.qr_code, { width: 180, margin: 1 });
      } catch (err) {
        console.error('Error generating qr_data_url in connectAccount:', err);
      }
    }
    return res;
  });

  ipcMain.handle('conduit:disconnectAccount', async (_e, accountId) => {
    return await rpcClient.disconnectAccount(accountId);
  });

  ipcMain.handle('conduit:listPlugins', async () => {
    try {
      return await rpcClient.listPlugins();
    } catch (e) {
      return [];
    }
  });

  ipcMain.handle('conduit:installPlugin', async (_e, pluginId) => {
    return await rpcClient.installPlugin(pluginId);
  });

  ipcMain.handle('conduit:togglePlugin', async (_e, pluginId, enabled) => {
    return await rpcClient.togglePlugin(pluginId, enabled);
  });

  ipcMain.handle('conduit:listContacts', async () => {
    try {
      return await rpcClient.listContacts();
    } catch (e) {
      return [];
    }
  });

  ipcMain.handle('conduit:mergeContacts', async (_e, primaryId, secondaryId) => {
    return await rpcClient.mergeContacts(primaryId, secondaryId);
  });

  ipcMain.handle('conduit:splitChannel', async (_e, channelId, newName) => {
    return await rpcClient.splitChannel(channelId, newName);
  });

  ipcMain.handle('conduit:setContactIdentity', async (_e, contactId, email, phone) => {
    return await rpcClient.setContactIdentity(contactId, email, phone);
  });

  ipcMain.handle('conduit:findContactsByIdentity', async (_e, query) => {
    try {
      return await rpcClient.findContactsByIdentity(query);
    } catch (e) {
      return [];
    }
  });

  ipcMain.handle('conduit:getSettings', async () => {
    try {
      return await rpcClient.getSettings();
    } catch (e) {
      console.error('Error getSettings:', e);
      return {};
    }
  });

  ipcMain.handle('conduit:updateSettings', async (_e, settings) => {
    try {
      const updated = await rpcClient.updateSettings(settings);
      
      // Aplicar corrector ortográfico nativo en la sesión de Electron
      if (mainWindow && mainWindow.webContents && mainWindow.webContents.session) {
        const ses = mainWindow.webContents.session;
        if (settings.spellcheck_enabled) {
          const lang = settings.spellcheck_language || 'es-ES';
          ses.setSpellCheckerLanguages([lang, 'es']);
        } else {
          ses.setSpellCheckerLanguages([]);
        }
      }

      return updated;
    } catch (e) {
      console.error('Error updateSettings:', e);
      throw e;
    }
  });

}

async function cleanupAndQuit() {
  if (isQuitting) return;
  isQuitting = true;
  try {
    if (rpcClient) {
      try {
        rpcClient.stopEventStream();
      } catch (_) {}
    }
    if (daemonManager) {
      console.log('[Main] Cerrando conduitd limpiamente...');
      await daemonManager.stop();
    }
  } catch (err) {
    console.error('[Main] Error deteniendo daemon:', err);
  }
}

// Window controls
ipcMain.on('window:minimize', () => {
  if (mainWindow) mainWindow.minimize();
});
ipcMain.on('window:maximize', () => {
  if (mainWindow) {
    mainWindow.isMaximized() ? mainWindow.unmaximize() : mainWindow.maximize();
  }
});
ipcMain.on('window:close', () => {
  if (mainWindow) mainWindow.close();
});

// Multimedia IPC Handlers
ipcMain.handle('conduit:pickFiles', async () => {
  if (!mainWindow) return [];
  try {
    const result = await dialog.showOpenDialog(mainWindow, {
      title: 'Attach files',
      properties: ['openFile', 'multiSelections']
    });
    if (result.canceled || !result.filePaths) return [];

    return result.filePaths.map(filePath => {
      let size = 0;
      try {
        size = fs.statSync(filePath).size;
      } catch (err) {
        console.error('Error reading attachment size:', err);
      }
      return { path: filePath, name: path.basename(filePath), size };
    });
  } catch (err) {
    console.error('Error pickFiles:', err);
    return [];
  }
});

// Clipboard bitmaps have no filesystem path, so they are staged in a temp directory.
ipcMain.handle('conduit:saveTempAttachment', async (_e, buffer, name) => {
  try {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'conduit-paste-'));
    const safeName = path.basename(String(name || 'pasted-file'));
    const filePath = path.join(dir, safeName);
    fs.writeFileSync(filePath, Buffer.from(buffer));
    return filePath;
  } catch (err) {
    console.error('Error saveTempAttachment:', err);
    return '';
  }
});

// Procesa notas de voz grabadas en el navegador convirtiéndolas a Ogg Opus con ffmpeg
// para máxima compatibilidad con WhatsApp PTT y Telegram Voice.
ipcMain.handle('conduit:saveVoiceNote', async (_e, buffer, mimeType) => {
  try {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'conduit-voice-'));
    const timestamp = Date.now();
    const rawPath = path.join(dir, `voice-raw-${timestamp}.webm`);
    fs.writeFileSync(rawPath, Buffer.from(buffer));

    const oggPath = path.join(dir, `voice-note-${timestamp}.ogg`);
    const oggSuccess = await new Promise((resolve) => {
      execFile('ffmpeg', ['-y', '-i', rawPath, '-vn', '-c:a', 'copy', oggPath], (err) => {
        if (!err && fs.existsSync(oggPath) && fs.statSync(oggPath).size > 0) {
          try { fs.unlinkSync(rawPath); } catch (_) {}
          return resolve(true);
        }
        execFile('ffmpeg', ['-y', '-i', rawPath, '-vn', '-c:a', 'libopus', '-b:a', '64k', oggPath], (err2) => {
          if (!err2 && fs.existsSync(oggPath) && fs.statSync(oggPath).size > 0) {
            try { fs.unlinkSync(rawPath); } catch (_) {}
            return resolve(true);
          }
          resolve(false);
        });
      });
    });

    if (oggSuccess) {
      return {
        path: oggPath,
        name: `voice-note-${timestamp}.ogg`,
        mimeType: 'audio/ogg; codecs=opus',
        size: fs.statSync(oggPath).size
      };
    }

    return {
      path: rawPath,
      name: `voice-note-${timestamp}.webm`,
      mimeType: mimeType || 'audio/webm',
      size: fs.statSync(rawPath).size
    };
  } catch (err) {
    console.error('Error saveVoiceNote:', err);
    return null;
  }
});

ipcMain.handle('conduit:downloadAttachment', async (_e, attachmentId, wait) => {
  try {
    return await rpcClient.downloadAttachment(attachmentId, Boolean(wait));
  } catch (err) {
    console.error('Error downloadAttachment:', err);
    throw err;
  }
});

ipcMain.handle('conduit:saveAttachmentAs', async (_e, attachment) => {
  return saveAttachmentAs(
    { rpcClient, dialog, fs: fs.promises, os, path },
    attachment || {},
    mainWindow || undefined
  );
});

ipcMain.handle('conduit:openPath', async (_e, filePath) => {
  if (!filePath) return false;
  try {
    const res = await shell.openPath(filePath);
    return res === '';
  } catch (err) {
    console.error('Error openPath:', err);
    return false;
  }
});

ipcMain.handle('conduit:openExternal', async (_e, url) => {
  if (!url) return false;
  try {
    await shell.openExternal(url);
    return true;
  } catch (err) {
    console.error('Error openExternal:', err);
    return false;
  }
});

app.whenReady().then(async () => {
  // Protocolo seguro para reproducir y renderizar fotos/videos/audios locales de ~/.local/share/conduit/media/
  protocol.registerFileProtocol('local-media', (request, callback) => {
    const url = request.url.replace(/^local-media:\/\//, '');
    try {
      const decodedPath = decodeURIComponent(url);
      return callback(decodedPath);
    } catch (error) {
      console.error('Error local-media protocol:', error);
      return callback({ error: -6 }); // FILE_NOT_FOUND
    }
  });

  setupIpcHandlers();

  // Iniciar demonio conduitd automáticamente en segundo plano (Sidecar)
  daemonManager = new DaemonManager({
    onExit: (code, signal, { wasStopping }) => {
      if (!wasStopping && mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('conduit:daemon-exit', { code, signal });
      }
    }
  });

  try {
    const startRes = await daemonManager.start();
    if (startRes.spawned) {
      console.log(`[Main] conduitd auto-iniciado exitosamente como Sidecar (PID ${startRes.pid})`);
    } else {
      console.log(`[Main] Conectado a conduitd pre-existente en puerto ${startRes.port}`);
    }
  } catch (err) {
    console.error('[Main] Advertencia: no se pudo auto-iniciar conduitd:', err.message);
  }

  connectGrpc();
  createWindow();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('before-quit', async (event) => {
  if (daemonManager && daemonManager.isOwned && !isQuitting) {
    event.preventDefault();
    await cleanupAndQuit();
    app.quit();
  }
});

process.on('SIGINT', async () => {
  await cleanupAndQuit();
  process.exit(0);
});

process.on('SIGTERM', async () => {
  await cleanupAndQuit();
  process.exit(0);
});

app.on('window-all-closed', async () => {
  if (process.platform !== 'darwin') {
    await cleanupAndQuit();
    app.quit();
  }
});
