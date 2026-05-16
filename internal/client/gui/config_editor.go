package gui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	clientconfig "zenhub/internal/client/config"
	"zenhub/internal/core/runtimeconfig"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var (
	routeModeOptions = []string{"direct", "relay"}
	strategyOptions  = []string{"round_robin", "fill_first"}
)

type configEditor struct {
	owner *shell

	loading    bool
	routes     []clientconfig.Route
	groups     []clientconfig.ProviderGroup
	routeIndex int
	groupIndex int
	nodeIndex  int

	loadStructuredButton *widget.Button

	routeSelect            *widget.Select
	routeModeSelect        *widget.Select
	routeProviderGroupPick *widget.Select
	routeModelEntry        *widget.Entry
	routeUpstreamEntry     *widget.Entry
	addRouteButton         *widget.Button
	saveRouteButton        *widget.Button
	removeRouteButton      *widget.Button

	groupSelect                *widget.Select
	groupStrategySelect        *widget.Select
	groupNameEntry             *widget.Entry
	groupTimeoutEntry          *widget.Entry
	groupRetryCountEntry       *widget.Entry
	groupMaxNodeAttemptsEntry  *widget.Entry
	groupFailureThresholdEntry *widget.Entry
	groupCooldownEntry         *widget.Entry
	addGroupButton             *widget.Button
	saveGroupButton            *widget.Button
	removeGroupButton          *widget.Button

	nodeSelect       *widget.Select
	nodeNameEntry    *widget.Entry
	nodeBaseURLEntry *widget.Entry
	nodeAPIKeyEntry  *widget.Entry
	nodeAPIKeyEnv    *widget.Entry
	nodeHeadersEntry *widget.Entry
	addNodeButton    *widget.Button
	saveNodeButton   *widget.Button
	removeNodeButton *widget.Button
}

func newConfigEditor(owner *shell) *configEditor {
	editor := &configEditor{
		owner:      owner,
		routeIndex: -1,
		groupIndex: -1,
		nodeIndex:  -1,
	}

	editor.routeSelect = widget.NewSelect(nil, func(value string) {
		if editor.loading {
			return
		}
		editor.routeIndex = indexOfOption(editor.routeSelect.Options, value)
		editor.populateRouteForm()
		editor.refreshAvailability(owner.busy)
	})
	editor.routeModeSelect = widget.NewSelect(routeModeOptions, nil)
	editor.routeProviderGroupPick = widget.NewSelect(nil, nil)
	editor.routeModelEntry = widget.NewEntry()
	editor.routeUpstreamEntry = widget.NewEntry()

	editor.groupSelect = widget.NewSelect(nil, func(value string) {
		if editor.loading {
			return
		}
		editor.groupIndex = indexOfOption(editor.groupSelect.Options, value)
		editor.clampNodeIndex()
		editor.refreshSelectors()
	})
	editor.groupStrategySelect = widget.NewSelect(strategyOptions, nil)
	editor.groupNameEntry = widget.NewEntry()
	editor.groupTimeoutEntry = widget.NewEntry()
	editor.groupRetryCountEntry = widget.NewEntry()
	editor.groupMaxNodeAttemptsEntry = widget.NewEntry()
	editor.groupFailureThresholdEntry = widget.NewEntry()
	editor.groupCooldownEntry = widget.NewEntry()

	editor.nodeSelect = widget.NewSelect(nil, func(value string) {
		if editor.loading {
			return
		}
		editor.nodeIndex = indexOfOption(editor.nodeSelect.Options, value)
		editor.populateNodeForm()
		editor.refreshAvailability(owner.busy)
	})
	editor.nodeNameEntry = widget.NewEntry()
	editor.nodeBaseURLEntry = widget.NewEntry()
	editor.nodeAPIKeyEntry = widget.NewEntry()
	editor.nodeAPIKeyEnv = widget.NewEntry()
	editor.nodeHeadersEntry = widget.NewMultiLineEntry()
	editor.nodeHeadersEntry.Wrapping = fyne.TextWrapWord
	editor.nodeHeadersEntry.SetMinRowsVisible(4)

	editor.loadStructuredButton = widget.NewButton("Load JSON Into Forms", func() {
		if err := editor.loadFromJSON(owner.routesEditor.Text, owner.providerGroupsEditor.Text); err != nil {
			owner.configActionLabel.SetText(err.Error())
			return
		}
		owner.configActionLabel.SetText("Structured editor rebuilt from the current JSON text.")
	})

	editor.addRouteButton = widget.NewButton("Add Route", editor.addRoute)
	editor.saveRouteButton = widget.NewButton("Save Route", editor.saveRoute)
	editor.removeRouteButton = widget.NewButton("Remove Route", editor.removeRoute)

	editor.addGroupButton = widget.NewButton("Add Group", editor.addGroup)
	editor.saveGroupButton = widget.NewButton("Save Group", editor.saveGroup)
	editor.removeGroupButton = widget.NewButton("Remove Group", editor.removeGroup)

	editor.addNodeButton = widget.NewButton("Add Node", editor.addNode)
	editor.saveNodeButton = widget.NewButton("Save Node", editor.saveNode)
	editor.removeNodeButton = widget.NewButton("Remove Node", editor.removeNode)

	return editor
}

