// Tests for Plugins view (updates, uninstall, custom install, compatibility, sidebar badge),
// status.core_version bugfix, and About view + update fallback.
// Run with: node test/plugins-about.test.js

const { createHarness, createChecker } = require('./dom-harness');

const pluginsFixture = [
  {
    id: 'conduit-plugin-whatsapp',
    name: 'WhatsApp Plugin',
    version: '1.2.0',
    latest_version: '1.3.0',
    update_available: true,
    is_new: false,
    compatible: true,
    managed_externally: false,
    description: 'Official WhatsApp connector',
    is_installed: true,
    is_enabled: true,
  },
  {
    id: 'conduit-plugin-telegram',
    name: 'Telegram Plugin',
    version: '1.0.0',
    latest_version: '1.1.0',
    update_available: true,
    is_new: false,
    compatible: true,
    managed_externally: true,
    description: 'Externally managed plugin',
    is_installed: true,
    is_enabled: true,
  },
  {
    id: 'conduit-plugin-signal',
    name: 'Signal Plugin',
    version: '2.0.0',
    latest_version: '2.0.0',
    update_available: false,
    is_new: true,
    compatible: false,
    incompatibility_reason: 'Requires SDK Protocol v3',
    description: 'Incompatible plugin',
    is_installed: false,
    is_enabled: false,
  },
];

