package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcore "zenhub/internal/client/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const refreshInterval = time.Second

type shell struct {
	runtime *appcore.Runtime

	routes    []appcore.RouteView
	requests  []appcore.RequestView
	conflict  appcore.SyncConflictView
	syncState appcore.SyncStateView
	busy      bool
	configUI  *configEditor

	runningLabel        *widget.Label
	listenLabel         *widget.Label
	configLabel         *widget.Label
	modelsLabel         *widget.Label
	groupsLabel         *widget.Label
	observabilityLabel  *widget.Label
	syncLabel           *widget.Label
	lastRequestLabel    *widget.Label
	lastSyncLabel       *widget.Label
	errorLabel          *widget.Label
	conflictReasonLabel *widget.Label
	localConflictLabel  *widget.Label
	cloudConflictLabel  *widget.Label
	localStateLabel     *widget.Label
	cloudStateLabel     *widget.Label
	actionLabel         *widget.Label
	configActionLabel   *widget.Label

	resolveLocalButton *widget.Button
	resolveCloudButton *widget.Button
	reloadConfigButton *widget.Button
	applyConfigButton  *widget.Button
	syncNowButton      *widget.Button

	routesEditor         *widget.Entry
	providerGroupsEditor *widget.Entry
	editorDirty          bool
	loadingEditor        bool

	routeTable   *widget.Table
	requestTable *widget.Table
}

func Run(runtime *appcore.Runtime) error {
	return runTray(runtime)
}

func newShell(runtime *appcore.Runtime) *shell {
	ui := &shell{
		runtime:             runtime,
		runningLabel:        newValueLabel(),
		listenLabel:         newValueLabel(),
		configLabel:         newValueLabel(),
		modelsLabel:         newValueLabel(),
		groupsLabel:         newValueLabel(),
		observabilityLabel:  newValueLabel(),
		syncLabel:           newValueLabel(),
		lastRequestLabel:    newValueLabel(),
		lastSyncLabel:       newValueLabel(),
		errorLabel:          newValueLabel(),
		conflictReasonLabel: newValueLabel(),
		localConflictLabel:  newValueLabel(),
		cloudConflictLabel:  newValueLabel(),
		localStateLabel:     newValueLabel(),
		cloudStateLabel:     newValueLabel(),
		actionLabel:         newValueLabel(),
		configActionLabel:   newValueLabel(),
	}

	ui.routesEditor = widget.NewMultiLineEntry()
	ui.routesEditor.Wrapping = fyne.TextWrapWord
	ui.routesEditor.SetMinRowsVisible(12)
	ui.routesEditor.OnChanged = func(string) {
		ui.markEditorDirty()
	}

	ui.providerGroupsEditor = widget.NewMultiLineEntry()
	ui.providerGroupsEditor.Wrapping = fyne.TextWrapWord
	ui.providerGroupsEditor.SetMinRowsVisible(18)
	ui.providerGroupsEditor.OnChanged = func(string) {
		ui.markEditorDirty()
	}

	ui.resolveLocalButton = widget.NewButton("Keep Local Version", func() {
		ui.resolveConflict("local")
	})
	ui.resolveCloudButton = widget.NewButton("Keep Cloud Version", func() {
		ui.resolveConflict("cloud")
	})
	ui.reloadConfigButton = widget.NewButton("Reload Editor", func() {
		ui.loadConfigEditors(true)
		ui.refresh()
	})
	ui.applyConfigButton = widget.NewButton("Apply Changes", func() {
		ui.applyConfigEdits()
	})
	ui.syncNowButton = widget.NewButton("Sync Now", func() {
		ui.runSyncNow()
	})
	ui.configUI = newConfigEditor(ui)

	ui.routeTable = widget.NewTable(
		func() (int, int) {
			return len(ui.routes) + 1, 9
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextWrapWord
			return label
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			cell.(*widget.Label).SetText(ui.routeCell(id.Row, id.Col))
		},
	)

	ui.requestTable = widget.NewTable(
		func() (int, int) {
			return len(ui.requests) + 1, 9
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextWrapWord
			return label
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			cell.(*widget.Label).SetText(ui.requestCell(id.Row, id.Col))
		},
	)

	setRouteColumnWidths(ui.routeTable)
	setRequestColumnWidths(ui.requestTable)
	ui.loadConfigEditors(true)
	return ui
}