func (e *configEditor) content() fyne.CanvasObject {
	return widget.NewCard("Structured Editor", "", container.NewVBox(
		widget.NewLabel("Edit routes, provider groups, and nodes without touching raw JSON. Save the selected item to push the draft back into the advanced JSON editor below."),
		container.NewGridWithColumns(2, e.loadStructuredButton, widget.NewLabel("")),
		container.NewGridWithColumns(2,
			widget.NewCard("Routes", "", container.NewVBox(
				container.NewGridWithColumns(3, e.addRouteButton, e.saveRouteButton, e.removeRouteButton),
				widget.NewForm(
					widget.NewFormItem("Current Route", e.routeSelect),
					widget.NewFormItem("Model", e.routeModelEntry),
					widget.NewFormItem("Mode", e.routeModeSelect),
					widget.NewFormItem("Provider Group", e.routeProviderGroupPick),
					widget.NewFormItem("Upstream Model", e.routeUpstreamEntry),
				),
			)),
			widget.NewCard("Provider Groups", "", container.NewVBox(
				container.NewGridWithColumns(3, e.addGroupButton, e.saveGroupButton, e.removeGroupButton),
				widget.NewForm(
					widget.NewFormItem("Current Group", e.groupSelect),
					widget.NewFormItem("Name", e.groupNameEntry),
					widget.NewFormItem("Strategy", e.groupStrategySelect),
					widget.NewFormItem("Timeout", e.groupTimeoutEntry),
					widget.NewFormItem("Retry Count", e.groupRetryCountEntry),
					widget.NewFormItem("Max Node Attempts", e.groupMaxNodeAttemptsEntry),
					widget.NewFormItem("Failure Threshold", e.groupFailureThresholdEntry),
					widget.NewFormItem("Cooldown", e.groupCooldownEntry),
				),
			)),
		),
		widget.NewCard("Nodes", "", container.NewVBox(
			widget.NewLabel("Nodes belong to the currently selected provider group."),
			container.NewGridWithColumns(3, e.addNodeButton, e.saveNodeButton, e.removeNodeButton),
			widget.NewForm(
				widget.NewFormItem("Current Node", e.nodeSelect),
				widget.NewFormItem("Name", e.nodeNameEntry),
				widget.NewFormItem("Base URL", e.nodeBaseURLEntry),
				widget.NewFormItem("API Key", e.nodeAPIKeyEntry),
				widget.NewFormItem("API Key Env", e.nodeAPIKeyEnv),
				widget.NewFormItem("Headers JSON", e.nodeHeadersEntry),
			),
		)),
	))
}

