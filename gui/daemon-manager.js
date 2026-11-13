const { spawn } = require('child_process');
const net = require('net');
const path = require('path');
const fs = require('fs');

class DaemonManager {
  constructor(options = {}) {
    this.port = options.port || 50051;
    this.host = options.host || '127.0.0.1';
    this.binaryPath = options.binaryPath || null;
    this.spawnArgs = options.spawnArgs || [];
    this.childProcess = null;
    this.isOwned = false;
    this.stopping = false;
    this.onLog = options.onLog || null;
    this.onExit = options.onExit || null;
  }

  findBinary() {
    if (this.binaryPath && fs.existsSync(this.binaryPath)) {
      return this.binaryPath;
    }

    const envPath = process.env.CONDUITD_PATH;
    if (envPath && fs.existsSync(envPath)) {
      return envPath;
    }

    const names = process.platform === 'win32' ? ['conduitd.exe', 'conduitd'] : ['conduitd'];
    const candidates = [];
    for (const name of names) {
      candidates.push(path.resolve(__dirname, '../bin', name));
      candidates.push(path.join(__dirname, 'bin', name));
    }

    if (process.resourcesPath) {
      for (const name of names) {
        candidates.push(path.join(process.resourcesPath, 'bin', name));
        candidates.push(path.join(process.resourcesPath, name));
      }
    }

    for (const cand of candidates) {
      if (fs.existsSync(cand)) {
        try {
          const stat = fs.statSync(cand);
          if (stat.isFile()) return cand;
        } catch (_) {}
      }
    }

    return null;
  }

  isPortOpen(timeoutMs = 300) {
    return new Promise((resolve) => {
      const socket = new net.Socket();
      let resolved = false;

      const finish = (open) => {
        if (resolved) return;
        resolved = true;
        socket.destroy();
        resolve(open);
      };

      socket.setTimeout(timeoutMs);
      socket.once('connect', () => finish(true));
      socket.once('timeout', () => finish(false));
      socket.once('error', () => finish(false));

      try {
        socket.connect(this.port, this.host);
      } catch (_) {
        finish(false);
      }
    });
  }

  async waitForPort(targetOpen, maxWaitMs = 10000, intervalMs = 150) {
    const start = Date.now();
    while (Date.now() - start < maxWaitMs) {
      const open = await this.isPortOpen();
      if (open === targetOpen) return true;
      await new Promise((r) => setTimeout(r, intervalMs));
    }
    return false;
  }

  async start() {
    // 1. Verificar si ya hay un proceso respondiendo en el puerto
    const alreadyOpen = await this.isPortOpen();
    if (alreadyOpen) {
      this.isOwned = false;
      this.childProcess = null;
      return { spawned: false, port: this.port, external: true };
    }

    // 2. Localizar binario
    const bin = this.findBinary();
    if (!bin) {
      throw new Error(`No se encontró el binario conduitd en las rutas predeterminadas.`);
    }

    const cwd = path.dirname(bin);
    this.stopping = false;

    // 3. Spawneamos el proceso
    const proc = spawn(bin, this.spawnArgs, {
      cwd,
      env: { ...process.env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    this.childProcess = proc;
    this.isOwned = true;

    if (proc.stdout) {
      proc.stdout.on('data', (data) => {
        const text = data.toString().trim();
        if (this.onLog) this.onLog('stdout', text);
        else console.log('[conduitd]', text);
      });
    }

    if (proc.stderr) {
      proc.stderr.on('data', (data) => {
        const text = data.toString().trim();
        if (this.onLog) this.onLog('stderr', text);
        else console.error('[conduitd:err]', text);
      });
    }

    proc.on('exit', (code, signal) => {
      const wasOwned = this.isOwned;
      const wasStopping = this.stopping;
      this.childProcess = null;
      this.isOwned = false;

      if (this.onExit) {
        this.onExit(code, signal, { wasOwned, wasStopping });
      }
      if (!wasStopping) {
        console.warn(`[DaemonManager] conduitd finalizó inesperadamente con código ${code}, señal ${signal}`);
      }
    });

    proc.on('error', (err) => {
      console.error('[DaemonManager] Error al ejecutar conduitd:', err);
    });

    // 4. Esperar a que el puerto se abra
    const ready = await this.waitForPort(true, 10000, 150);
    if (!ready) {
      await this.stop();
      throw new Error(`Tiempo de espera agotado: conduitd no abrió el puerto ${this.port} en 10s.`);
    }

    return { spawned: true, port: this.port, pid: proc.pid };
  }

  async stop(timeoutMs = 3000) {
    if (!this.isOwned || !this.childProcess) {
      return { stopped: false, reason: 'not_owned_or_running' };
    }

    this.stopping = true;
    const proc = this.childProcess;

    return new Promise((resolve) => {
      let resolved = false;

      const finish = () => {
        if (resolved) return;
        resolved = true;
        this.childProcess = null;
        this.isOwned = false;
        this.stopping = false;
        resolve({ stopped: true });
      };

      const killTimeout = setTimeout(() => {
        if (!resolved && proc) {
          try {
            proc.kill('SIGKILL');
          } catch (_) {}
          finish();
        }
      }, timeoutMs);

      proc.once('exit', () => {
        clearTimeout(killTimeout);
        finish();
      });

      try {
        proc.kill('SIGTERM');
      } catch (err) {
        clearTimeout(killTimeout);
        finish();
      }
    });
  }

  async restart() {
    await this.stop();
    // Esperar a que el puerto realmente cierre
    await this.waitForPort(false, 3000, 100);
    return await this.start();
  }

  getStatus() {
    return {
      isOwned: this.isOwned,
      pid: this.childProcess ? this.childProcess.pid : null,
      port: this.port,
      host: this.host,
    };
  }
}

module.exports = DaemonManager;
