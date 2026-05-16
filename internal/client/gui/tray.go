package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	appcore "zenhub/internal/client/app"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

const (
	trayRefreshInterval = time.Second
	traySyncTimeout     = 15 * time.Second
	trayShutdownTimeout = 5 * time.Second
)

type trayController struct {
	app     fyne.App
	desktop desktop.App
	runtime *appcore.Runtime
	menu    *fyne.Menu

	mu           sync.Mutex
	busy         bool
	notice       string
	shutdownOnce sync.Once
	shutdownErr  error

	runningItem       *fyne.MenuItem
	listenItem        *fyne.MenuItem
	modelsItem        *fyne.MenuItem
	lastSyncItem      *fyne.MenuItem
	lastReqItem       *fyne.MenuItem
	syncItem          *fyne.MenuItem
	localItem         *fyne.MenuItem
	cloudItem         *fyne.MenuItem
	conflictItem      *fyne.MenuItem
	codexItem         *fyne.MenuItem
	noticeItem        *fyne.MenuItem
	errorItem         *fyne.MenuItem
	switchCodexItem   *fyne.MenuItem
	syncProvidersItem *fyne.MenuItem
	syncNowItem       *fyne.MenuItem
	keepLocalItem     *fyne.MenuItem
	keepCloudItem     *fyne.MenuItem
	refreshItem       *fyne.MenuItem
	quitItem          *fyne.MenuItem
}

func runTray(runtime *appcore.Runtime) error {
	guiApp := fyneapp.NewWithID("dev.zenhub.client")
	desk, ok := guiApp.(desktop.App)
	if !ok {
		return errors.New("desktop system tray is not supported on this platform")
	}

	controller := newTrayController(guiApp, desk, runtime)
	controller.install()

	stop := make(chan struct{})
	done := make(chan struct{})
	ticker := time.NewTicker(trayRefreshInterval)
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fyne.Do(controller.refresh)
			case <-stop:
				return
			}
		}
	}()

	guiApp.Run()

	close(stop)
	<-done
	return controller.shutdown()
}

func newTrayController(app fyne.App, desk desktop.App, runtime *appcore.Runtime) *trayController {
	controller := &trayController{
		app:     app,
		desktop: desk,
		runtime: runtime,
		notice:  "Ready",
	}

	controller.runningItem = newDisabledMenuItem("")
	controller.listenItem = newDisabledMenuItem("")
	controller.modelsItem = newDisabledMenuItem("")
	controller.lastSyncItem = newDisabledMenuItem("")
	controller.lastReqItem = newDisabledMenuItem("")
	controller.syncItem = newDisabledMenuItem("")
	controller.localItem = newDisabledMenuItem("")
	controller.cloudItem = newDisabledMenuItem("")
	controller.conflictItem = newDisabledMenuItem("")
	controller.codexItem = newDisabledMenuItem("")
	controller.noticeItem = newDisabledMenuItem("")
	controller.errorItem = newDisabledMenuItem("")

	controller.switchCodexItem = fyne.NewMenuItem("Switch Codex Provider", nil)
	controller.syncProvidersItem = fyne.NewMenuItem("Sync Providers", controller.runSyncProviders)
	controller.syncNowItem = fyne.NewMenuItem("Sync Now", controller.runSyncNow)
	controller.keepLocalItem = fyne.NewMenuItem("Keep Local Snapshot", func() {
		controller.resolveConflict("local")
	})
	controller.keepCloudItem = fyne.NewMenuItem("Keep Cloud Snapshot", func() {
		controller.resolveConflict("cloud")
	})
	controller.refreshItem = fyne.NewMenuItem("Refresh Status", controller.refresh)
	controller.quitItem = fyne.NewMenuItem("Quit ZenHub", controller.quit)
	controller.quitItem.IsQuit = true

	controller.menu = fyne.NewMenu("ZenHub",
		controller.runningItem,
		controller.listenItem,
		controller.modelsItem,
		controller.lastSyncItem,
		controller.lastReqItem,
		controller.syncItem,
		controller.localItem,
		controller.cloudItem,
		controller.conflictItem,
		controller.codexItem,
		controller.noticeItem,
		controller.errorItem,
		fyne.NewMenuItemSeparator(),
		controller.switchCodexItem,
		controller.syncProvidersItem,
		controller.syncNowItem,
		controller.keepLocalItem,
		controller.keepCloudItem,
		controller.refreshItem,
		fyne.NewMenuItemSeparator(),
		controller.quitItem,
	)
	return controller
}

func (c *trayController) install() {
	c.app.SetIcon(theme.ComputerIcon())
	c.desktop.SetSystemTrayMenu(c.menu)
	c.desktop.SetSystemTrayIcon(theme.ComputerIcon())
	c.refresh()
}

func (c *trayController) refresh() {
	status := c.runtime.Status()
	syncState := c.runtime.SyncState()
	conflict := c.runtime.SyncConflict()
	codexProviders := c.runtime.CodexProviders()

	c.mu.Lock()
	busy := c.busy
	notice := c.notice
	c.mu.Unlock()

	c.runningItem.Label = "Running: " + boolText(status.Running)
	c.listenItem.Label = "Listen: " + fallback(status.ListenAddress, "-")
	c.modelsItem.Label = fmt.Sprintf("Models: %d", status.ModelCount)
	c.lastSyncItem.Label = "Last Sync: " + trayTimeText(status.LastSyncTime, "never")
	c.lastReqItem.Label = "Last Request: " + trayTimeText(status.LastRequestTime, "none")
	c.syncItem.Label = "Sync: " + traySyncStatusText(status)
	c.localItem.Label = "Local: " + localSyncStatusText(syncState)
	c.cloudItem.Label = "Cloud: " + cloudSyncStatusText(syncState)
	c.conflictItem.Label = "Conflict: " + trayConflictText(conflict)
	c.codexItem.Label = "Codex: " + codexProviderText(codexProviders)
	c.noticeItem.Label = "Notice: " + fallback(notice, "Ready")
	c.errorItem.Label = "Error: " + trayErrorText(status, syncState)

	c.switchCodexItem.ChildMenu = codexProviderMenu(codexProviders, busy, c.switchCodexProvider)
	c.switchCodexItem.Disabled = len(codexProviders) == 0
	c.syncProvidersItem.Disabled = busy || !status.SyncEnabled
	c.syncNowItem.Disabled = busy || !status.SyncEnabled || status.HasSyncConflict
	c.keepLocalItem.Disabled = busy || !conflict.HasConflict
	c.keepCloudItem.Disabled = busy || !conflict.HasConflict
	c.refreshItem.Disabled = busy

	c.menu.Refresh()
}