func (e *configEditor) loadFromJSON(routesJSON, groupsJSON string) error {
	routes, err := decodeRoutesJSON(routesJSON)
	if err != nil {
		return err
	}
	groups, err := decodeProviderGroupsJSON(groupsJSON)
	if err != nil {
		return err
	}
	if err := validateDraft(routes, groups); err != nil {
		return fmt.Errorf("validate structured draft: %w", err)
	}

	e.routes = cloneRoutes(routes)
	e.groups = cloneProviderGroups(groups)
	e.clampRouteIndex()
	e.clampGroupIndex()
	e.clampNodeIndex()
	e.refreshSelectors()
	return nil
}

func (e *configEditor) syncJSONEditors(message string) error {
	routesJSON, err := prettyJSON(e.routes)
	if err != nil {
		return fmt.Errorf("encode routes JSON: %w", err)
	}
	groupsJSON, err := prettyJSON(e.groups)
	if err != nil {
		return fmt.Errorf("encode provider groups JSON: %w", err)
	}

	e.owner.loadingEditor = true
	e.owner.routesEditor.SetText(routesJSON)
	e.owner.providerGroupsEditor.SetText(groupsJSON)
	e.owner.loadingEditor = false
	e.owner.editorDirty = true
	e.owner.configActionLabel.SetText(message)

	e.refreshSelectors()
	return nil
}

func (e *configEditor) refreshAvailability(busy bool) {
	setButtonState(e.loadStructuredButton, !busy)
	setButtonState(e.addRouteButton, !busy)
	setButtonState(e.addGroupButton, !busy)
	setButtonState(e.addNodeButton, !busy && e.groupIndex >= 0)
	setButtonState(e.saveRouteButton, !busy && e.routeIndex >= 0)
	setButtonState(e.removeRouteButton, !busy && e.routeIndex >= 0)
	setButtonState(e.saveGroupButton, !busy && e.groupIndex >= 0)
	setButtonState(e.removeGroupButton, !busy && e.groupIndex >= 0)
	setButtonState(e.saveNodeButton, !busy && e.groupIndex >= 0 && e.nodeIndex >= 0)
	setButtonState(e.removeNodeButton, !busy && e.groupIndex >= 0 && e.nodeIndex >= 0)
}

