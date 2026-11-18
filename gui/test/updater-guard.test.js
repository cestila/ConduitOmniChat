const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { detectPackagingMode, isPathWritable } = require('../updater-guard');
const ConduitRpcClient = require('../conduit-client');

function runPackagingModeTests() {
  // isPackaged: false -> dev
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: false, platform: 'linux', env: { APPIMAGE: '/home/user/Conduit.AppImage' } }),
    { packagingMode: 'dev', selfUpdateSupported: false }
  );
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: false, platform: 'win32', env: {} }),
    { packagingMode: 'dev', selfUpdateSupported: false }
  );

  // win32 + PORTABLE_EXECUTABLE_DIR -> win32-portable (no self-update)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'win32', env: { PORTABLE_EXECUTABLE_DIR: 'D:\\Conduit' } }),
    { packagingMode: 'win32-portable', selfUpdateSupported: false }
  );

  // win32 without PORTABLE_EXECUTABLE_DIR -> win32-nsis (self-update supported)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'win32', env: {} }),
    { packagingMode: 'win32-nsis', selfUpdateSupported: true }
  );

  // darwin -> darwin (self-update supported)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'darwin', env: {} }),
    { packagingMode: 'darwin', selfUpdateSupported: true }
  );

  // linux + Nix store APPIMAGE -> nix-appimage (no self-update)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'linux', env: { APPIMAGE: '/nix/store/abc-conduit.AppImage' } }),
    { packagingMode: 'nix-appimage', selfUpdateSupported: false }
  );

  // linux + user APPIMAGE -> appimage (self-update supported)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'linux', env: { APPIMAGE: '/home/user/Conduit.AppImage' } }),
    { packagingMode: 'appimage', selfUpdateSupported: true }
  );

  // linux without APPIMAGE (e.g. extracted tar.gz) -> archive (no self-update)
  assert.deepStrictEqual(
    detectPackagingMode({ isPackaged: true, platform: 'linux', env: {} }),
    { packagingMode: 'archive', selfUpdateSupported: false }
  );

  // linux + non-writable APPIMAGE -> appimage-readonly (no self-update)
  assert.deepStrictEqual(
    detectPackagingMode({
      isPackaged: true,
      platform: 'linux',
      env: { APPIMAGE: '/opt/conduit/Conduit.AppImage' },
      isWritable: () => false
    }),
    { packagingMode: 'appimage-readonly', selfUpdateSupported: false }
  );

  // Real filesystem check with isPathWritable on a temporary file
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'conduit-updater-guard-'));
  const writableAppImage = path.join(tmpDir, 'Conduit.AppImage');
  fs.writeFileSync(writableAppImage, 'appimage');
  try {
    assert.strictEqual(isPathWritable(writableAppImage), true);
    assert.deepStrictEqual(
      detectPackagingMode({
        isPackaged: true,
        platform: 'linux',
        env: { APPIMAGE: writableAppImage },
        isWritable: isPathWritable
      }),
      { packagingMode: 'appimage', selfUpdateSupported: true }
    );

    if (typeof process.getuid === 'function' && process.getuid() !== 0) {
      fs.chmodSync(writableAppImage, 0o444);
      assert.strictEqual(isPathWritable(writableAppImage), false);
      assert.deepStrictEqual(
        detectPackagingMode({
          isPackaged: true,
          platform: 'linux',
          env: { APPIMAGE: writableAppImage },
          isWritable: isPathWritable
        }),
        { packagingMode: 'appimage-readonly', selfUpdateSupported: false }
      );
      fs.chmodSync(writableAppImage, 0o644);
    }

    assert.strictEqual(isPathWritable(path.join(tmpDir, 'missing.AppImage')), false);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }

  console.log('PASS  detectPackagingMode and isPathWritable');
}

async function runRpcClientTests() {
  const calls = [];
  const fakeClient = Object.create(ConduitRpcClient.prototype);
  fakeClient.client = {
    InstallPlugin(req, cb) { calls.push(['InstallPlugin', req]); cb(null, { success: true, message: 'ok' }); },
    UninstallPlugin(req, cb) { calls.push(['UninstallPlugin', req]); cb(null, { success: true, message: 'removed' }); },
    UpdatePlugin(req, cb) { calls.push(['UpdatePlugin', req]); cb(null, { success: true, message: 'updated', results: [] }); },
    CheckUpdates(req, cb) { calls.push(['CheckUpdates', req]); cb(null, { registry_reachable: true, plugins: [] }); },
    MarkPluginsSeen(req, cb) { calls.push(['MarkPluginsSeen', req]); cb(null, { success: true }); }
  };

  await fakeClient.installPlugin('whatsapp');
  await fakeClient.installPlugin('cestila/conduit-plugin-slack', 'v0.2.0');
  await fakeClient.uninstallPlugin('whatsapp');
  await fakeClient.updatePlugin('whatsapp', false);
  await fakeClient.updatePlugin('', true);
  await fakeClient.checkUpdates(true);
  await fakeClient.markPluginsSeen(['telegram', 'discord']);
  await fakeClient.markPluginsSeen();

  assert.deepStrictEqual(calls, [
    ['InstallPlugin', { plugin_id: 'whatsapp', version: '' }],
    ['InstallPlugin', { plugin_id: 'cestila/conduit-plugin-slack', version: 'v0.2.0' }],
    ['UninstallPlugin', { plugin_id: 'whatsapp' }],
    ['UpdatePlugin', { plugin_id: 'whatsapp', all: false }],
    ['UpdatePlugin', { plugin_id: '', all: true }],
    ['CheckUpdates', { force_refresh: true }],
    ['MarkPluginsSeen', { plugin_ids: ['telegram', 'discord'] }],
    ['MarkPluginsSeen', { plugin_ids: [] }]
  ]);

  console.log('PASS  ConduitRpcClient plugin lifecycle methods');
}

async function run() {
  console.log('--- TEST UPDATER GUARD & PLUGIN RPC CLIENT ---');
  runPackagingModeTests();
  await runRpcClientTests();
  console.log('\nRESULT: ALL_CHECKS_PASSED');
}

run().catch((err) => {
  console.error('FAIL:', err);
  process.exit(1);
});
