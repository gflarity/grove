package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	rtdebug "runtime/debug"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// header defines a table header with expansion and alignment
type header struct {
	name      string
	expansion int
	align     int
}

// Pane represents which pane is active
type Pane int

const (
	ResourcesPane Pane = iota
	EventsPane
)

// ViewType represents the current view in the hierarchy
type ViewType int

const (
	ForestView ViewType = iota
	PodCliqueSetView
	PodCliqueSetReplicaView
	PodCliqueScalingGroupView
	PodCliqueView
	PodView
)

// ViewState tracks the current navigation state
type ViewState struct {
	viewType             ViewType
	selectedPodCliqueSet string
	selectedReplicaIndex string // The replica index (e.g., "0", "1", "2")
	selectedScalingGroup string
	selectedPodClique    string
	selectedPod          string
}

// Resource and Event types are defined in k8s_client.go

// App encapsulates the split-pane application
type App struct {
	*tview.Application
	activePane         Pane
	resourcesTable     *tview.Table
	resourcesView      *tview.TextView // For Pod YAML view
	eventsTable        *tview.Table
	statusBar          *tview.TextView
	filterInput        *tview.InputField // Search/filter input bar
	rootFlex           *tview.Flex       // Root layout container
	mainFlex           *tview.Flex       // Main layout container
	filterActive       bool              // Whether search/filter mode is active
	filterText         string            // Current filter text
	viewState          ViewState
	allResources       map[string][]Resource // Key is parent identifier
	allEvents          []Event
	podYAMLData        map[string]string // Pod name -> YAML content
	k8sClient          *K8sClient                  // Kubernetes client
	ctx                context.Context             // Context for K8s operations
	cachedTopologyInfo *TopologyInfo               // Cached topology info from current PCS
	cachedPods         map[string]CachedPodInfo    // Pod name -> info (for current PCS)
	cachedNodeLabels   map[string]map[string]string // Node name -> topology labels
}

func NewApp() *App {
	// Initialize Kubernetes client
	debugLog("initializing Kubernetes client")
	k8sClient, err := NewK8sClient()
	if err != nil {
		// If we can't connect to Kubernetes, show an error but don't crash
		// The app will show empty data
		debugLog("WARNING: failed to initialize Kubernetes client: %v", err)
		k8sClient = nil
	} else {
		debugLog("Kubernetes client initialized successfully")
	}

	app := &App{
		Application:  tview.NewApplication(),
		activePane:   ResourcesPane,
		allResources: make(map[string][]Resource),
		podYAMLData:  make(map[string]string),
		k8sClient:    k8sClient,
		ctx:          context.Background(),
		viewState: ViewState{
			viewType: ForestView,
		},
	}

	// Load initial data
	debugLog("loading initial forest data")
	app.loadForestData()

	debugLog("NewApp complete, viewState=%+v", app.viewState)
	return app
}

// loadForestData loads PodCliqueSet data from Kubernetes
func (a *App) loadForestData() {
	if a.k8sClient == nil {
		// No K8s client, show empty data
		debugLog("loadForestData: no K8s client, showing empty data")
		a.allResources["forest"] = []Resource{}
		return
	}

	debugLog("loadForestData: fetching all PodCliqueSets")

	resources, err := a.k8sClient.GetAllPodCliqueSets(a.ctx)
	if err != nil {
		debugLog("ERROR loading PodCliqueSets: %v", err)
		a.allResources["forest"] = []Resource{}
		return
	}

	debugLog("loaded %d PodCliqueSets", len(resources))
	a.allResources["forest"] = resources
}