func (s *shell) content() fyne.CanvasObject {
	overview := container.NewVBox(
		widget.NewCard("Service Status", "", widget.NewForm(
			widget.NewFormItem("Running", s.runningLabel),
			widget.NewFormItem("Listen", s.listenLabel),
			widget.NewFormItem("Config", s.configLabel),
			widget.NewFormItem("Models", s.modelsLabel),
			widget.NewFormItem("Groups / Nodes", s.groupsLabel),
			widget.NewFormItem("Observability", s.observabilityLabel),
			widget.NewFormItem("Sync", s.syncLabel),
			widget.NewFormItem("Last Sync", s.lastSyncLabel),
			widget.NewFormItem("Last Request", s.lastRequestLabel),
			widget.NewFormItem("Service Error", s.errorLabel),
		)),
		widget.NewCard("Read-Only Scope", "", widget.NewLabel(
			"Status, routes, request history, and sync conflict resolution are handled in-process. General config editing and prompt/response bodies stay out of this MVP.",
		)),
	)

	syncTab := container.NewVScroll(container.NewVBox(
		widget.NewCard("Sync Status", "", widget.NewForm(
			widget.NewFormItem("Mode", s.syncLabel),
			widget.NewFormItem("Last Sync", s.lastSyncLabel),
			widget.NewFormItem("Local", s.localStateLabel),
			widget.NewFormItem("Cloud", s.cloudStateLabel),
			widget.NewFormItem("Error", s.errorLabel),
		)),
		container.NewGridWithColumns(2, s.syncNowButton, widget.NewLabel("")),
		widget.NewCard("Snapshot & Conflict", "", container.NewVBox(
			widget.NewLabel("If both local and cloud snapshots changed, choose which version ZenHub should keep."),
			widget.NewForm(
				widget.NewFormItem("Reason", s.conflictReasonLabel),
				widget.NewFormItem("Action", s.actionLabel),
			),
			container.NewGridWithColumns(2,
				widget.NewCard("Local Snapshot", "", s.localConflictLabel),
				widget.NewCard("Cloud Snapshot", "", s.cloudConflictLabel),
			),
			container.NewGridWithColumns(2, s.resolveLocalButton, s.resolveCloudButton),
		)),
	))

	configTab := container.NewVScroll(container.NewVBox(
		s.configUI.content(),
		widget.NewCard("Snapshot Editor", "", container.NewVBox(
			widget.NewLabel("Advanced mode: edit the syncable snapshot as raw JSON arrays. The structured editor above writes back into these fields."),
			container.NewGridWithColumns(2, s.reloadConfigButton, s.applyConfigButton),
			widget.NewForm(
				widget.NewFormItem("Editor Status", s.configActionLabel),
			),
			widget.NewCard("Routes", "", s.routesEditor),
			widget.NewCard("Provider Groups", "", s.providerGroupsEditor),
		)),
	))

	tabs := container.NewAppTabs(
		container.NewTabItem("Overview", container.NewVScroll(overview)),
		container.NewTabItem("Sync", syncTab),
		container.NewTabItem("Config", configTab),
		container.NewTabItem("Routes", s.routeTable),
		container.NewTabItem("Requests", s.requestTable),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func (s *shell) refresh() {
	status := s.runtime.Status()
	s.routes = s.runtime.Routes()
	s.requests = s.runtime.Requests()
	s.conflict = s.runtime.SyncConflict()
	s.syncState = s.runtime.SyncState()

	s.runningLabel.SetText(boolText(status.Running))
	s.listenLabel.SetText(status.ListenAddress)
	s.configLabel.SetText(status.ConfigPath)
	s.modelsLabel.SetText(fmt.Sprintf("%d total: %s", status.ModelCount, strings.Join(status.Models, ", ")))
	s.groupsLabel.SetText(fmt.Sprintf("%d groups / %d nodes", status.ProviderGroupCount, status.NodeCount))
	s.observabilityLabel.SetText(fmt.Sprintf("ring buffer max %d records", status.ObservabilityLimit))
	if status.SyncEnabled {
		s.syncLabel.SetText(fallback(status.LastSyncStatus, "enabled"))
		if status.LastSyncTime.IsZero() {
			s.lastSyncLabel.SetText("No completed sync yet")
		} else {
			s.lastSyncLabel.SetText(status.LastSyncTime.Format(time.RFC3339))
		}
	} else {
		s.syncLabel.SetText("disabled")
		s.lastSyncLabel.SetText("-")
	}
	s.localStateLabel.SetText(localSyncStatusText(s.syncState))
	s.cloudStateLabel.SetText(cloudSyncStatusText(s.syncState))
	if status.SyncEnabled && !status.HasSyncConflict && !s.busy {
		s.syncNowButton.Enable()
	} else {
		s.syncNowButton.Disable()
	}
	if s.conflict.HasConflict {
		s.conflictReasonLabel.SetText(conflictReasonText(s.conflict.Reason))
		s.localConflictLabel.SetText(formatConflictSnapshot("Local", s.conflict.Local))
		s.cloudConflictLabel.SetText(formatConflictSnapshot("Cloud", s.conflict.Cloud))
		s.actionLabel.SetText("Choose one version to keep and apply immediately.")
		if !s.busy {
			s.resolveLocalButton.Enable()
			s.resolveCloudButton.Enable()
		} else {
			s.resolveLocalButton.Disable()
			s.resolveCloudButton.Disable()
		}
	} else {
		s.conflictReasonLabel.SetText("No conflict detected")
		s.localConflictLabel.SetText(formatSyncSnapshot("Local", s.syncState.LocalHash, s.syncState.LocalModifiedAt, "Modified At"))
		s.cloudConflictLabel.SetText(formatSyncSnapshot("Cloud", s.syncState.CloudHash, s.syncState.CloudUpdatedAt, "Updated At"))
		s.actionLabel.SetText("Nothing to resolve.")
		s.resolveLocalButton.Disable()
		s.resolveCloudButton.Disable()
	}
	if status.HasSyncConflict || s.busy {
		s.applyConfigButton.Disable()
	} else {
		s.applyConfigButton.Enable()
	}
	if s.busy {
		s.reloadConfigButton.Disable()
	} else {
		s.reloadConfigButton.Enable()
	}
	if s.configUI != nil {
		s.configUI.refreshAvailability(s.busy)
	}
	if status.LastRequestOK {
		s.lastRequestLabel.SetText(status.LastRequestTime.Format(time.RFC3339))
	} else {
		s.lastRequestLabel.SetText("No traffic yet")
	}
	switch {
	case status.ServiceError != "":
		s.errorLabel.SetText(status.ServiceError)
	case status.SyncError != "":
		s.errorLabel.SetText(status.SyncError)
	default:
		s.errorLabel.SetText("none")
	}

	s.routeTable.Refresh()
	s.requestTable.Refresh()
}

func (s *shell) resolveConflict(choice string) {
	s.busy = true
	s.actionLabel.SetText("Applying selection...")
	s.refresh()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err := s.runtime.ResolveSyncConflict(ctx, choice)
		fyne.Do(func() {
			s.busy = false
			if err != nil {
				s.actionLabel.SetText(err.Error())
				s.refresh()
				return
			}
			s.loadConfigEditors(true)
			s.refresh()
		})
	}()
}

func (s *shell) applyConfigEdits() {
	s.busy = true
	s.configActionLabel.SetText("Applying local config changes...")
	s.refresh()

	routesJSON := s.routesEditor.Text
	groupsJSON := s.providerGroupsEditor.Text
	go func() {
		err := s.runtime.ApplyConfigEdits(routesJSON, groupsJSON)
		fyne.Do(func() {
			s.busy = false
			if err != nil {
				s.configActionLabel.SetText(err.Error())
				s.refresh()
				return
			}
			s.loadConfigEditors(true)
			s.configActionLabel.SetText("Config applied to the running proxy.")
			s.refresh()
		})
	}()
}

func (s *shell) runSyncNow() {
	s.busy = true
	s.configActionLabel.SetText("Running sync now...")
	s.actionLabel.SetText("Sync in progress...")
	s.refresh()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err := s.runtime.SyncNow(ctx)
		fyne.Do(func() {
			s.busy = false
			if err != nil {
				s.configActionLabel.SetText(err.Error())
				s.actionLabel.SetText(err.Error())
				s.refresh()
				return
			}
			s.loadConfigEditors(true)
			s.configActionLabel.SetText("Sync completed.")
			s.refresh()
		})
	}()
}

