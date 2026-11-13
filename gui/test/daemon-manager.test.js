const assert = require('assert');
const net = require('net');
const path = require('path');
const fs = require('fs');

async function run() {
  console.log('--- TEST DAEMON MANAGER ---');

  const DaemonManager = require('../daemon-manager');

  // Test 1: findBinary resolution
  {
    const dm = new DaemonManager();
    const bin = dm.findBinary();
    assert(bin, 'Debe resolver la ruta al binario conduitd');
    assert(fs.existsSync(bin), `El archivo binario ${bin} debe existir`);
    console.log('PASS  findBinary resuelve binario existente');
  }

  // Test 1b: en win32 los candidatos de resources incluyen conduitd.exe y CONDUITD_PATH tiene precedencia
  {
    const os = require('os');
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'dm-win-'));
    const exe = path.join(tmp, 'bin', 'conduitd.exe');
    fs.mkdirSync(path.dirname(exe));
    fs.writeFileSync(exe, '');
    const platformDesc = Object.getOwnPropertyDescriptor(process, 'platform');
    const hadRes = Object.prototype.hasOwnProperty.call(process, 'resourcesPath');
    const prevRes = process.resourcesPath;
    const prevEnv = process.env.CONDUITD_PATH;
    try {
      Object.defineProperty(process, 'platform', { value: 'win32' });
      process.resourcesPath = tmp;
      delete process.env.CONDUITD_PATH;
      const found = new DaemonManager({}).findBinary();
      assert(found === exe || (found && fs.existsSync(found)), 'win32 debe resolver algún binario');
      // Aislar: con un resourcesPath que solo tiene conduitd.exe y sin binarios del repo
      const dm = new DaemonManager({});
      const origExists = fs.existsSync;
      fs.existsSync = (p) => p === exe || (!String(p).includes('conduitd') && origExists(p));
      const orig = fs.statSync;
      fs.statSync = (p, ...a) => (p === exe ? { isFile: () => true } : orig(p, ...a));
      try {
        assert.strictEqual(dm.findBinary(), exe, 'win32 debe encontrar resources/bin/conduitd.exe');
        process.env.CONDUITD_PATH = process.execPath;
        fs.existsSync = (p) => p === exe || p === process.execPath;
        assert.strictEqual(dm.findBinary(), process.execPath, 'CONDUITD_PATH tiene precedencia');
      } finally {
        fs.existsSync = origExists;
        fs.statSync = orig;
      }
    } finally {
      Object.defineProperty(process, 'platform', platformDesc);
      if (hadRes) process.resourcesPath = prevRes; else delete process.resourcesPath;
      if (prevEnv === undefined) delete process.env.CONDUITD_PATH; else process.env.CONDUITD_PATH = prevEnv;
      fs.rmSync(tmp, { recursive: true, force: true });
    }
    console.log('PASS  findBinary en win32 incluye conduitd.exe y respeta CONDUITD_PATH');
  }

  // Test 2: isPortOpen detection
  {
    const dm = new DaemonManager({ port: 59998 });
    const initiallyOpen = await dm.isPortOpen();
    assert.strictEqual(initiallyOpen, false, 'Puerto no utilizado debe reportar cerrado');
    console.log('PASS  isPortOpen detecta puerto cerrado');

    // Levantamos un servidor dummy en ese puerto
    const server = net.createServer().listen(59998, '127.0.0.1');
    await new Promise(r => server.on('listening', r));

    const isOpenNow = await dm.isPortOpen();
    assert.strictEqual(isOpenNow, true, 'Puerto abierto debe reportar abierto');
    console.log('PASS  isPortOpen detecta puerto abierto');

    await new Promise(r => server.close(r));
  }

  // Test 3: start() cuando el puerto ya está en uso (demonio externo)
  {
    const dummyServer = net.createServer().listen(59997, '127.0.0.1');
    await new Promise(r => dummyServer.on('listening', r));

    const dm = new DaemonManager({ port: 59997 });
    const res = await dm.start();
    assert.strictEqual(res.spawned, false, 'No debe spawnear si el puerto ya está activo');
    assert.strictEqual(res.external, true, 'Debe marcar como externo');
    assert.strictEqual(dm.isOwned, false, 'No debe ser owned');
    console.log('PASS  start respeta demonio externo si el puerto ya está escuchando');

    const stopRes = await dm.stop();
    assert.strictEqual(stopRes.stopped, false, 'No debe intentar matar un demonio que no creó');
    console.log('PASS  stop no mata demonio externo');

    await new Promise(r => dummyServer.close(r));
  }

  // Test 4: start() con un proceso hijo mockeado o controlado
  {
    // Usamos node como ejecutable para simular un proceso daemon que abre un socket y responde a SIGTERM
    const mockScript = path.join(__dirname, 'mock-daemon.js');
    fs.writeFileSync(mockScript, `
      const net = require('net');
      const server = net.createServer((c) => c.end()).listen(59996, '127.0.0.1');
      process.on('SIGTERM', () => {
        server.close(() => process.exit(0));
      });
    `);

    const dm = new DaemonManager({
      port: 59996,
      binaryPath: process.execPath,
      spawnArgs: [mockScript]
    });

    const startRes = await dm.start();
    assert.strictEqual(startRes.spawned, true, 'Debe haber spawneado el mock daemon');
    assert.strictEqual(dm.isOwned, true, 'Debe marcarse como owned');
    assert(dm.childProcess && dm.childProcess.pid, 'Debe tener PID');
    console.log('PASS  start spawnea el proceso y espera a que abra el puerto');

    // Test 5: restart()
    const restartRes = await dm.restart();
    assert.strictEqual(restartRes.spawned, true, 'Debe reiniciar el daemon');
    assert.strictEqual(dm.isOwned, true);
    console.log('PASS  restart detiene y vuelve a levantar el proceso');

    // Test 6: stop()
    const stopRes = await dm.stop();
    assert.strictEqual(stopRes.stopped, true, 'Debe haber detenido el daemon');
    assert.strictEqual(dm.childProcess, null, 'childProcess debe quedar limpio');
    const portAfterStop = await dm.isPortOpen();
    assert.strictEqual(portAfterStop, false, 'El puerto debe estar cerrado tras stop');
    console.log('PASS  stop finaliza el proceso hijo y libera el puerto');

    try { fs.unlinkSync(mockScript); } catch (_) {}
  }

  console.log('\nRESULT: ALL_CHECKS_PASSED');
}

run().catch(err => {
  console.error('FAIL:', err);
  process.exit(1);
});