func (c *trayController) runSyncNow() {
	if !c.beginAction("Sync in progress...") {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traySyncTimeout)
		defer cancel()

		err := c.runtime.SyncNow(ctx)
		if err != nil {
			c.endAction("Sync failed: " + truncate(err.Error(), 96))
			return
		}
		c.endAction("Sync completed.")
	}()
}

func (c *trayController) runSyncProviders() {
	if !c.beginAction("Syncing providers...") {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traySyncTimeout)
		defer cancel()

		err := c.runtime.SyncProviders(ctx)
		if err != nil {
			c.endAction("Provider sync failed: " + truncate(err.Error(), 96))
			return
		}
		c.endAction("Provider sync completed.")
	}()
}

func (c *trayController) switchCodexProvider(providerGroupName string) {
	notice := "Switching Codex provider..."
	if strings.TrimSpace(providerGroupName) != "" {
		notice = "Switching Codex to " + strings.TrimSpace(providerGroupName) + "..."
	}
	if !c.beginAction(notice) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traySyncTimeout)
		defer cancel()

		err := c.runtime.SwitchCodexProvider(ctx, providerGroupName)
		if err != nil {
			c.endAction("Codex switch failed: " + truncate(err.Error(), 96))
			return
		}
		c.endAction("Codex provider switched.")
	}()
}

func (c *trayController) resolveConflict(choice string) {
	label := "Applying conflict selection..."
	if choice == "local" {
		label = "Keeping local snapshot..."
	} else if choice == "cloud" {
		label = "Keeping cloud snapshot..."
	}
	if !c.beginAction(label) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traySyncTimeout)
		defer cancel()

		err := c.runtime.ResolveSyncConflict(ctx, choice)
		if err != nil {
			c.endAction("Conflict resolution failed: " + truncate(err.Error(), 96))
			return
		}
		c.endAction("Conflict resolved.")
	}()
}

func (c *trayController) quit() {
	if !c.beginAction("Stopping ZenHub...") {
		return
	}

	go func() {
		err := c.shutdown()
		if err != nil {
			c.endAction("Shutdown sync failed: " + truncate(err.Error(), 96))
			fyne.Do(c.app.Quit)
			return
		}
		fyne.Do(c.app.Quit)
	}()
}

func (c *trayController) beginAction(notice string) bool {
	c.mu.Lock()
	if c.busy {
		c.notice = "Another action is already running."
		c.mu.Unlock()
		fyne.Do(c.refresh)
		return false
	}
	c.busy = true
	c.notice = notice
	c.mu.Unlock()

	fyne.Do(c.refresh)
	return true
}

func (c *trayController) endAction(notice string) {
	c.mu.Lock()
	c.busy = false
	c.notice = notice
	c.mu.Unlock()

	fyne.Do(c.refresh)
}

func (c *trayController) shutdown() error {
	c.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), trayShutdownTimeout)
		defer cancel()
		c.shutdownErr = c.runtime.Shutdown(ctx)
	})
	return c.shutdownErr
}

func newDisabledMenuItem(label string) *fyne.MenuItem {
	item := fyne.NewMenuItem(label, nil)
	item.Disabled = true
	return item
}

func trayTimeText(value time.Time, fallbackValue string) string {
	if value.IsZero() {
		return fallbackValue
	}
	return value.Format("2006-01-02 15:04:05")
}

func traySyncStatusText(status appcore.StatusView) string {
	if !status.SyncEnabled {
		return "disabled"
	}
	return fallback(status.LastSyncStatus, "enabled")
}

func trayConflictText(conflict appcore.SyncConflictView) string {
	if !conflict.HasConflict {
		return "none"
	}
	return conflictReasonText(conflict.Reason)
}

func trayErrorText(status appcore.StatusView, syncState appcore.SyncStateView) string {
	switch {
	case status.ServiceError != "":
		return status.ServiceError
	case status.SyncError != "":
		return status.SyncError
	case syncState.Error != "":
		return syncState.Error
	default:
		return "none"
	}
}

func codexProviderText(providers []appcore.CodexProviderView) string {
	for _, provider := range providers {
		if provider.Current {
			return provider.Name
		}
	}
	if len(providers) == 0 {
		return "none"
	}
	return "not selected"
}

func codexProviderMenu(
	providers []appcore.CodexProviderView,
	busy bool,
	switcher func(string),
) *fyne.Menu {
	items := make([]*fyne.MenuItem, 0, len(providers))
	for _, provider := range providers {
		name := provider.Name
		label := name
		if provider.Current {
			label = "* " + name
		}
		item := fyne.NewMenuItem(label, func() {
			switcher(name)
		})
		item.Disabled = busy
		items = append(items, item)
	}
	if len(items) == 0 {
		items = append(items, newDisabledMenuItem("No Codex providers"))
	}
	return fyne.NewMenu("", items...)
}
