const path = require('path');
const { createChecker } = require('./dom-harness');
const { saveAttachmentAs } = require('../save-attachment');

const osFake = { homedir: () => '/home/tester' };

function makeDeps({ settings = { download_directory: '~/Descargas' }, dialogResult = { canceled: false, filePath: '/dest/out.pdf' }, downloadResult, copyError } = {}) {
  const log = { download: [], settings: 0, dialog: [], copy: [] };
  return {
    log,
    deps: {
      rpcClient: {
        downloadAttachment: async (id, wait) => { log.download.push([id, wait]); return downloadResult; },
        getSettings: async () => { log.settings++; return settings; },
      },
      dialog: { showSaveDialog: async (opts) => { log.dialog.push(opts); return dialogResult; } },
      fs: { copyFile: async (src, dst) => { log.copy.push([src, dst]); if (copyError) throw copyError; } },
      os: osFake,
      path,
    },
  };
}

async function run() {
  console.log('--- TEST GUARDAR COMO (main) ---');
  const { check, finish } = createChecker();

  // cached: no descarga, copia al destino elegido con defaultPath expandido
  let t = makeDeps();
  let r = await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'doc.pdf', local_path: '/m/doc.pdf', download_status: 'cached' });
  check('cached: no llama DownloadAttachment', t.log.download.length === 0);
  check('cached: defaultPath = download_directory con ~ expandido + nombre', t.log.dialog.length === 1 && t.log.dialog[0].defaultPath === '/home/tester/Descargas/doc.pdf');
  check('cached: copia local_path al destino elegido', t.log.copy.length === 1 && t.log.copy[0][0] === '/m/doc.pdf' && t.log.copy[0][1] === '/dest/out.pdf');
  check('cached: resultado con ruta', r.canceled === false && r.path === '/dest/out.pdf' && !r.error);

  // directorio absoluto sin ~ y "~" solo
  t = makeDeps({ settings: { download_directory: '/data/dl' } });
  await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'f.txt', local_path: '/m/f.txt', download_status: 'cached' });
  check('directorio absoluto se respeta', t.log.dialog[0].defaultPath === '/data/dl/f.txt');
  t = makeDeps({ settings: { download_directory: '~' } });
  await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'f.txt', local_path: '/m/f.txt', download_status: 'cached' });
  check('"~" solo se expande al home', t.log.dialog[0].defaultPath === '/home/tester/f.txt');
  t = makeDeps({ settings: {} });
  await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'f.txt', local_path: '/m/f.txt', download_status: 'cached' });
  check('sin ajuste: defaultPath es solo el nombre', t.log.dialog[0].defaultPath === 'f.txt');

  // remote_only: descarga con wait=true y luego copia la ruta devuelta
  t = makeDeps({ downloadResult: { id: 'a2', download_status: 'cached', local_path: '/m/new.pdf' } });
  r = await saveAttachmentAs(t.deps, { id: 'a2', file_name: 'new.pdf', download_status: 'remote_only' });
  check('remote_only: DownloadAttachment(id, true)', t.log.download.length === 1 && t.log.download[0][0] === 'a2' && t.log.download[0][1] === true);
  check('remote_only: copia la ruta devuelta por la descarga', t.log.copy.length === 1 && t.log.copy[0][0] === '/m/new.pdf' && t.log.copy[0][1] === '/dest/out.pdf');
  check('remote_only: resultado ok', r.canceled === false && !r.error);

  // cancelado: nada escrito
  t = makeDeps({ dialogResult: { canceled: true } });
  r = await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'doc.pdf', local_path: '/m/doc.pdf', download_status: 'cached' });
  check('cancelado: devuelve {canceled:true}', r.canceled === true && !r.error);
  check('cancelado: no copia nada', t.log.copy.length === 0);

  // descarga fallida: error y sin diálogo
  t = makeDeps({ downloadResult: { id: 'a3', download_status: 'failed' } });
  r = await saveAttachmentAs(t.deps, { id: 'a3', file_name: 'x.zip', download_status: 'failed' });
  check('descarga fallida: devuelve error', typeof r.error === 'string' && r.error.length > 0 && !r.canceled);
  check('descarga fallida: no abre diálogo ni copia', t.log.dialog.length === 0 && t.log.copy.length === 0);

  // error de RPC en la descarga
  t = makeDeps();
  t.deps.rpcClient.downloadAttachment = async () => { throw new Error('boom'); };
  r = await saveAttachmentAs(t.deps, { id: 'a3', file_name: 'x.zip', download_status: 'remote_only' });
  check('RPC falla: error con mensaje y sin diálogo', r.error && r.error.includes('boom') && t.log.dialog.length === 0);

  // fallo de copia
  t = makeDeps({ copyError: new Error('EACCES') });
  r = await saveAttachmentAs(t.deps, { id: 'a1', file_name: 'doc.pdf', local_path: '/m/doc.pdf', download_status: 'cached' });
  check('copia falla: devuelve error', r.error && r.error.includes('EACCES'));

  finish();
}

run().catch(e => { console.error(e); process.exit(1); });
