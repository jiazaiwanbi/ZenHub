package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcore "zenhub/internal/app"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const refreshInterval = time.Second

type shell struct {
	runtime *appcore.Runtime

	routes   []appcore.RouteView
	requests []appcore.RequestView

	runningLabel       *widget.Label
	listenLabel        *widget.Label
	configLabel        *widget.Label
	modelsLabel        *widget.Label
	groupsLabel        *widget.Label
	observabilityLabel *widget.Label
	lastRequestLabel   *widget.Label
	errorLabel         *widget.Label

	routeTable   *widget.Table
	requestTable *widget.Table
}

func Run(runtime *appcore.Runtime) error {
	guiApp := fyneapp.NewWithID("dev.zenhub.client")
	window := guiApp.NewWindow("ZenHub")

	ui := newShell(runtime)
	ui.refresh()

	window.SetContent(ui.content())
	window.Resize(fyne.NewSize(1380, 860))

	stop := make(chan struct{})
	done := make(chan struct{})
	ticker := time.NewTicker(refreshInterval)
	go func() {
		defer close(done)
		for {
			select {
			case <-ticker.C:
				fyne.Do(ui.refresh)
			case <-stop:
				return
			}
		}
	}()

	window.ShowAndRun()

	close(stop)
	ticker.Stop()
	<-done

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return runtime.Shutdown(shutdownCtx)
}

func newShell(runtime *appcore.Runtime) *shell {
	ui := &shell{
		runtime:            runtime,
		runningLabel:       newValueLabel(),
		listenLabel:        newValueLabel(),
		configLabel:        newValueLabel(),
		modelsLabel:        newValueLabel(),
		groupsLabel:        newValueLabel(),
		observabilityLabel: newValueLabel(),
		lastRequestLabel:   newValueLabel(),
		errorLabel:         newValueLabel(),
	}

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
			widget.NewFormItem("Last Request", s.lastRequestLabel),
			widget.NewFormItem("Service Error", s.errorLabel),
		)),
		widget.NewCard("Read-Only Scope", "", widget.NewLabel(
			"Status, routes, and request history are shown from in-process runtime state. Config editing and prompt/response bodies stay out of this MVP.",
		)),
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("Overview", container.NewVScroll(overview)),
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

	s.runningLabel.SetText(boolText(status.Running))
	s.listenLabel.SetText(status.ListenAddress)
	s.configLabel.SetText(status.ConfigPath)
	s.modelsLabel.SetText(fmt.Sprintf("%d total: %s", status.ModelCount, strings.Join(status.Models, ", ")))
	s.groupsLabel.SetText(fmt.Sprintf("%d groups / %d nodes", status.ProviderGroupCount, status.NodeCount))
	s.observabilityLabel.SetText(fmt.Sprintf("ring buffer max %d records", status.ObservabilityLimit))
	if status.LastRequestOK {
		s.lastRequestLabel.SetText(status.LastRequestTime.Format(time.RFC3339))
	} else {
		s.lastRequestLabel.SetText("No traffic yet")
	}
	if status.ServiceError == "" {
		s.errorLabel.SetText("none")
	} else {
		s.errorLabel.SetText(status.ServiceError)
	}

	s.routeTable.Refresh()
	s.requestTable.Refresh()
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
