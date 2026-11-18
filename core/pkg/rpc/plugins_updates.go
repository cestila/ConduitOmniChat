package rpc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cestila/ConduitOmniChat/core/pkg/buildinfo"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	conduitv1 "github.com/cestila/ConduitOmniChat/sdk/conduitv1"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

const seenPluginsSettingKey = "seen_plugins"

// ListPlugins returns installed plugins followed by uninstalled catalog/registry plugins,
// enriched with update availability, compatibility, managed-path, and seen state.
func (s *Server) ListPlugins(ctx context.Context, _ *conduitv1.ListPluginsRequest) (*conduitv1.ListPluginsResponse, error) {
	reg, _, local := s.fetchRegistryAndManifest(ctx, false)
	plugins := s.buildPluginInfos(ctx, reg, local)
	return &conduitv1.ListPluginsResponse{Plugins: plugins}, nil
}

// InstallPlugin installs an official plugin ID or third-party owner/conduit-plugin-<id>
// after verifying compatibility and dependencies, persists manifest state, and hot-loads it.
func (s *Server) InstallPlugin(ctx context.Context, req *conduitv1.InstallPluginRequest) (*conduitv1.InstallPluginResponse, error) {
	fail := func(msg string) (*conduitv1.InstallPluginResponse, error) {
		return &conduitv1.InstallPluginResponse{Success: false, Message: msg}, nil
	}
	if s.installer == nil {
		return fail("plugin installation is not available in this daemon")
	}

	id, repo, err := s.installer.Resolve(req.PluginId)
	if err != nil {
		return fail(err.Error())
	}

	reg, _, local := s.fetchRegistryAndManifest(ctx, false)
	if entry, ok := entryForCompatibility(id, reg, local); ok {
		if compat, reason := pluginmgr.CheckCompatibility(entry, buildinfo.Version, pluginsdk.ProtocolVersion, s.installedVersionsMap()); !compat {
			return fail(reason)
		}
	}

	res, err := s.installer.Install(ctx, req.PluginId, req.Version)
	if err != nil {
		return fail(err.Error())
	}
	_ = s.db.SetSetting(ctx, fmt.Sprintf("plugin_%s_enabled", res.ID), "true")
	s.recordInstalledInManifest(res.ID, repo, res.Version, strings.Contains(req.PluginId, "/"))

	msg := fmt.Sprintf("Plugin '%s' %s installed at %s", res.ID, res.Version, res.Path)
	switch {
	case s.loader == nil:
		msg += "; restart conduitd to load it"
	default:
		if err := s.loader(ctx, res.Path); err != nil {
			msg += fmt.Sprintf("; it could not be loaded (%v), restart conduitd to load it", err)
		} else {
			msg += " and loaded"
		}
	}
	return &conduitv1.InstallPluginResponse{Success: true, Message: msg}, nil
}

// UninstallPlugin stops a managed plugin, deletes its binary, and removes its manifest record.
// Externally managed plugins are rejected without modifying disk or process state.
func (s *Server) UninstallPlugin(ctx context.Context, req *conduitv1.UninstallPluginRequest) (*conduitv1.UninstallPluginResponse, error) {
	fail := func(msg string) (*conduitv1.UninstallPluginResponse, error) {
		return &conduitv1.UninstallPluginResponse{Success: false, Message: msg}, nil
	}
	id := strings.TrimSpace(req.GetPluginId())
	if id == "" {
		return fail("plugin_id is required")
	}
	if s.installer == nil {
		return fail("plugin management is not available in this daemon")
	}

	if d, ok := s.findDiscovered(id); ok && d.Path != "" && !s.installer.IsManagedPath(d.Path) {
		return fail(fmt.Sprintf("plugin %q is managed externally (%s) and cannot be uninstalled", id, d.Path))
	}

	if err := s.stopPlugin(id); err != nil {
		return fail(fmt.Sprintf("stopping plugin %q: %v", id, err))
	}
	path, err := s.installer.Remove(id)
	if err != nil {
		return fail(err.Error())
	}
	if s.manifestStore != nil {
		_ = s.manifestStore.Remove(id)
	}
	return &conduitv1.UninstallPluginResponse{
		Success: true,
		Message: fmt.Sprintf("Plugin '%s' uninstalled (%s)", id, path),
	}, nil
}

