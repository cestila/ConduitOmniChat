// Flujo de "Guardar como…" del proceso principal, con dependencias inyectadas para poder probarlo.

// Expande un "~" inicial al directorio del usuario.
function expandHome(dir, os, path) {
  if (!dir) return '';
  if (dir === '~') return os.homedir();
  if (dir.startsWith('~/')) return path.join(os.homedir(), dir.slice(2));
  return dir;
}

// Devuelve {canceled:true}, {canceled:false, path} o {error}.
async function saveAttachmentAs({ rpcClient, dialog, fs, os, path }, attachment, parentWindow) {
  try {
    let localPath = attachment.local_path;
    if (attachment.download_status !== 'cached' || !localPath) {
      const result = await rpcClient.downloadAttachment(attachment.id, true);
      if (!result || result.download_status !== 'cached' || !result.local_path) {
        return { error: 'No se pudo descargar el archivo' };
      }
      localPath = result.local_path;
    }

    const settings = (await rpcClient.getSettings()) || {};
    const dir = expandHome(settings.download_directory, os, path);
    const fileName = attachment.file_name || path.basename(localPath);
    const options = { defaultPath: dir ? path.join(dir, fileName) : fileName };

    const choice = parentWindow
      ? await dialog.showSaveDialog(parentWindow, options)
      : await dialog.showSaveDialog(options);
    if (choice.canceled || !choice.filePath) return { canceled: true };

    await fs.copyFile(localPath, choice.filePath);
    return { canceled: false, path: choice.filePath };
  } catch (err) {
    return { error: err && err.message ? err.message : String(err) };
  }
}

module.exports = { saveAttachmentAs, expandHome };