func (s *shell) loadConfigEditors(force bool) {
	if s.editorDirty && !force {
		return
	}

	view, err := s.runtime.EditableConfig()
	if err != nil {
		s.configActionLabel.SetText(err.Error())
		return
	}

	s.loadingEditor = true
	s.routesEditor.SetText(view.RoutesJSON)
	s.providerGroupsEditor.SetText(view.ProviderGroupsJSON)
	s.loadingEditor = false
	s.editorDirty = false
	if s.configUI != nil {
		if err := s.configUI.loadFromJSON(view.RoutesJSON, view.ProviderGroupsJSON); err != nil {
			s.configActionLabel.SetText(err.Error())
			return
		}
	}
	s.configActionLabel.SetText("Editor loaded from the current config file.")
}

func (s *shell) markEditorDirty() {
	if s.loadingEditor {
		return
	}
	s.editorDirty = true
	s.configActionLabel.SetText("Unsaved JSON changes. Use Load JSON Into Forms to refresh the structured editor.")
}

func (s *shell) routeCell(row, col int) string {
	headers := []string{
		"Model",
		"Mode",
		"Provider Group",
		"Upstream Alias",
		"Strategy",
		"Timeout",
		"Retries",
		"Max Node Attempts",
		"Nodes",
	}
	if row == 0 {
		return headers[col]
	}

	route := s.routes[row-1]
	switch col {
	case 0:
		return route.Model
	case 1:
		return route.RouteMode
	case 2:
		return route.ProviderGroup
	case 3:
		if route.UpstreamModel == "" {
			return "-"
		}
		return route.UpstreamModel
	case 4:
		return route.BalancingStrategy
	case 5:
		return route.Timeout.String()
	case 6:
		return fmt.Sprintf("%d", route.RetryCount)
	case 7:
		return fmt.Sprintf("%d", route.MaxNodeAttempts)
	case 8:
		return fmt.Sprintf("%d", route.NodeCount)
	default:
		return ""
	}
}