(async () => {
  const { check, finish } = createChecker();

  // 1. Status uses status.core_version (not status.version / v1.0)
  const appStatus = createHarness({
    status: { core_version: '0.9.5', protocol_version: 2, tui_version: '' },
    plugins: pluginsFixture,
    appInfo: { version: '0.1.1', platform: 'linux', packagingMode: 'dev', selfUpdateSupported: false },
  });
  appStatus.sandbox.window.conduitAPI.getDaemonInfo = async () => ({ isOwned: false, isRunning: false, port: 50051 });
  await appStatus.start();

  check(
    'daemonStatusText uses status.core_version',
    appStatus.el.daemonStatusText.textContent.includes('v0.9.5') &&
      !appStatus.el.daemonStatusText.textContent.includes('v1.0'),
    appStatus.el.daemonStatusText.textContent
  );
  check(
    'settingDaemonInfoText uses status.core_version',
    appStatus.el.settingDaemonInfoText.textContent.includes('v0.9.5') &&
      !appStatus.el.settingDaemonInfoText.textContent.includes('v1.0'),
    appStatus.el.settingDaemonInfoText.textContent
  );

  // 2. Sidebar badge counts update_available + is_new (2 updates + 1 new = 3)
  const badgeEl = appStatus.documentStub.getElementById('plugins-update-badge');
  check(
    'sidebar badge shows count 3 for 2 updates + 1 new',
    badgeEl.textContent === '3' && !badgeEl.classList.contains('hidden'),
    `text=${badgeEl.textContent}, hidden=${badgeEl.classList.contains('hidden')}`
  );

  // Opening #view-plugins calls markPluginsSeen and clears is_new from badge (leaving 2)
  await appStatus.el.btnPlugins.click();
  await new Promise(r => setTimeout(r, 10));
  check(
    'opening plugins view calls markPluginsSeen',
    appStatus.calls.markPluginsSeen.length >= 1,
    `calls=${appStatus.calls.markPluginsSeen.length}`
  );
  check(
    'sidebar badge updates to 2 after clearing is_new',
    badgeEl.textContent === '2',
    `text=${badgeEl.textContent}`
  );

  // 3. renderPlugins: Update button, managed_externally disabled, incompatibility reason, Uninstall confirmation
  const cards = appStatus.el.pluginsList.children;
  check('renders 3 plugin cards', cards.length === 3, `count=${cards.length}`);

  const waCard = cards[0];
  const waUpdateBtn = waCard.querySelector('.btn-update-plugin');
  check('WhatsApp card renders Update button when update_available', !!waUpdateBtn && !waUpdateBtn.disabled);

  waUpdateBtn.click();
  await new Promise(r => setTimeout(r, 10));
  check(
    'clicking Update invokes conduitAPI.updatePlugin(id, false)',
    appStatus.calls.updatePlugin.some(c => c[0] === 'conduit-plugin-whatsapp' && c[1] === false),
    JSON.stringify(appStatus.calls.updatePlugin)
  );

  // Uninstall confirmation flow on WhatsApp card (re-query card after update re-render)
  const waCard2 = appStatus.el.pluginsList.children[0];
  const waUninstallBtn = waCard2.querySelectorAll('.btn-uninstall-plugin')[0];
  check('WhatsApp card renders Uninstall button', !!waUninstallBtn && !waUninstallBtn.disabled);
  waUninstallBtn.click();
  const confirmBox = waCard2.querySelectorAll('.uninstall-confirm')[0];
  const confirmBtn = waCard2.querySelectorAll('.btn-confirm-uninstall')[0];
  check('clicking Uninstall shows confirmation flow', !!confirmBox && !!confirmBtn);
  confirmBtn.click();
  await new Promise(r => setTimeout(r, 10));
  check(
    'confirming Uninstall invokes conduitAPI.uninstallPlugin(id)',
    appStatus.calls.uninstallPlugin.some(c => c[0] === 'conduit-plugin-whatsapp'),
    JSON.stringify(appStatus.calls.uninstallPlugin)
  );

  // Managed externally disables Update and Uninstall
  const tgCard = cards[1];
  const tgUpdateBtn = tgCard.querySelector('.btn-update-plugin');
  const tgUninstallBtn = tgCard.querySelector('.btn-uninstall-plugin');
  check('managed_externally disables Update button', !!tgUpdateBtn && tgUpdateBtn.disabled === true);
  check('managed_externally disables Uninstall button', !!tgUninstallBtn && tgUninstallBtn.disabled === true);

  // Incompatibility reason shown when compatible === false
  const signalCard = cards[2];
  check(
    'incompatible plugin shows incompatibility_reason',
    signalCard.innerHTML.includes('Requires SDK Protocol v3'),
    signalCard.innerHTML
  );

  // Custom owner/repo install
  const customInput = appStatus.documentStub.getElementById('plugin-custom-repo-input');
  const customBtn = appStatus.documentStub.getElementById('btn-install-custom-plugin');
  customInput.value = 'owner/conduit-plugin-matrix';
  customBtn.click();
  await new Promise(r => setTimeout(r, 10));
  check(
    'custom install invokes conduitAPI.installPlugin with owner/repo',
    appStatus.calls.installPlugin.some(c => c[0] === 'owner/conduit-plugin-matrix'),
    JSON.stringify(appStatus.calls.installPlugin)
  );

  // Update All button
  const updateAllBtn = appStatus.documentStub.getElementById('btn-update-all-plugins');
  updateAllBtn.click();
  await new Promise(r => setTimeout(r, 10));
  check(
    'Update All invokes conduitAPI.updatePlugin("", true)',
    appStatus.calls.updatePlugin.some(c => c[0] === '' && c[1] === true),
    JSON.stringify(appStatus.calls.updatePlugin)
  );

  // 4. About view population (GUI, Core, SDK Protocol, TUI "Not installed" and installed plugins table)
  await appStatus.loadAboutInfo();
  const aboutGui = appStatus.documentStub.getElementById('about-gui-version');
  const aboutCore = appStatus.documentStub.getElementById('about-core-version');
  const aboutSdk = appStatus.documentStub.getElementById('about-sdk-protocol');
  const aboutTui = appStatus.documentStub.getElementById('about-tui-version');
  const aboutPluginsTbody = appStatus.documentStub.getElementById('about-plugins-tbody');

  check('About renders GUI version', aboutGui.textContent.includes('0.1.1'), aboutGui.textContent);
  check('About renders Core version', aboutCore.textContent.includes('0.9.5'), aboutCore.textContent);
  check('About renders SDK Protocol v2', aboutSdk.textContent.includes('SDK Protocol v2'), aboutSdk.textContent);
  check('About renders Not installed when tui_version is empty', aboutTui.textContent === 'Not installed', aboutTui.textContent);
  check(
    'About installed plugins table lists installed plugins only (2)',
    aboutPluginsTbody.innerHTML.includes('WhatsApp Plugin') &&
      aboutPluginsTbody.innerHTML.includes('Telegram Plugin') &&
      !aboutPluginsTbody.innerHTML.includes('Signal Plugin'),
    aboutPluginsTbody.innerHTML
  );

  // 5. About view with TUI installed + selfUpdateSupported=false version comparison
  const appWithTui = createHarness({
    status: { core_version: '0.9.5', protocol_version: 2, tui_version: '0.4.0' },
    plugins: pluginsFixture,
    appInfo: { version: '0.1.1', platform: 'linux', packagingMode: 'archive', selfUpdateSupported: false },
    updatesResponse: {
      registry_reachable: true,
      gui_latest_version: '0.1.1',
      gui_release_url: 'https://github.com/cestila/ConduitOmniChat/releases/tag/v0.1.1',
      gui_update_available: true, // stale/false flag from conduitd comparing against tuiVersion
      core_update_available: false,
      plugins: pluginsFixture,
    },
  });
  await appWithTui.start();
  await appWithTui.loadAboutInfo();
  check(
    'About renders TUI version when present',
    appWithTui.documentStub.getElementById('about-tui-version').textContent.includes('0.4.0'),
    appWithTui.documentStub.getElementById('about-tui-version').textContent
  );

  await appWithTui.checkForUpdates();
  const updateStatusEl = appWithTui.documentStub.getElementById('about-update-status');
  check(
    'does not show false GUI update when gui_latest_version === appInfo.version',
    !updateStatusEl.innerHTML.includes('btn-open-gui-release'),
    updateStatusEl.innerHTML
  );

  finish();
})();