func (e *configEditor) addRoute() {
	next := clientconfig.Route{
		Model:         fmt.Sprintf("new-model-%d", len(e.routes)+1),
		Mode:          "direct",
		ProviderGroup: e.firstGroupName(),
		UpstreamModel: "",
	}
	routes := append(cloneRoutes(e.routes), next)
	if err := validateDraft(routes, e.groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.routes = routes
	e.routeIndex = len(e.routes) - 1
	if err := e.syncJSONEditors("Added a route draft. Review the placeholder values before applying."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) saveRoute() {
	if e.routeIndex < 0 || e.routeIndex >= len(e.routes) {
		e.owner.configActionLabel.SetText("Select a route before saving.")
		return
	}

	mode := strings.TrimSpace(e.routeModeSelect.Selected)
	if mode == "" {
		mode = "direct"
	}
	route := clientconfig.Route{
		Model:         strings.TrimSpace(e.routeModelEntry.Text),
		Mode:          mode,
		ProviderGroup: strings.TrimSpace(e.routeProviderGroupPick.Selected),
		UpstreamModel: strings.TrimSpace(e.routeUpstreamEntry.Text),
	}

	routes := cloneRoutes(e.routes)
	routes[e.routeIndex] = route
	if err := validateDraft(routes, e.groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.routes = routes
	if err := e.syncJSONEditors("Saved the selected route into the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) removeRoute() {
	if len(e.routes) <= 1 {
		e.owner.configActionLabel.SetText("At least one route is required.")
		return
	}
	if e.routeIndex < 0 || e.routeIndex >= len(e.routes) {
		e.owner.configActionLabel.SetText("Select a route before removing it.")
		return
	}

	routes := cloneRoutes(e.routes)
	routes = append(routes[:e.routeIndex], routes[e.routeIndex+1:]...)
	e.routes = routes
	e.clampRouteIndex()
	if err := e.syncJSONEditors("Removed the selected route from the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) addGroup() {
	nextGroup := defaultProviderGroup(len(e.groups) + 1)
	groups := append(cloneProviderGroups(e.groups), nextGroup)
	if err := validateDraft(e.routes, groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.groups = groups
	e.groupIndex = len(e.groups) - 1
	e.nodeIndex = 0
	if err := e.syncJSONEditors("Added a provider group draft with placeholder node values. Review it before applying."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) saveGroup() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.owner.configActionLabel.SetText("Select a provider group before saving.")
		return
	}

	timeout, err := parseDurationField("group timeout", e.groupTimeoutEntry.Text)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}
	retryCount, err := parseIntegerField("retry count", e.groupRetryCountEntry.Text, true)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}
	maxNodeAttempts, err := parseIntegerField("max node attempts", e.groupMaxNodeAttemptsEntry.Text, false)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}
	failureThreshold, err := parseIntegerField("failure threshold", e.groupFailureThresholdEntry.Text, false)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}
	cooldown, err := parseDurationField("cooldown", e.groupCooldownEntry.Text)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	current := e.groups[e.groupIndex]
	name := strings.TrimSpace(e.groupNameEntry.Text)
	strategy := strings.TrimSpace(e.groupStrategySelect.Selected)
	if strategy == "" {
		strategy = "round_robin"
	}

	group := current
	group.Name = name
	group.Strategy = strategy
	group.Timeout = clientconfig.Duration{Duration: timeout}
	group.RetryCount = retryCount
	group.MaxNodeAttempts = maxNodeAttempts
	group.PassiveHealth = clientconfig.PassiveHealthConfig{
		FailureThreshold: failureThreshold,
		Cooldown:         clientconfig.Duration{Duration: cooldown},
	}

	routes := cloneRoutes(e.routes)
	if strings.TrimSpace(current.Name) != strings.TrimSpace(group.Name) {
		for idx := range routes {
			if routes[idx].ProviderGroup == current.Name {
				routes[idx].ProviderGroup = group.Name
			}
		}
	}

	groups := cloneProviderGroups(e.groups)
	groups[e.groupIndex] = group
	if err := validateDraft(routes, groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.routes = routes
	e.groups = groups
	if err := e.syncJSONEditors("Saved the selected provider group into the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) removeGroup() {
	if len(e.groups) <= 1 {
		e.owner.configActionLabel.SetText("At least one provider group is required.")
		return
	}
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.owner.configActionLabel.SetText("Select a provider group before removing it.")
		return
	}

	name := e.groups[e.groupIndex].Name
	for _, route := range e.routes {
		if route.ProviderGroup == name {
			e.owner.configActionLabel.SetText("Move routes off this provider group before removing it.")
			return
		}
	}

	groups := cloneProviderGroups(e.groups)
	groups = append(groups[:e.groupIndex], groups[e.groupIndex+1:]...)
	e.groups = groups
	e.clampGroupIndex()
	e.clampNodeIndex()
	if err := e.syncJSONEditors("Removed the selected provider group from the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) addNode() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.owner.configActionLabel.SetText("Select a provider group before adding a node.")
		return
	}

	groups := cloneProviderGroups(e.groups)
	group := groups[e.groupIndex]
	group.Nodes = append(group.Nodes, defaultNode(len(group.Nodes)+1))
	groups[e.groupIndex] = group
	if err := validateDraft(e.routes, groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.groups = groups
	e.nodeIndex = len(e.groups[e.groupIndex].Nodes) - 1
	if err := e.syncJSONEditors("Added a node draft with placeholder values. Review it before applying."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) saveNode() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.owner.configActionLabel.SetText("Select a provider group before saving a node.")
		return
	}
	if e.nodeIndex < 0 || e.nodeIndex >= len(e.groups[e.groupIndex].Nodes) {
		e.owner.configActionLabel.SetText("Select a node before saving it.")
		return
	}

	headers, err := parseHeadersJSON(e.nodeHeadersEntry.Text)
	if err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	groups := cloneProviderGroups(e.groups)
	group := groups[e.groupIndex]
	group.Nodes[e.nodeIndex] = clientconfig.Node{
		Name:      strings.TrimSpace(e.nodeNameEntry.Text),
		BaseURL:   strings.TrimSpace(e.nodeBaseURLEntry.Text),
		APIKey:    strings.TrimSpace(e.nodeAPIKeyEntry.Text),
		APIKeyEnv: strings.TrimSpace(e.nodeAPIKeyEnv.Text),
		Headers:   headers,
	}
	groups[e.groupIndex] = group
	if err := validateDraft(e.routes, groups); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
		return
	}

	e.groups = groups
	if err := e.syncJSONEditors("Saved the selected node into the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) removeNode() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.owner.configActionLabel.SetText("Select a provider group before removing a node.")
		return
	}
	group := e.groups[e.groupIndex]
	if len(group.Nodes) <= 1 {
		e.owner.configActionLabel.SetText("Each provider group needs at least one node.")
		return
	}
	if e.nodeIndex < 0 || e.nodeIndex >= len(group.Nodes) {
		e.owner.configActionLabel.SetText("Select a node before removing it.")
		return
	}

	groups := cloneProviderGroups(e.groups)
	group = groups[e.groupIndex]
	group.Nodes = append(group.Nodes[:e.nodeIndex], group.Nodes[e.nodeIndex+1:]...)
	groups[e.groupIndex] = group
	e.groups = groups
	e.clampNodeIndex()
	if err := e.syncJSONEditors("Removed the selected node from the JSON editor."); err != nil {
		e.owner.configActionLabel.SetText(err.Error())
	}
}

func (e *configEditor) refreshSelectors() {
	e.loading = true
	defer func() {
		e.loading = false
		e.refreshAvailability(e.owner.busy)
	}()

	routeOptions := make([]string, 0, len(e.routes))
	for idx, route := range e.routes {
		routeOptions = append(routeOptions, fmt.Sprintf("%d. %s (%s)", idx+1, fallback(route.Model, "unnamed"), fallback(route.Mode, "direct")))
	}
	e.routeSelect.SetOptions(routeOptions)
	if e.routeIndex >= 0 && e.routeIndex < len(routeOptions) {
		e.routeSelect.SetSelected(routeOptions[e.routeIndex])
	} else {
		e.routeSelect.ClearSelected()
	}

	groupOptions := make([]string, 0, len(e.groups))
	groupNames := make([]string, 0, len(e.groups))
	for idx, group := range e.groups {
		groupOptions = append(groupOptions, fmt.Sprintf("%d. %s", idx+1, fallback(group.Name, "unnamed")))
		groupNames = append(groupNames, group.Name)
	}
	e.groupSelect.SetOptions(groupOptions)
	if e.groupIndex >= 0 && e.groupIndex < len(groupOptions) {
		e.groupSelect.SetSelected(groupOptions[e.groupIndex])
	} else {
		e.groupSelect.ClearSelected()
	}

	e.routeProviderGroupPick.SetOptions(groupNames)

	nodeOptions := make([]string, 0)
	if e.groupIndex >= 0 && e.groupIndex < len(e.groups) {
		nodeOptions = make([]string, 0, len(e.groups[e.groupIndex].Nodes))
		for idx, node := range e.groups[e.groupIndex].Nodes {
			nodeOptions = append(nodeOptions, fmt.Sprintf("%d. %s", idx+1, fallback(node.Name, "unnamed")))
		}
	}
	e.nodeSelect.SetOptions(nodeOptions)
	if e.nodeIndex >= 0 && e.nodeIndex < len(nodeOptions) {
		e.nodeSelect.SetSelected(nodeOptions[e.nodeIndex])
	} else {
		e.nodeSelect.ClearSelected()
	}

	e.populateRouteForm()
	e.populateGroupForm()
	e.populateNodeForm()
}

func (e *configEditor) populateRouteForm() {
	if e.routeIndex < 0 || e.routeIndex >= len(e.routes) {
		e.routeModelEntry.SetText("")
		e.routeModeSelect.ClearSelected()
		e.routeProviderGroupPick.ClearSelected()
		e.routeUpstreamEntry.SetText("")
		return
	}

	route := e.routes[e.routeIndex]
	e.routeModelEntry.SetText(route.Model)
	e.routeModeSelect.SetSelected(fallback(route.Mode, "direct"))
	if optionExists(e.routeProviderGroupPick.Options, route.ProviderGroup) {
		e.routeProviderGroupPick.SetSelected(route.ProviderGroup)
	} else {
		e.routeProviderGroupPick.ClearSelected()
	}
	e.routeUpstreamEntry.SetText(route.UpstreamModel)
}

func (e *configEditor) populateGroupForm() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.groupNameEntry.SetText("")
		e.groupStrategySelect.ClearSelected()
		e.groupTimeoutEntry.SetText("")
		e.groupRetryCountEntry.SetText("")
		e.groupMaxNodeAttemptsEntry.SetText("")
		e.groupFailureThresholdEntry.SetText("")
		e.groupCooldownEntry.SetText("")
		return
	}

	group := e.groups[e.groupIndex]
	e.groupNameEntry.SetText(group.Name)
	e.groupStrategySelect.SetSelected(fallback(group.Strategy, "round_robin"))
	e.groupTimeoutEntry.SetText(durationText(group.Timeout.Duration, 30*time.Second))
	e.groupRetryCountEntry.SetText(strconv.Itoa(group.RetryCount))
	e.groupMaxNodeAttemptsEntry.SetText(strconv.Itoa(group.MaxNodeAttempts))
	e.groupFailureThresholdEntry.SetText(strconv.Itoa(group.PassiveHealth.FailureThreshold))
	e.groupCooldownEntry.SetText(durationText(group.PassiveHealth.Cooldown.Duration, 30*time.Second))
}