func (s *shell) requestCell(row, col int) string {
	headers := []string{
		"Request Time",
		"Model",
		"Mode",
		"Node",
		"Strategy",
		"Retries",
		"Duration",
		"Status",
		"Error",
	}
	if row == 0 {
		return headers[col]
	}

	record := s.requests[row-1]
	switch col {
	case 0:
		return record.RequestTime.Format("2006-01-02 15:04:05")
	case 1:
		return record.Model
	case 2:
		return record.RouteMode
	case 3:
		return fallback(record.SelectedNode, "-")
	case 4:
		return fallback(record.BalancingStrategy, "-")
	case 5:
		return fmt.Sprintf("%d", record.RetryCount)
	case 6:
		return record.Duration.Round(time.Millisecond).String()
	case 7:
		return record.FinalStatus
	case 8:
		return truncate(record.Error, 96)
	default:
		return ""
	}
}

func setRouteColumnWidths(table *widget.Table) {
	table.SetColumnWidth(0, 180)
	table.SetColumnWidth(1, 95)
	table.SetColumnWidth(2, 140)
	table.SetColumnWidth(3, 170)
	table.SetColumnWidth(4, 130)
	table.SetColumnWidth(5, 110)
	table.SetColumnWidth(6, 70)
	table.SetColumnWidth(7, 135)
	table.SetColumnWidth(8, 70)
}