// loadPodCliqueSetReplicas loads replica index resources for a PodCliqueSet
func (a *App) loadPodCliqueSetReplicas(pcsName, namespace string) {
	if a.k8sClient == nil {
		return
	}

	key := "PodCliqueSet/" + pcsName

	// Get all replica indexes
	replicaIndexes, err := a.k8sClient.GetReplicaIndexesForPodCliqueSet(a.ctx, pcsName, namespace)
	if err != nil {
		debugLog("ERROR loading replica indexes for %s/%s: %v", namespace, pcsName, err)
		replicaIndexes = []string{}
	} else {
		debugLog("loaded %d replica indexes for %s/%s: %v", len(replicaIndexes), namespace, pcsName, replicaIndexes)
	}

	// Create virtual PodCliqueSetReplica resources
	resources := make([]Resource, 0, len(replicaIndexes))
	for _, replicaIndex := range replicaIndexes {
		// Get stats for this replica
		scalingGroups, _ := a.k8sClient.GetPodCliqueScalingGroupsForPodCliqueSetReplica(a.ctx, pcsName, namespace, replicaIndex)
		podCliques, _ := a.k8sClient.GetPodCliquesForPodCliqueSetReplica(a.ctx, pcsName, namespace, replicaIndex)

		// Calculate aggregate ready/scheduled counts
		var totalReady, totalScheduled, totalReplicas int
		for _, sg := range scalingGroups {
			// Parse ready string like "3/5"
			var ready, replicas int
			fmt.Sscanf(sg.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(sg.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}
		for _, pc := range podCliques {
			var ready, replicas int
			fmt.Sscanf(pc.Ready, "%d/%d", &ready, &replicas)
			totalReady += ready
			totalReplicas += replicas

			var scheduled, scheduledMax int
			fmt.Sscanf(pc.Scheduled, "%d/%d", &scheduled, &scheduledMax)
			totalScheduled += scheduled
		}

		// Topology: replicas inherit the PCS-level topology
		replicaTopology := "N/A"
		if a.cachedTopologyInfo != nil && a.cachedTopologyInfo.PCSPackDomain != "" {
			replicaTopology = ResolveTopologyDisplay("", a.cachedTopologyInfo.PCSPackDomain)
			// Enhance with actual value from pod nodes
			domain := extractDomain(replicaTopology)
			value := resolveTopologyValueByReplicaIndex(domain, pcsName, replicaIndex, a.cachedTopologyInfo, a.cachedPods, a.cachedNodeLabels)
			replicaTopology = enhanceTopologyDisplay(replicaTopology, value)
		}

		resources = append(resources, Resource{
			Name:       fmt.Sprintf("%s-replica-%s", pcsName, replicaIndex),
			Type:       "PodCliqueSetReplica",
			Ready:      fmt.Sprintf("%d/%d", totalReady, totalReplicas),
			Scheduled:  fmt.Sprintf("%d/%d", totalScheduled, totalReplicas),
			Status:     "",
			Namespace:  namespace,
			ParentType: "PodCliqueSet",
			ParentName: pcsName,
			Topology:   replicaTopology,
		})
	}

	a.allResources[key] = resources
}

// loadPodCliqueSetReplicaChildren loads children resources for a specific PodCliqueSet replica
func (a *App) loadPodCliqueSetReplicaChildren(pcsName, namespace, replicaIndex string) {
	if a.k8sClient == nil {
		return
	}

	key := "PodCliqueSetReplica/" + pcsName + "/" + replicaIndex

	// Get PodCliqueScalingGroups for this replica
	scalingGroups, err := a.k8sClient.GetPodCliqueScalingGroupsForPodCliqueSetReplica(a.ctx, pcsName, namespace, replicaIndex)
	if err != nil {
		debugLog("ERROR loading PodCliqueScalingGroups for %s/%s replica %s: %v", namespace, pcsName, replicaIndex, err)
		scalingGroups = []Resource{}
	} else {
		debugLog("loaded %d PodCliqueScalingGroups for %s/%s replica %s", len(scalingGroups), namespace, pcsName, replicaIndex)
	}

	// Get standalone PodCliques for this replica
	podCliques, err := a.k8sClient.GetPodCliquesForPodCliqueSetReplica(a.ctx, pcsName, namespace, replicaIndex)
	if err != nil {
		debugLog("ERROR loading standalone PodCliques for %s/%s replica %s: %v", namespace, pcsName, replicaIndex, err)
		podCliques = []Resource{}
	} else {
		debugLog("loaded %d standalone PodCliques for %s/%s replica %s", len(podCliques), namespace, pcsName, replicaIndex)
	}

	// Set topology on PCSGs
	for i := range scalingGroups {
		pcsgConfigName := ExtractConfigName(scalingGroups[i].Name, pcsName, replicaIndex)
		if a.cachedTopologyInfo != nil {
			scalingGroups[i].Topology = a.cachedTopologyInfo.ResolvePCSGTopology(pcsgConfigName)
			// Enhance with actual value
			domain := extractDomain(scalingGroups[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podcliquescalinggroup", scalingGroups[i].Name, a.cachedTopologyInfo, a.cachedPods, a.cachedNodeLabels)
			scalingGroups[i].Topology = enhanceTopologyDisplay(scalingGroups[i].Topology, value)
		} else {
			scalingGroups[i].Topology = "N/A"
		}
	}

	// Set topology on standalone PodCliques
	for i := range podCliques {
		cliqueTemplateName := ExtractConfigName(podCliques[i].Name, pcsName, replicaIndex)
		if a.cachedTopologyInfo != nil {
			podCliques[i].Topology = a.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			// Enhance with actual value
			domain := extractDomain(podCliques[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podclique", podCliques[i].Name, a.cachedTopologyInfo, a.cachedPods, a.cachedNodeLabels)
			podCliques[i].Topology = enhanceTopologyDisplay(podCliques[i].Topology, value)
		} else {
			podCliques[i].Topology = "N/A"
		}
	}

	// Combine them
	resources := append(scalingGroups, podCliques...)
	a.allResources[key] = resources
}

// loadEventsForPodCliqueSet loads events for a PodCliqueSet (all replicas)
func (a *App) loadEventsForPodCliqueSet(pcsName, namespace string) {
	if a.k8sClient == nil {
		a.allEvents = []Event{}
		return
	}

	events, err := a.k8sClient.GetEventsForPodCliqueSet(a.ctx, pcsName, namespace)
	if err != nil {
		debugLog("ERROR loading events for PCS %s/%s: %v", namespace, pcsName, err)
		a.allEvents = []Event{}
		return
	}

	debugLog("loaded %d events for PCS %s/%s", len(events), namespace, pcsName)
	a.allEvents = events
}

// loadEventsForPodCliqueSetReplica loads events for a specific PodCliqueSet replica
func (a *App) loadEventsForPodCliqueSetReplica(pcsName, namespace, replicaIndex string) {
	if a.k8sClient == nil {
		a.allEvents = []Event{}
		return
	}

	events, err := a.k8sClient.GetEventsForPodCliqueSetReplica(a.ctx, pcsName, namespace, replicaIndex)
	if err != nil {
		debugLog("ERROR loading events for PCS %s/%s replica %s: %v", namespace, pcsName, replicaIndex, err)
		a.allEvents = []Event{}
		return
	}

	debugLog("loaded %d events for PCS %s/%s replica %s", len(events), namespace, pcsName, replicaIndex)
	a.allEvents = events
}

// loadPodCliqueScalingGroupChildren loads children resources for a PodCliqueScalingGroup
func (a *App) loadPodCliqueScalingGroupChildren(pcsgName, namespace string) {
	if a.k8sClient == nil {
		return
	}

	key := "PodCliqueScalingGroup/" + pcsgName

	// Get PodCliques that belong to this scaling group
	podCliques, err := a.k8sClient.GetPodCliquesForPodCliqueScalingGroup(a.ctx, pcsgName, namespace)
	if err != nil {
		debugLog("ERROR loading PodCliques for PCSG %s/%s: %v", namespace, pcsgName, err)
		podCliques = []Resource{}
	} else {
		debugLog("loaded %d PodCliques for PCSG %s/%s", len(podCliques), namespace, pcsgName)
	}

	// Set topology on PodCliques within this PCSG
	pcsgConfigName := ExtractConfigName(pcsgName, a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
	for i := range podCliques {
		cliqueTemplateName := ExtractCliqueTemplateNameFromPCSGChild(podCliques[i].Name, pcsgName)
		if a.cachedTopologyInfo != nil {
			podCliques[i].Topology = a.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			// Enhance with actual value
			domain := extractDomain(podCliques[i].Topology)
			value := resolveTopologyValue(domain, "grove.io/podclique", podCliques[i].Name, a.cachedTopologyInfo, a.cachedPods, a.cachedNodeLabels)
			podCliques[i].Topology = enhanceTopologyDisplay(podCliques[i].Topology, value)
		} else {
			podCliques[i].Topology = "N/A"
		}
	}

	a.allResources[key] = podCliques
}

// loadEventsForPodCliqueScalingGroup loads events for a PodCliqueScalingGroup
func (a *App) loadEventsForPodCliqueScalingGroup(pcsgName, namespace string) {
	if a.k8sClient == nil {
		a.allEvents = []Event{}
		return
	}

	events, err := a.k8sClient.GetEventsForPodCliqueScalingGroup(a.ctx, pcsgName, namespace)
	if err != nil {
		debugLog("ERROR loading events for PCSG %s/%s: %v", namespace, pcsgName, err)
		a.allEvents = []Event{}
		return
	}

	debugLog("loaded %d events for PCSG %s/%s", len(events), namespace, pcsgName)
	a.allEvents = events
}

// loadPodCliqueChildren loads children resources for a PodClique (Pods)
func (a *App) loadPodCliqueChildren(podCliqueName, namespace string) {
	if a.k8sClient == nil {
		return
	}

	key := "PodClique/" + podCliqueName

	// Get Pods that belong to this PodClique
	pods, err := a.k8sClient.GetPodsForPodClique(a.ctx, podCliqueName, namespace)
	if err != nil {
		debugLog("ERROR loading Pods for PodClique %s/%s: %v", namespace, podCliqueName, err)
		pods = []Resource{}
	} else {
		debugLog("loaded %d Pods for PodClique %s/%s", len(pods), namespace, podCliqueName)
	}

	// Pods inherit topology from their parent PodClique
	// First resolve the domain, then enhance each pod with its actual node value
	basePodTopology := "N/A"
	if a.cachedTopologyInfo != nil {
		if a.viewState.selectedScalingGroup != "" {
			pcsgConfigName := ExtractConfigName(a.viewState.selectedScalingGroup, a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
			cliqueTemplateName := ExtractCliqueTemplateNameFromPCSGChild(podCliqueName, a.viewState.selectedScalingGroup)
			effectiveClique := a.cachedTopologyInfo.ResolveCliqueInPCSGTopology(cliqueTemplateName, pcsgConfigName)
			basePodTopology = wrapInherited(effectiveClique)
		} else {
			cliqueTemplateName := ExtractConfigName(podCliqueName, a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
			effectiveClique := a.cachedTopologyInfo.ResolveStandaloneCliqueTopology(cliqueTemplateName)
			basePodTopology = wrapInherited(effectiveClique)
		}
	}
	domain := extractDomain(basePodTopology)
	for i := range pods {
		if domain != "" {
			// Resolve value from this pod's actual node
			nodeName := ""
			if cached, ok := a.cachedPods[pods[i].Name]; ok {
				nodeName = cached.NodeName
			}
			value := resolveTopologyValueForNode(domain, nodeName, a.cachedTopologyInfo, a.cachedNodeLabels)
			pods[i].Topology = enhanceTopologyDisplay(basePodTopology, value)
		} else {
			pods[i].Topology = basePodTopology
		}
	}

	a.allResources[key] = pods
}

// loadEventsForPodClique loads events for a PodClique
func (a *App) loadEventsForPodClique(podCliqueName, namespace string) {
	if a.k8sClient == nil {
		a.allEvents = []Event{}
		return
	}

	events, err := a.k8sClient.GetEventsForPodClique(a.ctx, podCliqueName, namespace)
	if err != nil {
		debugLog("ERROR loading events for PodClique %s/%s: %v", namespace, podCliqueName, err)
		a.allEvents = []Event{}
		return
	}

	debugLog("loaded %d events for PodClique %s/%s", len(events), namespace, podCliqueName)
	a.allEvents = events
}

// getCurrentViewKey returns the key for looking up resources in allResources map
func (a *App) getCurrentViewKey() string {
	switch a.viewState.viewType {
	case ForestView:
		return "forest"
	case PodCliqueSetView:
		return "PodCliqueSet/" + a.viewState.selectedPodCliqueSet
	case PodCliqueSetReplicaView:
		return "PodCliqueSetReplica/" + a.viewState.selectedPodCliqueSet + "/" + a.viewState.selectedReplicaIndex
	case PodCliqueScalingGroupView:
		return "PodCliqueScalingGroup/" + a.viewState.selectedScalingGroup
	case PodCliqueView:
		return "PodClique/" + a.viewState.selectedPodClique
	case PodView:
		return "" // Pod view doesn't list resources, it shows YAML
	}
	return "forest"
}

// getViewTitle returns a formatted breadcrumb title for the current view
func (a *App) getViewTitle() string {
	switch a.viewState.viewType {
	case ForestView:
		return "Forest"
	case PodCliqueSetView:
		return fmt.Sprintf("Forest > [cyan]%s[-]", a.viewState.selectedPodCliqueSet)
	case PodCliqueSetReplicaView:
		return fmt.Sprintf("Forest > %s > [cyan]replica-%s[-]", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
	case PodCliqueScalingGroupView:
		return fmt.Sprintf("Forest > %s > replica-%s > [purple]%s[-]", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex, a.viewState.selectedScalingGroup)
	case PodCliqueView:
		parent := fmt.Sprintf("%s > replica-%s", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
		if a.viewState.selectedScalingGroup != "" {
			parent = fmt.Sprintf("%s > replica-%s > %s", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex, a.viewState.selectedScalingGroup)
		}
		return fmt.Sprintf("Forest > %s > [aqua]%s[-]", parent, a.viewState.selectedPodClique)
	case PodView:
		parent := fmt.Sprintf("%s > replica-%s", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex)
		if a.viewState.selectedScalingGroup != "" {
			parent = fmt.Sprintf("%s > replica-%s > %s", a.viewState.selectedPodCliqueSet, a.viewState.selectedReplicaIndex, a.viewState.selectedScalingGroup)
		}
		return fmt.Sprintf("Forest > %s > %s > [lime]%s[-]", parent, a.viewState.selectedPodClique, a.viewState.selectedPod)
	}
	return "Forest"
}

func (a *App) createResourcesTable() *tview.Table {
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetSeparator(' ').
		SetFixed(1, 0)

	table.SetBackgroundColor(tcell.ColorBlack)
	table.SetBorder(true)
	table.SetBorderColor(tcell.ColorYellow) // Active border
	table.SetTitle(" [yellow::b]Resources[-] ")
	table.SetTitleAlign(tview.AlignLeft)

	return table
}

// refreshResourcesView updates the top pane based on current view state
func (a *App) refreshResourcesView() {
	debugLog("refreshResourcesView: viewType=%d", a.viewState.viewType)
	// For Pod view, show YAML instead of table
	if a.viewState.viewType == PodView {
		a.refreshPodYAMLView()
		return
	}
	a.refreshResourcesTable()
}

// refreshPodYAMLView shows the Pod YAML in the resources pane
func (a *App) refreshPodYAMLView() {
	if a.resourcesView == nil {
		return
	}

	// Update title with breadcrumb
	if a.activePane == ResourcesPane {
		a.resourcesView.SetTitle(fmt.Sprintf(" [yellow::b]Pod Status[-] [dimgray]|[-] %s ", a.getViewTitle()))
		a.resourcesView.SetBorderColor(tcell.ColorYellow)
	} else {
		a.resourcesView.SetTitle(fmt.Sprintf(" [dimgray]Pod Status |[-] %s ", a.getViewTitle()))
		a.resourcesView.SetBorderColor(tcell.ColorDimGray)
	}

	// Get YAML for selected pod
	yaml, exists := a.podYAMLData[a.viewState.selectedPod]
	if !exists {
		yaml = fmt.Sprintf("# No YAML data available for pod: %s", a.viewState.selectedPod)
	}

	a.resourcesView.SetText(yaml)
	a.resourcesView.ScrollToBeginning()
}

// refreshResourcesTable updates the resources table based on current view state
func (a *App) refreshResourcesTable() {
	table := a.resourcesTable

	// Clear table
	table.Clear()

	// Update title with breadcrumb and optional filter indicator
	filterTag := ""
	if a.filterText != "" {
		filterTag = fmt.Sprintf(" [dimgray]| filter:[-] [white]%s[-]", a.filterText)
	}
	if a.activePane == ResourcesPane {
		table.SetTitle(fmt.Sprintf(" [yellow::b]Resources[-] [dimgray]|[-] %s%s ", a.getViewTitle(), filterTag))
	} else {
		table.SetTitle(fmt.Sprintf(" [dimgray]Resources |[-] %s%s ", a.getViewTitle(), filterTag))
	}

	// Headers with expansion settings
	// Use "PHASE" for Pod view, "SCHEDULED" for other views
	lastColumnHeader := "SCHEDULED"
	if a.viewState.viewType == PodCliqueView {
		// Check if we're showing Pods (not other resources)
		viewKey := a.getCurrentViewKey()
		if resources, exists := a.allResources[viewKey]; exists && len(resources) > 0 {
			if resources[0].Type == "Pod" {
				lastColumnHeader = "PHASE"
			}
		}
	}

	headers := []header{
		{"NAMESPACE", 2, tview.AlignLeft},
		{"TYPE", 2, tview.AlignLeft},
		{"NAME", 3, tview.AlignLeft},
		{"TOPOLOGY", 1, tview.AlignCenter},
		{"READY", 1, tview.AlignCenter},
		{lastColumnHeader, 2, tview.AlignLeft},
	}

	for col, hdr := range headers {
		cell := tview.NewTableCell(hdr.name).
			SetTextColor(tcell.ColorDimGray).
			SetSelectable(false).
			SetExpansion(hdr.expansion).
			SetAlign(hdr.align)

		if col == 0 {
			cell.SetText(" " + hdr.name)
		}
		table.SetCell(0, col, cell)
	}

	statusColors := map[string]tcell.Color{
		"Running": tcell.ColorGreen,
		"Scaling": tcell.ColorYellow,
		"Pending": tcell.ColorOrange,
	}

	typeColors := map[string]tcell.Color{
		"PodClique":             tcell.ColorAqua,
		"PodCliqueSet":          tcell.ColorBlue,
		"PodCliqueSetReplica":   tcell.NewRGBColor(0, 255, 255), // Cyan
		"PodCliqueScalingGroup": tcell.ColorPurple,
		"Pod":                   tcell.ColorLime,
	}

	// Get resources for current view
	viewKey := a.getCurrentViewKey()
	resources, exists := a.allResources[viewKey]
	if !exists {
		debugLog("refreshResourcesTable: no resources found for key=%q", viewKey)
		resources = []Resource{}
	} else {
		debugLog("refreshResourcesTable: displaying %d resources for key=%q", len(resources), viewKey)
	}

	// Apply filter if active
	if a.filterText != "" {
		filter := strings.ToLower(a.filterText)
		var filtered []Resource
		for _, r := range resources {
			if strings.Contains(strings.ToLower(r.Name), filter) ||
				strings.Contains(strings.ToLower(r.Type), filter) ||
				strings.Contains(strings.ToLower(r.Namespace), filter) {
				filtered = append(filtered, r)
			}
		}
		debugLog("filter %q: %d -> %d resources", a.filterText, len(resources), len(filtered))
		resources = filtered
	}

	// Add data rows
	for row, resource := range resources {
		rowData := []string{
			resource.Namespace,
			resource.Type,
			resource.Name,
			resource.Topology,
			resource.Ready,
			resource.Scheduled,
		}

		for col, cellText := range rowData {
			cell := tview.NewTableCell(cellText).
				SetExpansion(headers[col].expansion).
				SetAlign(headers[col].align)

			if col == 0 {
				cell.SetText(" " + cellText)
			}

			// Color based on column
			switch col {
			case 1: // TYPE column
				if color, ok := typeColors[cellText]; ok {
					cell.SetTextColor(color)
				}
			case 3: // TOPOLOGY column
				if cellText == "N/A" {
					cell.SetTextColor(tcell.ColorDimGray)
				} else if len(cellText) > 0 && cellText[0] == '(' {
					// Inherited topology shown in dimmer color
					cell.SetTextColor(tcell.ColorDarkCyan)
				} else {
					cell.SetTextColor(tcell.ColorWhite)
				}
			case 4: // READY column
				if color, ok := statusColors[cellText]; ok {
					cell.SetTextColor(color)
				}
			}

			table.SetCell(row+1, col, cell)
		}
	}

	// Selection handler
	table.SetSelectionChangedFunc(func(row, column int) {
		// Clear previous selection
		for r := 1; r < table.GetRowCount(); r++ {
			for c := 0; c < table.GetColumnCount(); c++ {
				if cell := table.GetCell(r, c); cell != nil {
					cell.SetBackgroundColor(tcell.ColorBlack)
				}
			}
		}

		// Highlight current row
		if row > 0 {
			for c := 0; c < table.GetColumnCount(); c++ {
				if cell := table.GetCell(row, c); cell != nil {
					cell.SetBackgroundColor(tcell.NewRGBColor(40, 40, 40))
				}
			}
		}

		if a.activePane == ResourcesPane && row > 0 && row < table.GetRowCount() {
			// If in Forest view and a PodCliqueSet is selected, load its events
			if a.viewState.viewType == ForestView {
				selectedName := strings.TrimSpace(table.GetCell(row, 2).Text)
				selectedNamespace := strings.TrimSpace(table.GetCell(row, 0).Text)
				selectedType := strings.TrimSpace(table.GetCell(row, 1).Text)

				if selectedType == "PodCliqueSet" {
					a.loadEventsForPodCliqueSet(selectedName, selectedNamespace)
				}
			}

			// If in PodCliqueSetView and a PodCliqueSetReplica is selected, load its events
			if a.viewState.viewType == PodCliqueSetView {
				selectedName := strings.TrimSpace(table.GetCell(row, 2).Text)
				selectedNamespace := strings.TrimSpace(table.GetCell(row, 0).Text)
				selectedType := strings.TrimSpace(table.GetCell(row, 1).Text)

				if selectedType == "PodCliqueSetReplica" {
					// Extract replica index from name
					parts := strings.Split(selectedName, "-replica-")
					if len(parts) == 2 {
						a.loadEventsForPodCliqueSetReplica(a.viewState.selectedPodCliqueSet, selectedNamespace, parts[1])
					}
				}
			}

			// If in PodCliqueSetReplicaView and a child resource is selected, load its events
			if a.viewState.viewType == PodCliqueSetReplicaView {
				selectedName := strings.TrimSpace(table.GetCell(row, 2).Text)
				selectedNamespace := strings.TrimSpace(table.GetCell(row, 0).Text)
				selectedType := strings.TrimSpace(table.GetCell(row, 1).Text)

				if selectedType == "PodCliqueScalingGroup" {
					a.loadEventsForPodCliqueScalingGroup(selectedName, selectedNamespace)
				} else if selectedType == "PodClique" {
					a.loadEventsForPodClique(selectedName, selectedNamespace)
				}
			}

			a.updateStatusBar()
			a.refreshEventsTable()
		}
	})

	// Select first data row if available
	if table.GetRowCount() > 1 {
		table.Select(1, 0)
	}
}

func (a *App) createEventsTable() *tview.Table {
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetSeparator(' ').
		SetFixed(1, 0)

	table.SetBackgroundColor(tcell.ColorBlack)
	table.SetBorder(true)
	table.SetBorderColor(tcell.ColorDimGray) // Inactive border
	table.SetTitle(" [dimgray]Events[-] ")
	table.SetTitleAlign(tview.AlignLeft)

	return table
}

// getFilteredEvents returns events filtered by current selection
func (a *App) getFilteredEvents() []Event {
	// In Pod view, show events for that specific pod
	if a.viewState.viewType == PodView {
		filtered := []Event{}
		for _, event := range a.allEvents {
			if event.Parent == a.viewState.selectedPod {
				filtered = append(filtered, event)
			}
		}
		return filtered
	}

	// In Forest view, show events for the selected PodCliqueSet
	if a.viewState.viewType == ForestView {
		// Events are already filtered for the selected PodCliqueSet in allEvents
		// when selection changes, so just return them all
		return a.allEvents
	}

	// In PodCliqueSetView, show events for the selected replica
	if a.viewState.viewType == PodCliqueSetView {
		// If a PodCliqueSetReplica is selected, show events for that replica
		row, _ := a.resourcesTable.GetSelection()
		if row >= 1 && row < a.resourcesTable.GetRowCount() {
			selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
			selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)

			if selectedType == "PodCliqueSetReplica" {
				// Extract replica index from name
				parts := strings.Split(selectedName, "-replica-")
				if len(parts) == 2 {
					// Filter to just this replica's events
					filtered := []Event{}
					for _, event := range a.allEvents {
						// Events should match resources from this replica
						// This is already filtered in allEvents when the replica is selected
						filtered = append(filtered, event)
					}
					return filtered
				}
			}
		}
		// No replica selected or invalid selection, show all events
		return a.allEvents
	}

	// In PodClique view, if a Pod is selected, show only that Pod's events
	if a.viewState.viewType == PodCliqueView {
		row, _ := a.resourcesTable.GetSelection()
		if row >= 1 && row < a.resourcesTable.GetRowCount() {
			selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
			selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)

			if selectedType == "Pod" {
				// Filter to just this pod's events
				filtered := []Event{}
				for _, event := range a.allEvents {
					if event.Parent == selectedName {
						filtered = append(filtered, event)
					}
				}
				return filtered
			}
		}
		// No pod selected or invalid selection, show all events for the PodClique
		return a.allEvents
	}

	// In PodCliqueSetReplicaView, show all events for the replica
	if a.viewState.viewType == PodCliqueSetReplicaView {
		// Events are already filtered for this replica in allEvents
		return a.allEvents
	}

	// In table views, check if there's a selection
	row, _ := a.resourcesTable.GetSelection()
	if row < 1 || row >= a.resourcesTable.GetRowCount() {
		// No selection, filter by current view
		filterKey := ""
		switch a.viewState.viewType {
		case PodCliqueSetView:
			filterKey = a.viewState.selectedPodCliqueSet
		case PodCliqueScalingGroupView:
			filterKey = a.viewState.selectedScalingGroup
		case PodCliqueView:
			filterKey = a.viewState.selectedPodClique
		}

		filtered := []Event{}
		for _, event := range a.allEvents {
			if strings.Contains(event.Parent, filterKey) {
				filtered = append(filtered, event)
			}
		}
		return filtered
	}

	// Get the selected resource name (column 2 = NAME)
	selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)

	// Filter events by selected resource
	filtered := []Event{}
	for _, event := range a.allEvents {
		if event.Parent == selectedName || strings.HasPrefix(event.Parent, selectedName+"-") {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// refreshEventsTable updates the events table based on current selection
func (a *App) refreshEventsTable() {
	table := a.eventsTable

	// Clear table
	table.Clear()

	// Headers with expansion settings
	headers := []header{
		{"TYPE", 1, tview.AlignLeft},
		{"REASON", 2, tview.AlignLeft},
		{"AGE", 1, tview.AlignRight},
		{"FROM", 2, tview.AlignLeft},
		{"MESSAGE", 6, tview.AlignLeft},
	}

	for col, hdr := range headers {
		cell := tview.NewTableCell(hdr.name).
			SetTextColor(tcell.ColorDimGray).
			SetSelectable(false).
			SetExpansion(hdr.expansion).
			SetAlign(hdr.align)

		if col == 0 {
			cell.SetText(" " + hdr.name)
		}
		table.SetCell(0, col, cell)
	}

	typeColors := map[string]tcell.Color{
		"Normal":  tcell.ColorGreen,
		"Warning": tcell.ColorYellow,
		"Error":   tcell.ColorRed,
	}

	// Get filtered events
	events := a.getFilteredEvents()

	// Add data rows
	for row, event := range events {
		rowData := []string{
			event.Type,
			event.Reason,
			event.Age,
			event.From,
			event.Message,
		}

		for col, cellText := range rowData {
			cell := tview.NewTableCell(cellText).
				SetExpansion(headers[col].expansion).
				SetAlign(headers[col].align)

			if col == 0 {
				cell.SetText(" " + cellText)
			}

			// Color based on column
			if col == 0 { // TYPE column
				if color, ok := typeColors[cellText]; ok {
					cell.SetTextColor(color)
				}
			}

			table.SetCell(row+1, col, cell)
		}
	}

	// Selection handler
	table.SetSelectionChangedFunc(func(row, column int) {
		// Clear previous selection
		for r := 1; r < table.GetRowCount(); r++ {
			for c := 0; c < table.GetColumnCount(); c++ {
				if cell := table.GetCell(r, c); cell != nil {
					cell.SetBackgroundColor(tcell.ColorBlack)
				}
			}
		}

		// Highlight current row
		if row > 0 {
			for c := 0; c < table.GetColumnCount(); c++ {
				if cell := table.GetCell(row, c); cell != nil {
					cell.SetBackgroundColor(tcell.NewRGBColor(40, 40, 40))
				}
			}
		}

		if a.activePane == EventsPane && row > 0 {
			a.updateStatusBar()
		}
	})

	// Select first data row if available
	if table.GetRowCount() > 1 {
		table.Select(1, 0)
	}
}

// navigateInto drills down into the selected resource
func (a *App) navigateInto() {
	// Clear any active filter when navigating
	a.filterText = ""

	// Can't navigate if in Pod view
	if a.viewState.viewType == PodView {
		debugLog("navigateInto: already in PodView, ignoring")
		return
	}

	row, _ := a.resourcesTable.GetSelection()
	if row < 1 || row >= a.resourcesTable.GetRowCount() {
		debugLog("navigateInto: no valid row selected (row=%d, rowCount=%d)", row, a.resourcesTable.GetRowCount())
		return
	}

	selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)
	selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
	selectedNamespace := strings.TrimSpace(a.resourcesTable.GetCell(row, 0).Text)

	debugLog("navigateInto: type=%s name=%s namespace=%s", selectedType, selectedName, selectedNamespace)

	// Determine the next view based on current view and selected type
	switch selectedType {
	case "PodCliqueSet":
		a.viewState.selectedPodCliqueSet = selectedName
		a.viewState.selectedReplicaIndex = ""
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""

		// Build and cache topology info from the PCS spec.
		// Use a short timeout to avoid blocking the UI if the cluster is slow
		// or CRDs are missing.
		if a.k8sClient != nil {
			debugLog("fetching PCS %s/%s for topology info", selectedNamespace, selectedName)
			topoCtx, topoCancel := context.WithTimeout(a.ctx, 5*time.Second)

			pcs, err := a.k8sClient.GetPodCliqueSet(topoCtx, selectedName, selectedNamespace)
			if err != nil {
				debugLog("ERROR fetching PCS for topology: %v", err)
				a.cachedTopologyInfo = nil
			} else {
				a.cachedTopologyInfo = BuildTopologyInfo(pcs)
				debugLog("built topology info: PCSPackDomain=%q, %d PCSGs, %d cliques",
					a.cachedTopologyInfo.PCSPackDomain, len(a.cachedTopologyInfo.PCSGPackDomains), len(a.cachedTopologyInfo.CliquePackDomains))
			}

			// Fetch ClusterTopology for domain->key mapping
			ct, err := a.k8sClient.GetClusterTopology(topoCtx)
			if err == nil && ct != nil && a.cachedTopologyInfo != nil {
				for _, level := range ct.Spec.Levels {
					a.cachedTopologyInfo.DomainToKey[string(level.Domain)] = level.Key
				}
				debugLog("loaded ClusterTopology domain->key mappings: %v", a.cachedTopologyInfo.DomainToKey)
			} else {
				debugLog("no ClusterTopology available (err=%v, ct=%v)", err, ct)
			}

			// Cache pod info and node labels for topology value resolution
			podInfo, err := a.k8sClient.GetPodInfoForPCS(topoCtx, selectedName, selectedNamespace)
			if err != nil {
				debugLog("ERROR fetching pod info for PCS: %v", err)
				a.cachedPods = nil
			} else {
				debugLog("cached %d pods for PCS %s/%s", len(podInfo), selectedNamespace, selectedName)
				a.cachedPods = podInfo
			}

			if a.cachedTopologyInfo != nil && len(a.cachedTopologyInfo.DomainToKey) > 0 {
				topologyKeys := make([]string, 0, len(a.cachedTopologyInfo.DomainToKey))
				for _, key := range a.cachedTopologyInfo.DomainToKey {
					topologyKeys = append(topologyKeys, key)
				}
				nodeLabels, err := a.k8sClient.GetAllNodeLabels(topoCtx, topologyKeys)
				if err != nil {
					debugLog("ERROR fetching node labels: %v", err)
					a.cachedNodeLabels = nil
				} else {
					debugLog("cached labels for %d nodes", len(nodeLabels))
					a.cachedNodeLabels = nodeLabels
				}
			}

			topoCancel()
		}

		// Get replica indexes for this PodCliqueSet
		if a.k8sClient != nil {
			replicaIndexes, err := a.k8sClient.GetReplicaIndexesForPodCliqueSet(a.ctx, selectedName, selectedNamespace)
			if err != nil {
				debugLog("ERROR loading replica indexes for navigation: %v", err)
				replicaIndexes = []string{}
			} else {
				debugLog("found %d replica indexes for PCS %s: %v", len(replicaIndexes), selectedName, replicaIndexes)
			}

			// If there's only 1 replica, skip directly to PodCliqueSetReplicaView
			if len(replicaIndexes) == 1 {
				a.viewState.viewType = PodCliqueSetReplicaView
				a.viewState.selectedReplicaIndex = replicaIndexes[0]

				// Load children resources and events for this replica
				a.loadPodCliqueSetReplicaChildren(selectedName, selectedNamespace, replicaIndexes[0])
				a.loadEventsForPodCliqueSetReplica(selectedName, selectedNamespace, replicaIndexes[0])
			} else {
				// Multiple replicas, show PodCliqueSetView with replica list
				a.viewState.viewType = PodCliqueSetView

				// Load replica list and events
				a.loadPodCliqueSetReplicas(selectedName, selectedNamespace)
				a.loadEventsForPodCliqueSet(selectedName, selectedNamespace)
			}
		} else {
			// No k8s client, just show empty PodCliqueSetView
			a.viewState.viewType = PodCliqueSetView
			a.loadPodCliqueSetReplicas(selectedName, selectedNamespace)
		}

	case "PodCliqueSetReplica":
		// Extract replica index from name (format: "pcsname-replica-0")
		parts := strings.Split(selectedName, "-replica-")
		if len(parts) == 2 {
			a.viewState.viewType = PodCliqueSetReplicaView
			a.viewState.selectedReplicaIndex = parts[1]
			a.viewState.selectedScalingGroup = ""
			a.viewState.selectedPodClique = ""
			a.viewState.selectedPod = ""

			// Load children resources and events for this replica
			a.loadPodCliqueSetReplicaChildren(a.viewState.selectedPodCliqueSet, selectedNamespace, parts[1])
			a.loadEventsForPodCliqueSetReplica(a.viewState.selectedPodCliqueSet, selectedNamespace, parts[1])
		}

	case "PodCliqueScalingGroup":
		a.viewState.viewType = PodCliqueScalingGroupView
		a.viewState.selectedScalingGroup = selectedName
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""

		// Load children resources and events for this PodCliqueScalingGroup
		a.loadPodCliqueScalingGroupChildren(selectedName, selectedNamespace)
		a.loadEventsForPodCliqueScalingGroup(selectedName, selectedNamespace)
	case "PodClique":
		a.viewState.viewType = PodCliqueView
		a.viewState.selectedPodClique = selectedName
		a.viewState.selectedPod = ""

		// Load children resources (Pods) and events for this PodClique
		a.loadPodCliqueChildren(selectedName, selectedNamespace)
		a.loadEventsForPodClique(selectedName, selectedNamespace)
	case "Pod":
		// Enter Pod detail view
		a.viewState.viewType = PodView
		a.viewState.selectedPod = selectedName

		// Load Pod YAML
		if a.k8sClient != nil {
			debugLog("fetching YAML for Pod %s/%s", selectedNamespace, selectedName)
			yaml, err := a.k8sClient.GetPodYAML(a.ctx, selectedName, selectedNamespace)
			if err != nil {
				debugLog("ERROR loading Pod YAML: %v", err)
				a.podYAMLData[selectedName] = fmt.Sprintf("# Error loading Pod YAML: %v", err)
			} else {
				debugLog("loaded %d bytes of YAML for Pod %s", len(yaml), selectedName)
				a.podYAMLData[selectedName] = yaml
			}
		}

		a.switchToPodView()
	}

	a.refreshResourcesView()
	a.refreshEventsTable()
	a.updateStatusBar()
}

// navigateBack goes up one level in the hierarchy
func (a *App) navigateBack() {
	// Clear any active filter when navigating
	a.filterText = ""

	debugLog("navigateBack: current viewType=%d", a.viewState.viewType)
	switch a.viewState.viewType {
	case ForestView:
		// Already at root, nothing to do
		debugLog("navigateBack: already at ForestView, ignoring")
		return
	case PodCliqueSetView:
		// Go back to Forest
		a.viewState.viewType = ForestView
		a.viewState.selectedPodCliqueSet = ""
		a.viewState.selectedReplicaIndex = ""
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
		a.cachedTopologyInfo = nil
		a.cachedPods = nil
		a.cachedNodeLabels = nil

		// Reload forest data when going back to root
		a.loadForestData()
		a.allEvents = []Event{} // Clear events when at forest view

	case PodCliqueSetReplicaView:
		// Go back to PodCliqueSet view (or Forest if only 1 replica)
		// Check how many replicas there are
		if a.k8sClient != nil {
			// Get namespace from current resources
			viewKey := a.getCurrentViewKey()
			resources, exists := a.allResources[viewKey]
			namespace := "default"
			if exists && len(resources) > 0 {
				namespace = resources[0].Namespace
			}

			replicaIndexes, err := a.k8sClient.GetReplicaIndexesForPodCliqueSet(a.ctx, a.viewState.selectedPodCliqueSet, namespace)
			if err == nil && len(replicaIndexes) == 1 {
				// Only 1 replica, so we came directly from Forest view
				a.viewState.viewType = ForestView
				a.viewState.selectedPodCliqueSet = ""
				a.viewState.selectedReplicaIndex = ""
				a.cachedTopologyInfo = nil
				a.cachedPods = nil
				a.cachedNodeLabels = nil
				a.loadForestData()
				a.allEvents = []Event{}
			} else {
				// Multiple replicas, go back to PodCliqueSet view
				a.viewState.viewType = PodCliqueSetView
				a.viewState.selectedReplicaIndex = ""
				a.viewState.selectedScalingGroup = ""
				a.viewState.selectedPodClique = ""
				a.viewState.selectedPod = ""
				a.loadPodCliqueSetReplicas(a.viewState.selectedPodCliqueSet, namespace)
				a.loadEventsForPodCliqueSet(a.viewState.selectedPodCliqueSet, namespace)
			}
		} else {
			// No k8s client, assume multiple replicas
			a.viewState.viewType = PodCliqueSetView
			a.viewState.selectedReplicaIndex = ""
		}

	case PodCliqueScalingGroupView:
		// Go back to PodCliqueSetReplica
		a.viewState.viewType = PodCliqueSetReplicaView
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case PodCliqueView:
		// Go back to parent (either PodCliqueSetReplica or PodCliqueScalingGroup)
		if a.viewState.selectedScalingGroup != "" {
			a.viewState.viewType = PodCliqueScalingGroupView
		} else {
			a.viewState.viewType = PodCliqueSetReplicaView
		}
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case PodView:
		// Go back to PodClique view
		a.viewState.viewType = PodCliqueView
		a.viewState.selectedPod = ""
		a.switchToTableView()
	}

	a.refreshResourcesView()
	a.refreshEventsTable()
	a.updateStatusBar()
}

func (a *App) updateBorders() {
	if a.viewState.viewType == PodView {
		// Pod view uses TextView instead of Table
		if a.activePane == ResourcesPane {
			a.resourcesView.SetBorderColor(tcell.ColorYellow)
			a.resourcesView.SetTitle(fmt.Sprintf(" [yellow::b]Pod Status[-] [dimgray]|[-] %s ", a.getViewTitle()))
			a.eventsTable.SetBorderColor(tcell.ColorDimGray)
			a.eventsTable.SetTitle(" [dimgray]Events[-] ")
		} else {
			a.resourcesView.SetBorderColor(tcell.ColorDimGray)
			a.resourcesView.SetTitle(fmt.Sprintf(" [dimgray]Pod Status |[-] %s ", a.getViewTitle()))
			a.eventsTable.SetBorderColor(tcell.ColorYellow)
			a.eventsTable.SetTitle(" [yellow::b]Events[-] ")
		}
	} else {
		// Table view
		if a.activePane == ResourcesPane {
			a.resourcesTable.SetBorderColor(tcell.ColorYellow)
			a.resourcesTable.SetTitle(fmt.Sprintf(" [yellow::b]Resources[-] [dimgray]|[-] %s ", a.getViewTitle()))
			a.eventsTable.SetBorderColor(tcell.ColorDimGray)
			a.eventsTable.SetTitle(" [dimgray]Events[-] ")
		} else {
			a.resourcesTable.SetBorderColor(tcell.ColorDimGray)
			a.resourcesTable.SetTitle(fmt.Sprintf(" [dimgray]Resources |[-] %s ", a.getViewTitle()))
			a.eventsTable.SetBorderColor(tcell.ColorYellow)
			a.eventsTable.SetTitle(" [yellow::b]Events[-] ")
		}
	}
}

func (a *App) switchPane() {
	if a.activePane == ResourcesPane {
		a.activePane = EventsPane
		a.SetFocus(a.eventsTable)
	} else {
		a.activePane = ResourcesPane
		if a.viewState.viewType == PodView {
			a.SetFocus(a.resourcesView)
		} else {
			a.SetFocus(a.resourcesTable)
		}
	}
	a.updateBorders()
	a.updateStatusBar()
}

// switchToPodView switches the layout to show Pod YAML view
func (a *App) switchToPodView() {
	// Switch the top pane from table to text view
	a.mainFlex.Clear()
	a.mainFlex.AddItem(a.resourcesView, 0, 1, a.activePane == ResourcesPane)
	a.mainFlex.AddItem(a.eventsTable, 0, 1, a.activePane == EventsPane)

	if a.activePane == ResourcesPane {
		a.SetFocus(a.resourcesView)
	}
}

// switchToTableView switches the layout back to showing resource table
func (a *App) switchToTableView() {
	// Switch the top pane from text view to table
	a.mainFlex.Clear()
	a.mainFlex.AddItem(a.resourcesTable, 0, 1, a.activePane == ResourcesPane)
	a.mainFlex.AddItem(a.eventsTable, 0, 1, a.activePane == EventsPane)

	if a.activePane == ResourcesPane {
		a.SetFocus(a.resourcesTable)
	}
}

func (a *App) updateStatusBar() {
	var text string

	// Pod view has different status bar
	if a.viewState.viewType == PodView {
		if a.activePane == ResourcesPane {
			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> scroll <[white]Esc[dimgray]> back <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Pod Status[-] [dimgray]|[-] Viewing: [white]%s[-] [dimgray]|[-] %s", a.viewState.selectedPod, shortcuts)
		} else {
			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate <[white]Esc[dimgray]> back <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Events[-] [dimgray]|[-] Pod: [white]%s[-] [dimgray]|[-] %s", a.viewState.selectedPod, shortcuts)
		}
		a.statusBar.SetText(text)
		return
	}

	// Table views
	if a.activePane == ResourcesPane {
		row, _ := a.resourcesTable.GetSelection()
		if row > 0 && row < a.resourcesTable.GetRowCount() {
			name := strings.TrimSpace(a.resourcesTable.GetCell(row, 0).Text)
			resourceType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)

			// Show different shortcuts based on whether we can drill down
			canDrillDown := true // All resources can be drilled down now
			canGoBack := a.viewState.viewType != ForestView

			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if canDrillDown {
				shortcuts += " <[white]Enter[dimgray]> drill down"
			}
			if canGoBack {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"

			text = fmt.Sprintf(" [yellow]Resources[-] [dimgray]|[-] Selected: [white]%s[-] [dimgray](%s)[-] [dimgray]|[-] %s", name, resourceType, shortcuts)
		} else {
			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if a.viewState.viewType != ForestView {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Resources[-] [dimgray]|[-] %s", shortcuts)
		}
	} else {
		row, _ := a.eventsTable.GetSelection()
		if row > 0 && row < a.eventsTable.GetRowCount() {
			eventType := strings.TrimSpace(a.eventsTable.GetCell(row, 0).Text)
			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if a.viewState.viewType != ForestView {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Events[-] [dimgray]|[-] Selected: [white]%s[-] [dimgray]|[-] %s", eventType, shortcuts)
		} else {
			shortcuts := "<[white]/[dimgray]> filter <[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if a.viewState.viewType != ForestView {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Events[-] [dimgray]|[-] %s", shortcuts)
		}
	}
	a.statusBar.SetText(text)
}

// activateFilter enters search/filter mode, showing the filter input bar.
func (a *App) activateFilter() {
	if a.filterActive {
		return
	}
	a.filterActive = true
	a.filterText = ""
	a.filterInput.SetText("")

	// Show the filter bar at the top of the root layout
	a.rootFlex.Clear()
	a.rootFlex.AddItem(a.filterInput, 1, 0, true)
	a.rootFlex.AddItem(a.mainFlex, 0, 1, false)
	a.rootFlex.AddItem(a.statusBar, 1, 0, false)

	a.SetFocus(a.filterInput)
	debugLog("filter mode activated")
}

// deactivateFilter exits search/filter mode.
// If clearFilter is true the filter text is cleared and the table is refreshed.
func (a *App) deactivateFilter(clearFilter bool) {
	if !a.filterActive {
		return
	}
	a.filterActive = false

	if clearFilter {
		a.filterText = ""
		a.filterInput.SetText("")
	}

	// Hide the filter bar
	a.rootFlex.Clear()
	a.rootFlex.AddItem(a.mainFlex, 0, 1, true)
	a.rootFlex.AddItem(a.statusBar, 1, 0, false)

	// Restore focus to the active pane
	if a.activePane == ResourcesPane {
		if a.viewState.viewType == PodView {
			a.SetFocus(a.resourcesView)
		} else {
			a.SetFocus(a.resourcesTable)
		}
	} else {
		a.SetFocus(a.eventsTable)
	}

	a.refreshResourcesView()
	a.refreshEventsTable()
	a.updateStatusBar()
	debugLog("filter mode deactivated (clearFilter=%v, filterText=%q)", clearFilter, a.filterText)
}

func (a *App) setupKeyBindings() {
	// Global key handler — runs before any widget sees the event.
	a.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl+C always quits, regardless of mode.
		if event.Key() == tcell.KeyCtrlC {
			a.Stop()
			return nil
		}

		// ----- Filter mode -----
		if a.filterActive {
			switch event.Key() {
			case tcell.KeyEsc:
				// Exit filter mode and clear filter
				a.deactivateFilter(true)
				return nil
			case tcell.KeyEnter:
				// Exit filter mode but keep filter applied
				a.deactivateFilter(false)
				return nil
			case tcell.KeyTab:
				// Allow pane switching while filtering
				a.switchPane()
				return nil
			}
			// Let all other keys (runes, backspace, etc.) reach the InputField.
			return event
		}

		// ----- Normal mode -----
		switch event.Key() {
		case tcell.KeyTab:
			a.switchPane()
			return nil
		case tcell.KeyEsc:
			a.navigateBack()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				a.Stop()
				return nil
			case '/':
				a.activateFilter()
				return nil
			}
			// Consume all other runes so they don't reach the table's
			// built-in search handler.
			return nil
		}
		return event
	})

	// Resources table handler
	a.resourcesTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			a.navigateInto()
			return nil
		}
		return event
	})

	// Events table handler — no special enter behavior needed.
	a.eventsTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return event
	})
}

func (a *App) Run() error {
	// Set dark theme
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorBlack
	tview.Styles.ContrastBackgroundColor = tcell.ColorBlack
	tview.Styles.BorderColor = tcell.ColorDarkGray

	// Create components
	a.resourcesTable = a.createResourcesTable()
	a.eventsTable = a.createEventsTable()

	// Create Pod YAML view
	a.resourcesView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(false)
	a.resourcesView.SetBorder(true)
	a.resourcesView.SetBorderColor(tcell.ColorYellow)
	a.resourcesView.SetTitle(" [yellow::b]Pod Status[-] ")
	a.resourcesView.SetTitleAlign(tview.AlignLeft)
	a.resourcesView.SetBackgroundColor(tcell.ColorBlack)

	// Create filter input bar (hidden by default, shown when '/' is pressed)
	a.filterInput = tview.NewInputField().
		SetLabel("[yellow]/[-] ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite)
	a.filterInput.SetBackgroundColor(tcell.ColorBlack)
	a.filterInput.SetChangedFunc(func(text string) {
		a.filterText = text
		a.refreshResourcesView()
	})

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	a.statusBar.SetBackgroundColor(tcell.ColorBlack)

	// Setup key bindings
	a.setupKeyBindings()

	// Create layout with two vertical panes (starts with table view)
	a.mainFlex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.resourcesTable, 0, 1, true).
		AddItem(a.eventsTable, 0, 1, false)

	// Create overall layout (no header bar — filter bar appears here when active)
	a.rootFlex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.mainFlex, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

	// Populate tables with initial data
	debugLog("populating initial UI tables")
	a.refreshResourcesView()

	// If we're in Forest view and there are PodCliqueSets, load events for the first one
	if a.viewState.viewType == ForestView && len(a.allResources["forest"]) > 0 {
		firstPCS := a.allResources["forest"][0]
		debugLog("loading events for first PCS: %s/%s", firstPCS.Namespace, firstPCS.Name)
		a.loadEventsForPodCliqueSet(firstPCS.Name, firstPCS.Namespace)
	} else {
		debugLog("no PodCliqueSets found in forest view, skipping initial event load")
	}

	a.refreshEventsTable()

	// Initial status
	a.updateStatusBar()

	// Set root and run
	debugLog("starting tview application event loop")
	return a.SetRoot(a.rootFlex, true).SetFocus(a.resourcesTable).Run()
}

func main() {
	debugFile := flag.String("debug", "", "Path to debug log file (e.g. ~/tmp/arborist.log)")
	flag.Parse()

	if *debugFile != "" {
		// Expand ~ to home directory
		path := *debugFile
		if len(path) > 0 && path[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				path = home + path[1:]
			}
		}
		if err := InitDebugLog(path); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open debug log: %v\n", err)
			os.Exit(1)
		}
		defer CloseDebugLog()
	}

	debugLog("starting arborist")

	// Check for subcommands in positional args
	args := flag.Args()
	if len(args) > 0 && args[0] == "topology" {
		if err := runTopologyCommand(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Recover from panics (e.g. nil pointer in a tview callback) so we can
	// log the stack trace and restore the terminal before exiting.
	defer func() {
		if r := recover(); r != nil {
			stack := rtdebug.Stack()
			debugLog("PANIC: %v\n%s", r, stack)
			CloseDebugLog()
			fmt.Fprintf(os.Stderr, "arborist panic: %v\n%s", r, stack)
			os.Exit(1)
		}
	}()

	app := NewApp()
	if err := app.Run(); err != nil {
		debugLog("app exited with error: %v", err)
		panic(err)
	}
	debugLog("arborist exiting cleanly")
}