// UpdatePlugin performs a two-phase verified update (PrepareUpdate before StopPlugin)
// for a single plugin or all managed plugins with available updates, then relaunches
// and best-effort reconnects previously connected accounts.
func (s *Server) UpdatePlugin(ctx context.Context, req *conduitv1.UpdatePluginRequest) (*conduitv1.UpdatePluginResponse, error) {
	fail := func(msg string) (*conduitv1.UpdatePluginResponse, error) {
		return &conduitv1.UpdatePluginResponse{Success: false, Message: msg}, nil
	}
	if s.installer == nil {
		return fail("plugin installation is not available in this daemon")
	}
	id := strings.TrimSpace(req.GetPluginId())
	if req.GetAll() && id != "" {
		return fail("specify either plugin_id or all, not both")
	}
	if !req.GetAll() && id == "" {
		return fail("plugin_id or all=true is required")
	}

	reg, _, local := s.fetchRegistryAndManifest(ctx, false)
	targets := s.resolveUpdateTargets(ctx, id, req.GetAll(), reg, local)
	if len(targets) == 0 {
		return &conduitv1.UpdatePluginResponse{
			Success: true,
			Message: "all plugins are up to date",
		}, nil
	}

	results := make([]*conduitv1.PluginUpdateResult, 0, len(targets))
	allOK := true
	for _, targetID := range targets {
		res := s.updateSinglePlugin(ctx, targetID, reg, local)
		if !res.Success {
			allOK = false
		}
		results = append(results, res)
	}

	msg := "plugins updated"
	if !allOK {
		msg = "one or more plugin updates failed"
	} else if len(results) == 1 {
		msg = results[0].Message
	}
	return &conduitv1.UpdatePluginResponse{
		Success: allOK,
		Message: msg,
		Results: results,
	}, nil
}