func setRequestColumnWidths(table *widget.Table) {
	table.SetColumnWidth(0, 165)
	table.SetColumnWidth(1, 170)
	table.SetColumnWidth(2, 95)
	table.SetColumnWidth(3, 120)
	table.SetColumnWidth(4, 130)
	table.SetColumnWidth(5, 70)
	table.SetColumnWidth(6, 90)
	table.SetColumnWidth(7, 120)
	table.SetColumnWidth(8, 320)
}

func newValueLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Wrapping = fyne.TextWrapWord
	label.Selectable = true
	return label
}

func boolText(value bool) string {
	if value {
		return "running"
	}
	return "stopped"
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func conflictReasonText(reason string) string {
	switch reason {
	case "first_sync_mismatch":
		return "First sync found different local and cloud snapshots."
	case "pull_conflict":
		return "Both local and cloud snapshots changed since the last sync."
	case "push_conflict":
		return "Cloud changed before the local snapshot could be uploaded."
	default:
		if strings.TrimSpace(reason) == "" {
			return "Conflict details are unavailable."
		}
		return reason
	}
}

func formatConflictSnapshot(title string, snapshot appcore.SyncSnapshotView) string {
	lines := []string{
		fmt.Sprintf("%s hash: %s", title, fallback(snapshot.Hash, "-")),
		fmt.Sprintf("Models: %d", snapshot.ModelCount),
		fmt.Sprintf("Groups / Nodes: %d / %d", snapshot.ProviderGroupCount, snapshot.NodeCount),
	}
	if snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "Timestamp: -")
	} else {
		lines = append(lines, "Timestamp: "+snapshot.UpdatedAt.Format(time.RFC3339))
	}
	if len(snapshot.Models) == 0 {
		lines = append(lines, "Route models: -")
	} else {
		lines = append(lines, "Route models: "+strings.Join(snapshot.Models, ", "))
	}
	return strings.Join(lines, "\n")
}

func localSyncStatusText(state appcore.SyncStateView) string {
	switch {
	case !state.Enabled:
		return "Sync disabled"
	case state.HasConflict:
		return "Local snapshot differs from cloud"
	case state.LocalHash == "":
		return "Local snapshot unavailable"
	case state.CloudHash == "":
		return "Local snapshot has not been uploaded yet"
	case state.LocalHash == state.CloudHash:
		return "Matches the last known cloud snapshot"
	default:
		return "Local snapshot changed since the last sync"
	}
}

func cloudSyncStatusText(state appcore.SyncStateView) string {
	switch {
	case !state.Enabled:
		return "Sync disabled"
	case state.HasConflict:
		return "Cloud snapshot differs from local"
	case state.CloudHash == "":
		return "No cloud snapshot recorded yet"
	case state.LocalHash != "" && state.LocalHash == state.CloudHash:
		return "Matches the current local snapshot"
	default:
		return "Last known cloud snapshot differs from local"
	}
}

func formatSyncSnapshot(title, hash string, timestamp time.Time, timeLabel string) string {
	lines := []string{
		fmt.Sprintf("%s hash: %s", title, fallback(hash, "-")),
	}
	if timestamp.IsZero() {
		lines = append(lines, timeLabel+": -")
	} else {
		lines = append(lines, timeLabel+": "+timestamp.Format(time.RFC3339))
	}
	return strings.Join(lines, "\n")
}
