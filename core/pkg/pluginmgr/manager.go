package pluginmgr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"

	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

// Instance representa un plugin en ejecución como subproceso.
type Instance struct {
	Name     string
	Client   *plugin.Client
	RPC      conduitv1.PluginServiceClient
	Manifest *conduitv1.PluginManifest // nil for instances launched without discovery
	Path     string
}

// Discovered describes a plugin found on disk, running or disabled.
type Discovered struct {
	Manifest *conduitv1.PluginManifest
	Path     string
	Running  bool
}

var (
	// ErrDuplicatePlugin means another binary already registered the same plugin id.
	ErrDuplicatePlugin = errors.New("plugin already registered")
	// ErrPluginDisabled means the plugin was found but is disabled in settings.
	ErrPluginDisabled = errors.New("plugin disabled")
)

// manifestTimeout bounds the GetManifest call made right after launch.
const manifestTimeout = 10 * time.Second

// Manager coordina la carga, arranque y parada de los plugins del Core.
type Manager struct {
	mu        sync.RWMutex
	plugins   map[string]*Instance
	disabled  map[string]Discovered
	logger    hclog.Logger
	pluginMap map[string]plugin.Plugin
}

// NewManager crea una nueva instancia del Plugin Manager.
func NewManager() *Manager {
	logger := hclog.New(&hclog.LoggerOptions{
		Name:  "pluginmgr",
		Level: hclog.Info,
	})

	return &Manager{
		plugins:  make(map[string]*Instance),
		disabled: make(map[string]Discovered),
		logger:   logger,
		pluginMap: map[string]plugin.Plugin{
			pluginsdk.PluginKey: &pluginsdk.ChannelGRPCPlugin{},
		},
	}
}

// start lanza el binario como proceso hijo y devuelve el cliente gRPC del plugin.
func (m *Manager) start(binaryPath string) (*plugin.Client, conduitv1.PluginServiceClient, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  pluginsdk.HandshakeConfig,
		Plugins:          m.pluginMap,
		Cmd:              exec.Command(binaryPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           m.logger,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("error al conectar con el plugin %s: %w", binaryPath, err)
	}

	raw, err := rpcClient.Dispense(pluginsdk.PluginKey)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("error al obtener interfaz channel del plugin %s: %w", binaryPath, err)
	}

	pluginService, ok := raw.(conduitv1.PluginServiceClient)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("interfaz incompatible devuelta por el plugin %s", binaryPath)
	}
	return client, pluginService, nil
}

// Launch inicia un binario de plugin como proceso hijo y lo registra con name.
func (m *Manager) Launch(name, binaryPath string) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plugins[name]; exists {
		return nil, fmt.Errorf("el plugin '%s' ya está en ejecución", name)
	}

	client, rpc, err := m.start(binaryPath)
	if err != nil {
		return nil, err
	}
	instance := &Instance{Name: name, Client: client, RPC: rpc, Path: binaryPath}
	m.plugins[name] = instance
	return instance, nil
}

// LaunchDiscovered lanza un binario encontrado en disco, le pide su manifiesto
// y lo registra con el id que declara. Si enabled(id) es falso el proceso se
// detiene y el plugin queda registrado como deshabilitado (ErrPluginDisabled).
func (m *Manager) LaunchDiscovered(ctx context.Context, binaryPath string, enabled func(id string) bool) (*Instance, error) {
	client, rpc, err := m.start(binaryPath)
	if err != nil {
		return nil, err
	}

	manifestCtx, cancel := context.WithTimeout(ctx, manifestTimeout)
	manifest, err := rpc.GetManifest(manifestCtx, &conduitv1.GetManifestRequest{})
	cancel()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("el plugin %s no devolvió su manifiesto: %w", binaryPath, err)
	}
	if err := pluginsdk.ValidateManifest(manifest); err != nil {
		client.Kill()
		return nil, fmt.Errorf("manifiesto inválido en %s: %w", binaryPath, err)
	}

	id := manifest.GetId()
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, exists := m.plugins[id]; exists {
		client.Kill()
		return nil, fmt.Errorf("%w: %s (%s ya cargado desde %s)", ErrDuplicatePlugin, id, binaryPath, existing.Path)
	}
	if existing, exists := m.disabled[id]; exists {
		client.Kill()
		return nil, fmt.Errorf("%w: %s (%s ya encontrado en %s)", ErrDuplicatePlugin, id, binaryPath, existing.Path)
	}
	if enabled != nil && !enabled(id) {
		client.Kill()
		m.disabled[id] = Discovered{Manifest: manifest, Path: binaryPath}
		return nil, fmt.Errorf("%w: %s", ErrPluginDisabled, id)
	}

	instance := &Instance{Name: id, Client: client, RPC: rpc, Manifest: manifest, Path: binaryPath}
	m.plugins[id] = instance
	return instance, nil
}

// Discovered lista los plugins con manifiesto, en ejecución o deshabilitados, ordenados por id.
func (m *Manager) Discovered() []Discovered {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Discovered
	for _, inst := range m.plugins {
		if inst.Manifest != nil {
			out = append(out, Discovered{Manifest: inst.Manifest, Path: inst.Path, Running: true})
		}
	}
	for _, d := range m.disabled {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.GetId() < out[j].Manifest.GetId() })
	return out
}

// RegisterInstance registra directamente una instancia de plugin (útil para pruebas o plugins embebidos).
func (m *Manager) RegisterInstance(name string, instance *Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plugins[name] = instance
}

// GetInstance devuelve la instancia de un plugin si está activo.
func (m *Manager) GetInstance(name string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.plugins[name]
	return inst, ok
}

// Stop detiene un plugin en ejecución de forma segura.
func (m *Manager) Stop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, exists := m.plugins[name]; exists {
		if inst.Client != nil {
			inst.Client.Kill()
		}
		delete(m.plugins, name)
	}
}

// StopAll finaliza todos los plugins activos.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, inst := range m.plugins {
		if inst.Client != nil {
			inst.Client.Kill()
		}
		delete(m.plugins, name)
	}
}