func (e *configEditor) populateNodeForm() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) || e.nodeIndex < 0 || e.nodeIndex >= len(e.groups[e.groupIndex].Nodes) {
		e.nodeNameEntry.SetText("")
		e.nodeBaseURLEntry.SetText("")
		e.nodeAPIKeyEntry.SetText("")
		e.nodeAPIKeyEnv.SetText("")
		e.nodeHeadersEntry.SetText("{}")
		return
	}

	node := e.groups[e.groupIndex].Nodes[e.nodeIndex]
	e.nodeNameEntry.SetText(node.Name)
	e.nodeBaseURLEntry.SetText(node.BaseURL)
	e.nodeAPIKeyEntry.SetText(node.APIKey)
	e.nodeAPIKeyEnv.SetText(node.APIKeyEnv)
	e.nodeHeadersEntry.SetText(formatHeadersJSON(node.Headers))
}

func (e *configEditor) clampRouteIndex() {
	switch {
	case len(e.routes) == 0:
		e.routeIndex = -1
	case e.routeIndex < 0:
		e.routeIndex = 0
	case e.routeIndex >= len(e.routes):
		e.routeIndex = len(e.routes) - 1
	}
}

func (e *configEditor) clampGroupIndex() {
	switch {
	case len(e.groups) == 0:
		e.groupIndex = -1
	case e.groupIndex < 0:
		e.groupIndex = 0
	case e.groupIndex >= len(e.groups):
		e.groupIndex = len(e.groups) - 1
	}
}

