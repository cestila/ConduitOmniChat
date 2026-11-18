const fs = require('fs');
const path = require('path');

/**
 * Checks whether a target file and its parent directory are writable by the current user.
 * AppImage self-update replaces the executable inside its parent directory.
 */
function isPathWritable(targetPath) {
  if (!targetPath) return false;
  try {
    fs.accessSync(targetPath, fs.constants.W_OK);
    fs.accessSync(path.dirname(targetPath), fs.constants.W_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * Determines the packaging mode and whether electron-updater self-update is supported.
 *
 * @param {object} opts
 * @param {boolean} opts.isPackaged
 * @param {string} opts.platform
 * @param {Record<string, string|undefined>} [opts.env]
 * @param {(p: string) => boolean} [opts.isWritable]
 * @returns {{ packagingMode: string, selfUpdateSupported: boolean }}
 */
function detectPackagingMode({ isPackaged, platform, env = {}, isWritable } = {}) {
  if (!isPackaged) {
    return { packagingMode: 'dev', selfUpdateSupported: false };
  }

  if (platform === 'win32') {
    if (env.PORTABLE_EXECUTABLE_DIR) {
      return { packagingMode: 'win32-portable', selfUpdateSupported: false };
    }
    return { packagingMode: 'win32-nsis', selfUpdateSupported: true };
  }

  if (platform === 'darwin') {
    return { packagingMode: 'darwin', selfUpdateSupported: true };
  }

  if (platform === 'linux') {
    const appImage = env.APPIMAGE ? String(env.APPIMAGE).trim() : '';
    if (!appImage) {
      return { packagingMode: 'archive', selfUpdateSupported: false };
    }
    if (appImage === '/nix/store' || appImage.startsWith('/nix/store/')) {
      return { packagingMode: 'nix-appimage', selfUpdateSupported: false };
    }

    let writable = true;
    if (typeof isWritable === 'function') {
      writable = Boolean(isWritable(appImage));
    } else if (fs.existsSync(appImage)) {
      writable = isPathWritable(appImage);
    }

    if (!writable) {
      return { packagingMode: 'appimage-readonly', selfUpdateSupported: false };
    }
    return { packagingMode: 'appimage', selfUpdateSupported: true };
  }

  return { packagingMode: 'unknown', selfUpdateSupported: false };
}

module.exports = {
  detectPackagingMode,
  isPathWritable
};