// CheckUpdates queries the remote registry (or falls back gracefully when offline)
// and reports Core, GUI, and plugin update availability.
func (s *Server) CheckUpdates(ctx context.Context, req *conduitv1.CheckUpdatesRequest) (*conduitv1.CheckUpdatesResponse, error) {
	reg, reachable, local := s.fetchRegistryAndManifest(ctx, req.GetForceRefresh())

	if reachable && s.registry != nil && local != nil {
		s.refreshThirdPartyVersions(ctx, local)
	}

	plugins := s.buildPluginInfos(ctx, reg, local)
	var updatesCount, newCount int32
	for _, p := range plugins {
		if p.UpdateAvailable {
			updatesCount++
		}
		if p.IsNew {
			newCount++
		}
	}

	coreRel := reg.Core
	if coreRel.Version == "" && local != nil {
		coreRel = local.Core
	}
	guiRel := reg.GUI
	if guiRel.Version == "" && local != nil {
		guiRel = local.GUI
	}

	coreUpdate := isNewerVersion(coreRel.Version, buildinfo.Version)
	guiUpdate := isNewerVersion(guiRel.Version, s.tuiVersion)

	return &conduitv1.CheckUpdatesResponse{
		RegistryReachable:     reachable,
		CoreLatestVersion:     coreRel.Version,
		CoreReleaseUrl:        coreRel.ReleaseURL,
		CoreUpdateAvailable:   coreUpdate,
		GuiLatestVersion:      guiRel.Version,
		GuiReleaseUrl:         guiRel.ReleaseURL,
		GuiUpdateAvailable:    guiUpdate,
		Plugins:               plugins,
		UpdatesAvailableCount: updatesCount,
		NewPluginsCount:       newCount,
		CheckedAt:             time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// MarkPluginsSeen marks the given plugin IDs (or all current catalog/registry plugins
// when plugin_ids is empty) as seen so is_new becomes false.
func (s *Server) MarkPluginsSeen(ctx context.Context, req *conduitv1.MarkPluginsSeenRequest) (*conduitv1.MarkPluginsSeenResponse, error) {
	seen := s.seenPluginSet(ctx)
	if len(req.GetPluginIds()) > 0 {
		for _, id := range req.GetPluginIds() {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				seen[trimmed] = true
			}
		}
	} else {
		reg, _, local := s.fetchRegistryAndManifest(ctx, false)
		for _, p := range reg.Plugins {
			if p.ID != "" {
				seen[p.ID] = true
			}
		}
		for _, c := range pluginmgr.Catalog {
			seen[c.ID] = true
		}
		if local != nil {
			for id := range local.Plugins {
				seen[id] = true
			}
		}
	}
	if err := s.saveSeenPluginSet(ctx, seen); err != nil {
		return &conduitv1.MarkPluginsSeenResponse{Success: false}, err
	}
	return &conduitv1.MarkPluginsSeenResponse{Success: true}, nil
}

func (s *Server) fetchRegistryAndManifest(ctx context.Context, force bool) (*pluginmgr.Registry, bool, *pluginmgr.LocalManifest) {
	var reg *pluginmgr.Registry
	var reachable bool
	if s.registry != nil {
		reg, reachable, _ = s.registry.Fetch(ctx, force)
		if reachable && s.manifestStore != nil {
			_ = s.manifestStore.MergeRemote(reg)
		}
	}
	if reg == nil {
		reg = pluginmgr.CatalogRegistry()
	}
	var local *pluginmgr.LocalManifest
	if s.manifestStore != nil {
		local, _ = s.manifestStore.Load()
	}
	return reg, reachable, local
}

func (s *Server) refreshThirdPartyVersions(ctx context.Context, local *pluginmgr.LocalManifest) {
	if local == nil || s.registry == nil || s.manifestStore == nil {
		return
	}
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for id, rec := range local.Plugins {
		if rec.Source != pluginmgr.SourceThirdParty || rec.Repository == "" {
			continue
		}
		ver, relURL, err := s.registry.FetchLatestRepoVersion(ctx, rec.Repository)
		if err != nil || ver == "" {
			continue
		}
		rec.LatestVersion = ver
		if relURL != "" {
			rec.ReleaseURL = relURL
		}
		rec.LastCheckedAt = now
		local.Plugins[id] = rec
		changed = true
	}
	if changed {
		_ = s.manifestStore.Save(local)
	}
}

func (s *Server) buildPluginInfos(ctx context.Context, reg *pluginmgr.Registry, local *pluginmgr.LocalManifest) []*conduitv1.ProtoPluginInfo {
	regByID := make(map[string]pluginmgr.RegistryPlugin, len(reg.Plugins))
	for _, rp := range reg.Plugins {
		if rp.ID != "" {
			regByID[rp.ID] = rp
		}
	}

	installedVersions := s.installedVersionsMap()
	seenIDs := s.seenPluginSet(ctx)
	listed := make(map[string]bool)
	plugins := []*conduitv1.ProtoPluginInfo{}

	if s.plugins != nil {
		for _, d := range s.plugins.Discovered() {
			m := d.Manifest
			id := m.GetId()
			listed[id] = true

			info := &conduitv1.ProtoPluginInfo{
				Id:                id,
				Name:              m.GetDisplayName(),
				Description:       m.GetDescription(),
				Version:           m.GetVersion(),
				Author:            m.GetAuthor(),
				IsInstalled:       true,
				IsEnabled:         s.db.GetSetting(ctx, fmt.Sprintf("plugin_%s_enabled", id), "true") == "true",
				Manifest:          m,
				Path:              d.Path,
				Compatible:        true,
				ManagedExternally: s.installer != nil && d.Path != "" && !s.installer.IsManagedPath(d.Path),
			}
			s.enrichPluginInfo(info, id, regByID, local, installedVersions, seenIDs)
			plugins = append(plugins, info)
		}
	}

	// Uninstalled plugins from registry, local manifest, and embedded catalog.
	for _, rp := range reg.Plugins {
		if rp.ID == "" || listed[rp.ID] {
			continue
		}
		listed[rp.ID] = true
		info := &conduitv1.ProtoPluginInfo{
			Id:          rp.ID,
			Name:        rp.Name,
			Description: rp.Description,
			Repository:  rp.Repository,
			Compatible:  true,
		}
		s.enrichPluginInfo(info, rp.ID, regByID, local, installedVersions, seenIDs)
		plugins = append(plugins, info)
	}
	if local != nil {
		extraIDs := make([]string, 0, len(local.Plugins))
		for id := range local.Plugins {
			if !listed[id] {
				extraIDs = append(extraIDs, id)
			}
		}
		sort.Strings(extraIDs)
		for _, id := range extraIDs {
			rec := local.Plugins[id]
			listed[id] = true
			info := &conduitv1.ProtoPluginInfo{
				Id:         id,
				Name:       id,
				Repository: rec.Repository,
				Compatible: true,
			}
			s.enrichPluginInfo(info, id, regByID, local, installedVersions, seenIDs)
			plugins = append(plugins, info)
		}
	}
	for _, entry := range pluginmgr.Catalog {
		if listed[entry.ID] {
			continue
		}
		listed[entry.ID] = true
		info := &conduitv1.ProtoPluginInfo{
			Id:          entry.ID,
			Name:        entry.Name,
			Description: entry.Description,
			Repository:  entry.Repository,
			Compatible:  true,
		}
		s.enrichPluginInfo(info, entry.ID, regByID, local, installedVersions, seenIDs)
		plugins = append(plugins, info)
	}
	return plugins
}

func (s *Server) enrichPluginInfo(
	info *conduitv1.ProtoPluginInfo,
	id string,
	regByID map[string]pluginmgr.RegistryPlugin,
	local *pluginmgr.LocalManifest,
	installedVersions map[string]string,
	seenIDs map[string]bool,
) {
	if entry, ok := pluginmgr.CatalogEntryFor(id); ok && info.Repository == "" {
		info.Repository = entry.Repository
	}

	var compatEntry pluginmgr.RegistryPlugin
	var hasCompatEntry bool
	if rp, ok := regByID[id]; ok {
		compatEntry = rp
		hasCompatEntry = true
		if rp.Repository != "" {
			info.Repository = rp.Repository
		}
		if rp.LatestVersion != "" {
			info.LatestVersion = rp.LatestVersion
		}
		if rp.ReleaseURL != "" {
			info.ReleaseUrl = rp.ReleaseURL
		}
	}
	if local != nil {
		if rec, ok := local.Plugins[id]; ok {
			if rec.Source == pluginmgr.SourceThirdParty {
				info.IsThirdParty = true
			}
			if info.Repository == "" {
				info.Repository = rec.Repository
			}
			if info.LatestVersion == "" {
				info.LatestVersion = rec.LatestVersion
			}
			if info.ReleaseUrl == "" {
				info.ReleaseUrl = rec.ReleaseURL
			}
			if !hasCompatEntry {
				compatEntry = registryPluginFromLocal(rec)
				hasCompatEntry = true
			}
		}
	}

	if _, isOfficial := pluginmgr.CatalogEntryFor(id); !isOfficial {
		if _, inReg := regByID[id]; !inReg {
			info.IsThirdParty = true
		}
	}

	if info.IsInstalled {
		info.UpdateAvailable = isNewerVersion(info.LatestVersion, info.Version)
		info.IsNew = false
	} else {
		info.IsNew = !seenIDs[id]
	}

	if hasCompatEntry {
		compat, reason := pluginmgr.CheckCompatibility(compatEntry, buildinfo.Version, pluginsdk.ProtocolVersion, installedVersions)
		info.Compatible = compat
		info.IncompatibilityReason = reason
		for _, dep := range compatEntry.Dependencies {
			info.Dependencies = append(info.Dependencies, dep.PluginID)
		}
	}
}

func (s *Server) resolveUpdateTargets(ctx context.Context, id string, all bool, reg *pluginmgr.Registry, local *pluginmgr.LocalManifest) []string {
	if !all {
		return []string{id}
	}
	infos := s.buildPluginInfos(ctx, reg, local)
	var targets []string
	for _, p := range infos {
		if p.IsInstalled && !p.ManagedExternally && p.UpdateAvailable {
			targets = append(targets, p.Id)
		}
	}
	return targets
}

func (s *Server) updateSinglePlugin(ctx context.Context, id string, reg *pluginmgr.Registry, local *pluginmgr.LocalManifest) *conduitv1.PluginUpdateResult {
	fail := func(msg string) *conduitv1.PluginUpdateResult {
		return &conduitv1.PluginUpdateResult{PluginId: id, Success: false, Message: msg}
	}

	var fromVer string
	var provider conduitv1.Provider
	if d, ok := s.findDiscovered(id); ok {
		fromVer = d.Manifest.GetVersion()
		provider = d.Manifest.GetProvider()
		if d.Path != "" && !s.installer.IsManagedPath(d.Path) {
			return fail(fmt.Sprintf("plugin %q is managed externally (%s) and cannot be updated", id, d.Path))
		}
	}

	spec := ""
	if _, ok := pluginmgr.CatalogEntryFor(id); ok {
		spec = id
	} else if local != nil {
		if rec, ok := local.Plugins[id]; ok && rec.Repository != "" {
			spec = rec.Repository
		}
	}
	if spec == "" {
		return fail(fmt.Sprintf("unknown repository for plugin %q", id))
	}

	if entry, ok := entryForCompatibility(id, reg, local); ok {
		if compat, reason := pluginmgr.CheckCompatibility(entry, buildinfo.Version, pluginsdk.ProtocolVersion, s.installedVersionsMap()); !compat {
			return fail(reason)
		}
	}

	// Phase 1: Download & verify SHA-256 before stopping the running plugin.
	prep, err := s.installer.PrepareUpdate(ctx, spec, "")
	if err != nil {
		return fail(err.Error())
	}
	defer prep.Cleanup()

	accountsToReconnect := s.snapshotConnectedAccounts(ctx, provider, id)

	// Phase 2: Stop running plugin, commit binary atomically, relaunch, reconnect.
	if err := s.stopPlugin(id); err != nil {
		return fail(fmt.Sprintf("stopping plugin %q: %v", id, err))
	}
	res, err := prep.Commit()
	if err != nil {
		return fail(err.Error())
	}

	_, repo, _ := s.installer.Resolve(spec)
	s.recordInstalledInManifest(res.ID, repo, res.Version, strings.Contains(spec, "/"))

	msg := fmt.Sprintf("updated %s to %s", res.ID, res.Version)
	if s.loader != nil {
		if err := s.loader(ctx, res.Path); err != nil {
			msg += fmt.Sprintf("; relaunch failed (%v), restart conduitd", err)
		} else {
			s.reconnectAccounts(ctx, accountsToReconnect)
		}
	}
	return &conduitv1.PluginUpdateResult{
		PluginId:    id,
		Success:     true,
		Message:     msg,
		FromVersion: fromVer,
		ToVersion:   res.Version,
	}
}

func (s *Server) snapshotConnectedAccounts(ctx context.Context, provider conduitv1.Provider, pluginID string) []string {
	accs, err := s.db.ListAccounts(ctx)
	if err != nil {
		return nil
	}
	var ids []string
	for _, acc := range accs {
		if acc.GetStatus() != "connected" {
			continue
		}
		provName := strings.ToLower(strings.TrimPrefix(acc.GetProvider().String(), "PROVIDER_"))
		if (provider != conduitv1.Provider_PROVIDER_UNSPECIFIED && acc.GetProvider() == provider) ||
			strings.EqualFold(provName, pluginID) {
			ids = append(ids, acc.GetId())
		}
	}
	return ids
}

func (s *Server) reconnectAccounts(ctx context.Context, accountIDs []string) {
	for _, accID := range accountIDs {
		_, _ = s.ConnectAccount(ctx, &conduitv1.ConnectAccountRequest{AccountId: accID})
	}
}

func (s *Server) stopPlugin(id string) error {
	if s.stopper != nil {
		return s.stopper(id)
	}
	if s.plugins != nil {
		return s.plugins.StopPlugin(id)
	}
	return nil
}

func (s *Server) findDiscovered(id string) (pluginmgr.Discovered, bool) {
	if s.plugins == nil {
		return pluginmgr.Discovered{}, false
	}
	for _, d := range s.plugins.Discovered() {
		if d.Manifest.GetId() == id {
			return d, true
		}
	}
	return pluginmgr.Discovered{}, false
}

func (s *Server) installedVersionsMap() map[string]string {
	out := map[string]string{}
	if s.plugins == nil {
		return out
	}
	for _, d := range s.plugins.Discovered() {
		if id := d.Manifest.GetId(); id != "" {
			out[id] = d.Manifest.GetVersion()
		}
	}
	return out
}

func (s *Server) recordInstalledInManifest(id, repo, version string, isThirdParty bool) {
	if s.manifestStore == nil {
		return
	}
	_, isOfficial := pluginmgr.CatalogEntryFor(id)
	if isThirdParty && !isOfficial {
		_ = s.manifestStore.UpsertThirdParty(id, repo, version)
		return
	}
	m, err := s.manifestStore.Load()
	if err != nil {
		return
	}
	rec := m.Plugins[id]
	rec.ID = id
	if rec.Repository == "" {
		rec.Repository = repo
	}
	if rec.Source == "" {
		rec.Source = pluginmgr.SourceOfficial
	}
	rec.InstalledVersion = strings.TrimPrefix(strings.TrimSpace(version), "v")
	m.Plugins[id] = rec
	_ = s.manifestStore.Save(m)
}

func (s *Server) seenPluginSet(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	raw := s.db.GetSetting(ctx, seenPluginsSettingKey, "")
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out[trimmed] = true
		}
	}
	return out
}