func (e *configEditor) clampNodeIndex() {
	if e.groupIndex < 0 || e.groupIndex >= len(e.groups) {
		e.nodeIndex = -1
		return
	}

	nodes := e.groups[e.groupIndex].Nodes
	switch {
	case len(nodes) == 0:
		e.nodeIndex = -1
	case e.nodeIndex < 0:
		e.nodeIndex = 0
	case e.nodeIndex >= len(nodes):
		e.nodeIndex = len(nodes) - 1
	}
}

func (e *configEditor) firstGroupName() string {
	if len(e.groups) == 0 {
		return ""
	}
	return strings.TrimSpace(e.groups[0].Name)
}

func defaultProviderGroup(index int) clientconfig.ProviderGroup {
	return clientconfig.ProviderGroup{
		Name:            fmt.Sprintf("provider-group-%d", index),
		Strategy:        "round_robin",
		Timeout:         clientconfig.Duration{Duration: 30 * time.Second},
		RetryCount:      0,
		MaxNodeAttempts: 1,
		PassiveHealth: clientconfig.PassiveHealthConfig{
			FailureThreshold: 1,
			Cooldown:         clientconfig.Duration{Duration: 30 * time.Second},
		},
		Nodes: []clientconfig.Node{defaultNode(1)},
	}
}

func defaultNode(index int) clientconfig.Node {
	return clientconfig.Node{
		Name:    fmt.Sprintf("node-%d", index),
		BaseURL: "https://api.example.com",
	}
}

func validateDraft(routes []clientconfig.Route, groups []clientconfig.ProviderGroup) error {
	return runtimeconfig.Snapshot{
		Routes:         routes,
		ProviderGroups: groups,
	}.Validate()
}

func prettyJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeRoutesJSON(value string) ([]clientconfig.Route, error) {
	var routes []clientconfig.Route
	if err := decodeStrictJSON(value, &routes); err != nil {
		return nil, fmt.Errorf("decode routes JSON: %w", err)
	}
	return routes, nil
}

func decodeProviderGroupsJSON(value string) ([]clientconfig.ProviderGroup, error) {
	var groups []clientconfig.ProviderGroup
	if err := decodeStrictJSON(value, &groups); err != nil {
		return nil, fmt.Errorf("decode provider groups JSON: %w", err)
	}
	return groups, nil
}

func decodeStrictJSON(value string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("unexpected trailing JSON content")
	}
	return nil
}

func parseIntegerField(name, raw string, allowZero bool) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if allowZero {
		if number < 0 {
			return 0, fmt.Errorf("%s cannot be negative", name)
		}
		return number, nil
	}
	if number <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return number, nil
}

func parseDurationField(name, raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return duration, nil
}

func parseHeadersJSON(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil, nil
	}

	var headers map[string]string
	if err := decodeStrictJSON(trimmed, &headers); err != nil {
		return nil, fmt.Errorf("decode headers JSON: %w", err)
	}
	if len(headers) == 0 {
		return nil, nil
	}
	return headers, nil
}

func formatHeadersJSON(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	raw, err := json.MarshalIndent(headers, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func durationText(value, fallbackValue time.Duration) string {
	if value <= 0 {
		return fallbackValue.String()
	}
	return value.String()
}

func cloneRoutes(routes []clientconfig.Route) []clientconfig.Route {
	cloned := make([]clientconfig.Route, len(routes))
	copy(cloned, routes)
	return cloned
}

func cloneProviderGroups(groups []clientconfig.ProviderGroup) []clientconfig.ProviderGroup {
	cloned := make([]clientconfig.ProviderGroup, 0, len(groups))
	for _, group := range groups {
		next := group
		next.Nodes = make([]clientconfig.Node, 0, len(group.Nodes))
		for _, node := range group.Nodes {
			nextNode := node
			nextNode.Headers = cloneStringMap(node.Headers)
			next.Nodes = append(next.Nodes, nextNode)
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func optionExists(options []string, selected string) bool {
	return indexOfOption(options, selected) >= 0
}

func indexOfOption(options []string, value string) int {
	for idx, option := range options {
		if option == value {
			return idx
		}
	}
	return -1
}

func setButtonState(button *widget.Button, enabled bool) {
	if button == nil {
		return
	}
	if enabled {
		button.Enable()
		return
	}
	button.Disable()
}