func (s *Server) saveSeenPluginSet(ctx context.Context, seen map[string]bool) error {
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return s.db.SetSetting(ctx, seenPluginsSettingKey, strings.Join(ids, ","))
}

func entryForCompatibility(id string, reg *pluginmgr.Registry, local *pluginmgr.LocalManifest) (pluginmgr.RegistryPlugin, bool) {
	if reg != nil {
		for _, rp := range reg.Plugins {
			if rp.ID == id {
				return rp, true
			}
		}
	}
	if local != nil {
		if rec, ok := local.Plugins[id]; ok {
			return registryPluginFromLocal(rec), true
		}
	}
	return pluginmgr.RegistryPlugin{}, false
}

func registryPluginFromLocal(rec pluginmgr.LocalPluginRecord) pluginmgr.RegistryPlugin {
	return pluginmgr.RegistryPlugin{
		ID:                 rec.ID,
		Repository:         rec.Repository,
		LatestVersion:      rec.LatestVersion,
		ReleaseURL:         rec.ReleaseURL,
		MinCoreVersion:     rec.MinCoreVersion,
		MinProtocolVersion: rec.MinProtocolVersion,
		MaxProtocolVersion: rec.MaxProtocolVersion,
		Dependencies:       rec.Dependencies,
	}
}

func isNewerVersion(latest, current string) bool {
	latest = strings.TrimSpace(latest)
	current = strings.TrimSpace(current)
	if latest == "" || current == "" || current == "dev" {
		return false
	}
	return pluginmgr.CompareSemver(latest, current) > 0
}
