package main

import (
	"fmt"
	"strings"

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
	PodCliqueScalingGroupView
	PodCliqueView
	PodView
)

// ViewState tracks the current navigation state
type ViewState struct {
	viewType            ViewType
	selectedPodCliqueSet string
	selectedScalingGroup string
	selectedPodClique    string
	selectedPod          string
}

// Resource represents a generic resource item
type Resource struct {
	Name         string
	Type         string
	Ready        string
	Status       string
	Namespace    string
	ParentType   string
	ParentName   string
	YAML         string  // For Pod detail view
}

// Event represents a Kubernetes event
type Event struct {
	Type     string  // Normal, Warning, Error
	Reason   string  // The reason for the event
	Age      string  // How long ago
	From     string  // Component that generated the event
	Message  string  // Detailed message
	Parent   string  // Parent resource name for filtering
}

// App encapsulates the split-pane application
type App struct {
	*tview.Application
	activePane      Pane
	resourcesTable  *tview.Table
	resourcesView   *tview.TextView  // For Pod YAML view
	eventsTable     *tview.Table
	statusBar       *tview.TextView
	mainFlex        *tview.Flex      // Main layout container
	viewState       ViewState
	allResources    map[string][]Resource  // Key is parent identifier
	allEvents       []Event
	podYAMLData     map[string]string  // Pod name -> YAML content
}

func NewApp() *App {
	app := &App{
		Application:  tview.NewApplication(),
		activePane:   ResourcesPane,
		allResources: make(map[string][]Resource),
		podYAMLData:  make(map[string]string),
		viewState: ViewState{
			viewType: ForestView,
		},
	}
	app.initializeFakeData()
	return app
}

// initializeFakeData creates hardcoded test data
func (a *App) initializeFakeData() {
	// Forest view - root level PodCliqueSets
	a.allResources["forest"] = []Resource{
		{Name: "web-frontend", Type: "PodCliqueSet", Ready: "3/3", Status: "3/3", Namespace: "production"},
		{Name: "api-backend", Type: "PodCliqueSet", Ready: "5/5", Status: "5/5", Namespace: "production"},
		{Name: "ml-training", Type: "PodCliqueSet", Ready: "10/10", Status: "10/10", Namespace: "research"},
		{Name: "db-cluster", Type: "PodCliqueSet", Ready: "3/3", Status: "3/3", Namespace: "production"},
	}

	// PodCliqueSet: web-frontend (contains PodCliques and PodCliqueScalingGroups)
	a.allResources["PodCliqueSet/web-frontend"] = []Resource{
		{Name: "web-frontend-primary", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "web-frontend"},
		{Name: "web-frontend-canary", Type: "PodClique", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "web-frontend"},
		{Name: "web-autoscaler", Type: "PodCliqueScalingGroup", Ready: "5/5", Status: "5/5", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "web-frontend"},
	}

	// PodCliqueSet: api-backend
	a.allResources["PodCliqueSet/api-backend"] = []Resource{
		{Name: "api-v1", Type: "PodClique", Ready: "3/3", Status: "3/3", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "api-backend"},
		{Name: "api-v2", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "api-backend"},
		{Name: "api-scaler", Type: "PodCliqueScalingGroup", Ready: "10/10", Status: "10/10", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "api-backend"},
	}

	// PodCliqueSet: ml-training
	a.allResources["PodCliqueSet/ml-training"] = []Resource{
		{Name: "gpu-workers", Type: "PodCliqueScalingGroup", Ready: "10/10", Status: "10/10", Namespace: "research", ParentType: "PodCliqueSet", ParentName: "ml-training"},
		{Name: "coordinator", Type: "PodClique", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodCliqueSet", ParentName: "ml-training"},
	}

	// PodCliqueSet: db-cluster
	a.allResources["PodCliqueSet/db-cluster"] = []Resource{
		{Name: "postgres-primary", Type: "PodClique", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "db-cluster"},
		{Name: "postgres-replicas", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "production", ParentType: "PodCliqueSet", ParentName: "db-cluster"},
	}

	// PodCliqueScalingGroup: web-autoscaler (contains PodCliques)
	a.allResources["PodCliqueScalingGroup/web-autoscaler"] = []Resource{
		{Name: "web-autoscaler-0", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "web-autoscaler"},
		{Name: "web-autoscaler-1", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "web-autoscaler"},
		{Name: "web-autoscaler-2", Type: "PodClique", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "web-autoscaler"},
	}

	// PodCliqueScalingGroup: api-scaler
	a.allResources["PodCliqueScalingGroup/api-scaler"] = []Resource{
		{Name: "api-scaler-0", Type: "PodClique", Ready: "3/3", Status: "3/3", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "api-scaler"},
		{Name: "api-scaler-1", Type: "PodClique", Ready: "3/3", Status: "3/3", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "api-scaler"},
		{Name: "api-scaler-2", Type: "PodClique", Ready: "2/4", Status: "2/4", Namespace: "production", ParentType: "PodCliqueScalingGroup", ParentName: "api-scaler"},
	}

	// PodCliqueScalingGroup: gpu-workers
	a.allResources["PodCliqueScalingGroup/gpu-workers"] = []Resource{
		{Name: "gpu-workers-0", Type: "PodClique", Ready: "8/8", Status: "8/8", Namespace: "research", ParentType: "PodCliqueScalingGroup", ParentName: "gpu-workers"},
		{Name: "gpu-workers-1", Type: "PodClique", Ready: "2/2", Status: "2/2", Namespace: "research", ParentType: "PodCliqueScalingGroup", ParentName: "gpu-workers"},
	}

	// PodClique: web-frontend-primary (contains Pods)
	a.allResources["PodClique/web-frontend-primary"] = []Resource{
		{Name: "web-frontend-primary-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-frontend-primary"},
		{Name: "web-frontend-primary-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-frontend-primary"},
	}

	// PodClique: web-frontend-canary
	a.allResources["PodClique/web-frontend-canary"] = []Resource{
		{Name: "web-frontend-canary-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-frontend-canary"},
	}

	// PodClique: api-v1
	a.allResources["PodClique/api-v1"] = []Resource{
		{Name: "api-v1-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-v1"},
		{Name: "api-v1-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-v1"},
		{Name: "api-v1-2", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-v1"},
	}

	// PodClique: coordinator
	a.allResources["PodClique/coordinator"] = []Resource{
		{Name: "coordinator-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "coordinator"},
	}

	// PodClique: api-v2
	a.allResources["PodClique/api-v2"] = []Resource{
		{Name: "api-v2-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-v2"},
		{Name: "api-v2-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-v2"},
	}

	// PodClique: web-autoscaler-0
	a.allResources["PodClique/web-autoscaler-0"] = []Resource{
		{Name: "web-autoscaler-0-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-autoscaler-0"},
		{Name: "web-autoscaler-0-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-autoscaler-0"},
	}

	// PodClique: web-autoscaler-1
	a.allResources["PodClique/web-autoscaler-1"] = []Resource{
		{Name: "web-autoscaler-1-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-autoscaler-1"},
		{Name: "web-autoscaler-1-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-autoscaler-1"},
	}

	// PodClique: web-autoscaler-2
	a.allResources["PodClique/web-autoscaler-2"] = []Resource{
		{Name: "web-autoscaler-2-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "web-autoscaler-2"},
	}

	// PodClique: api-scaler-0
	a.allResources["PodClique/api-scaler-0"] = []Resource{
		{Name: "api-scaler-0-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-0"},
		{Name: "api-scaler-0-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-0"},
		{Name: "api-scaler-0-pod-2", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-0"},
	}

	// PodClique: api-scaler-1
	a.allResources["PodClique/api-scaler-1"] = []Resource{
		{Name: "api-scaler-1-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-1"},
		{Name: "api-scaler-1-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-1"},
		{Name: "api-scaler-1-pod-2", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-1"},
	}

	// PodClique: api-scaler-2
	a.allResources["PodClique/api-scaler-2"] = []Resource{
		{Name: "api-scaler-2-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-2"},
		{Name: "api-scaler-2-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "api-scaler-2"},
	}

	// PodClique: gpu-workers-0
	a.allResources["PodClique/gpu-workers-0"] = []Resource{
		{Name: "gpu-workers-0-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-2", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-3", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-4", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-5", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-6", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
		{Name: "gpu-workers-0-pod-7", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-0"},
	}

	// PodClique: gpu-workers-1
	a.allResources["PodClique/gpu-workers-1"] = []Resource{
		{Name: "gpu-workers-1-pod-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-1"},
		{Name: "gpu-workers-1-pod-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "research", ParentType: "PodClique", ParentName: "gpu-workers-1"},
	}

	// PodClique: postgres-primary
	a.allResources["PodClique/postgres-primary"] = []Resource{
		{Name: "postgres-primary-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "postgres-primary"},
	}

	// PodClique: postgres-replicas
	a.allResources["PodClique/postgres-replicas"] = []Resource{
		{Name: "postgres-replicas-0", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "postgres-replicas"},
		{Name: "postgres-replicas-1", Type: "Pod", Ready: "1/1", Status: "1/1", Namespace: "production", ParentType: "PodClique", ParentName: "postgres-replicas"},
	}

	// Pod YAML data
	a.podYAMLData["web-frontend-primary-0"] = `apiVersion: v1
kind: Pod
metadata:
  name: web-frontend-primary-0
  namespace: production
  labels:
    app: web-frontend
    clique: web-frontend-primary
    pod-index: "0"
spec:
  containers:
  - name: nginx
    image: nginx:1.21
    ports:
    - containerPort: 80
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 200m
        memory: 256Mi
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-11-13T10:00:00Z"
  - type: ContainersReady
    status: "True"
    lastTransitionTime: "2024-11-13T10:00:05Z"
  podIP: 10.244.1.15
  hostIP: 192.168.1.100
  startTime: "2024-11-13T09:59:50Z"
  containerStatuses:
  - name: nginx
    ready: true
    restartCount: 0
    state:
      running:
        startedAt: "2024-11-13T10:00:04Z"`

	a.podYAMLData["web-frontend-primary-1"] = `apiVersion: v1
kind: Pod
metadata:
  name: web-frontend-primary-1
  namespace: production
  labels:
    app: web-frontend
    clique: web-frontend-primary
    pod-index: "1"
spec:
  containers:
  - name: nginx
    image: nginx:1.21
    ports:
    - containerPort: 80
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 10.244.1.16
  hostIP: 192.168.1.100`

	a.podYAMLData["web-frontend-canary-0"] = `apiVersion: v1
kind: Pod
metadata:
  name: web-frontend-canary-0
  namespace: production
  labels:
    app: web-frontend
    clique: web-frontend-canary
    canary: "true"
spec:
  containers:
  - name: nginx
    image: nginx:1.22-alpine
    ports:
    - containerPort: 80
status:
  phase: Running
  podIP: 10.244.1.17`

	a.podYAMLData["api-v1-0"] = `apiVersion: v1
kind: Pod
metadata:
  name: api-v1-0
  namespace: production
  labels:
    app: api
    version: v1
    clique: api-v1
spec:
  containers:
  - name: api
    image: myapi:v1.0
    ports:
    - containerPort: 8080
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 10.244.2.20`

	a.podYAMLData["coordinator-0"] = `apiVersion: v1
kind: Pod
metadata:
  name: coordinator-0
  namespace: research
  labels:
    app: ml-training
    role: coordinator
    clique: coordinator
spec:
  containers:
  - name: coordinator
    image: ml-coordinator:latest
    ports:
    - containerPort: 9000
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
status:
  phase: Running
  podIP: 10.244.3.10`

	// Events
	a.allEvents = []Event{
		// ===== web-frontend PodCliqueSet =====
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podcliqueset-controller", Message: "PodCliqueSet web-frontend created", Parent: "web-frontend"},
		{Type: "Normal", Reason: "ScalingComplete", Age: "5m", From: "podcliqueset-controller", Message: "Scaled from 2 to 3 replicas", Parent: "web-frontend"},
		{Type: "Normal", Reason: "HealthCheckPassed", Age: "1m", From: "podcliqueset-controller", Message: "All health checks passing", Parent: "web-frontend"},
		
		// web-frontend-primary PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "PodClique created", Parent: "web-frontend-primary"},
		{Type: "Normal", Reason: "PodAdded", Age: "10m", From: "podclique-controller", Message: "Successfully added pod web-frontend-primary-0", Parent: "web-frontend-primary"},
		{Type: "Normal", Reason: "PodReady", Age: "10m", From: "podclique-controller", Message: "All pods ready", Parent: "web-frontend-primary"},
		
		// web-frontend-primary-0 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "15m", From: "scheduler", Message: "Successfully assigned to node worker-1", Parent: "web-frontend-primary-0"},
		{Type: "Normal", Reason: "Pulled", Age: "15m", From: "kubelet", Message: "Container image nginx:1.21 pulled", Parent: "web-frontend-primary-0"},
		{Type: "Normal", Reason: "Created", Age: "15m", From: "kubelet", Message: "Created container nginx", Parent: "web-frontend-primary-0"},
		{Type: "Normal", Reason: "Started", Age: "15m", From: "kubelet", Message: "Started container nginx", Parent: "web-frontend-primary-0"},
		
		// web-frontend-primary-1 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "15m", From: "scheduler", Message: "Successfully assigned to node worker-2", Parent: "web-frontend-primary-1"},
		{Type: "Normal", Reason: "Pulled", Age: "15m", From: "kubelet", Message: "Container image nginx:1.21 pulled", Parent: "web-frontend-primary-1"},
		{Type: "Normal", Reason: "Created", Age: "15m", From: "kubelet", Message: "Created container nginx", Parent: "web-frontend-primary-1"},
		{Type: "Normal", Reason: "Started", Age: "15m", From: "kubelet", Message: "Started container nginx", Parent: "web-frontend-primary-1"},
		
		// web-frontend-canary PodClique
		{Type: "Normal", Reason: "Created", Age: "25m", From: "podclique-controller", Message: "Canary PodClique created", Parent: "web-frontend-canary"},
		{Type: "Normal", Reason: "RolloutStarted", Age: "25m", From: "podclique-controller", Message: "Started canary rollout", Parent: "web-frontend-canary"},
		
		// web-frontend-canary-0 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "25m", From: "scheduler", Message: "Successfully assigned to node worker-3", Parent: "web-frontend-canary-0"},
		{Type: "Normal", Reason: "Pulled", Age: "25m", From: "kubelet", Message: "Container image nginx:1.22-alpine pulled", Parent: "web-frontend-canary-0"},
		{Type: "Normal", Reason: "Started", Age: "25m", From: "kubelet", Message: "Started container nginx", Parent: "web-frontend-canary-0"},
		{Type: "Normal", Reason: "ConfigUpdate", Age: "20m", From: "kubelet", Message: "Configuration reloaded", Parent: "web-frontend-canary-0"},
		
		// web-autoscaler ScalingGroup
		{Type: "Normal", Reason: "Created", Age: "1h", From: "scalinggroup-controller", Message: "ScalingGroup created", Parent: "web-autoscaler"},
		{Type: "Normal", Reason: "ScalingDecision", Age: "15m", From: "scalinggroup-controller", Message: "Scaling up based on CPU metrics", Parent: "web-autoscaler"},
		{Type: "Normal", Reason: "PodCliqueAdded", Age: "15m", From: "scalinggroup-controller", Message: "Added PodClique web-autoscaler-2", Parent: "web-autoscaler"},
		
		// web-autoscaler-0 PodClique
		{Type: "Normal", Reason: "Created", Age: "1h", From: "podclique-controller", Message: "PodClique created", Parent: "web-autoscaler-0"},
		{Type: "Normal", Reason: "Ready", Age: "1h", From: "podclique-controller", Message: "All pods ready", Parent: "web-autoscaler-0"},
		
		// web-autoscaler-0 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-1", Parent: "web-autoscaler-0-pod-0"},
		{Type: "Normal", Reason: "Pulled", Age: "1h", From: "kubelet", Message: "Container image nginx:1.21 pulled", Parent: "web-autoscaler-0-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "web-autoscaler-0-pod-0"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-2", Parent: "web-autoscaler-0-pod-1"},
		{Type: "Normal", Reason: "Pulled", Age: "1h", From: "kubelet", Message: "Container image nginx:1.21 pulled", Parent: "web-autoscaler-0-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "web-autoscaler-0-pod-1"},
		
		// web-autoscaler-1 PodClique
		{Type: "Normal", Reason: "Created", Age: "1h", From: "podclique-controller", Message: "PodClique created", Parent: "web-autoscaler-1"},
		{Type: "Warning", Reason: "NetworkError", Age: "45m", From: "kubelet", Message: "Temporary network connectivity issue", Parent: "web-autoscaler-1"},
		{Type: "Normal", Reason: "NetworkRestored", Age: "40m", From: "kubelet", Message: "Network connectivity restored", Parent: "web-autoscaler-1"},
		
		// web-autoscaler-1 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-3", Parent: "web-autoscaler-1-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "web-autoscaler-1-pod-0"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-4", Parent: "web-autoscaler-1-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "web-autoscaler-1-pod-1"},
		
		// web-autoscaler-2 PodClique
		{Type: "Normal", Reason: "Created", Age: "15m", From: "podclique-controller", Message: "PodClique created during scale-up", Parent: "web-autoscaler-2"},
		{Type: "Normal", Reason: "Ready", Age: "14m", From: "podclique-controller", Message: "Pod ready", Parent: "web-autoscaler-2"},
		
		// web-autoscaler-2 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "15m", From: "scheduler", Message: "Successfully assigned to node worker-5", Parent: "web-autoscaler-2-pod-0"},
		{Type: "Normal", Reason: "Pulling", Age: "15m", From: "kubelet", Message: "Pulling container image", Parent: "web-autoscaler-2-pod-0"},
		{Type: "Normal", Reason: "Pulled", Age: "14m", From: "kubelet", Message: "Container image pulled", Parent: "web-autoscaler-2-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "14m", From: "kubelet", Message: "Started container", Parent: "web-autoscaler-2-pod-0"},
		
		// ===== api-backend PodCliqueSet =====
		{Type: "Normal", Reason: "Deployed", Age: "2h", From: "podcliqueset-controller", Message: "API backend deployed successfully", Parent: "api-backend"},
		{Type: "Normal", Reason: "ScalingComplete", Age: "30m", From: "podcliqueset-controller", Message: "API backend scaling completed", Parent: "api-backend"},
		
		// api-v1 PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "API v1 PodClique created", Parent: "api-v1"},
		{Type: "Normal", Reason: "HealthCheck", Age: "15m", From: "podclique-controller", Message: "Health check passed", Parent: "api-v1"},
		{Type: "Warning", Reason: "HighMemory", Age: "5m", From: "podclique-controller", Message: "High memory usage detected", Parent: "api-v1"},
		{Type: "Warning", Reason: "SlowResponse", Age: "20m", From: "podclique-controller", Message: "API response time above threshold", Parent: "api-v1"},
		
		// api-v1 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-1", Parent: "api-v1-0"},
		{Type: "Normal", Reason: "Pulled", Age: "1h", From: "kubelet", Message: "Container image myapi:v1.0 pulled", Parent: "api-v1-0"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "api-v1-0"},
		{Type: "Normal", Reason: "HealthCheckPassed", Age: "15m", From: "kubelet", Message: "Readiness probe succeeded", Parent: "api-v1-0"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-2", Parent: "api-v1-1"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "api-v1-1"},
		{Type: "Warning", Reason: "MemoryPressure", Age: "5m", From: "kubelet", Message: "Container memory usage is high", Parent: "api-v1-1"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-3", Parent: "api-v1-2"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "api-v1-2"},
		
		// api-v2 PodClique
		{Type: "Normal", Reason: "Rollout", Age: "1h", From: "podclique-controller", Message: "Rolling out new version", Parent: "api-v2"},
		{Type: "Normal", Reason: "PodCreated", Age: "1h", From: "podclique-controller", Message: "Created pod api-v2-0", Parent: "api-v2"},
		{Type: "Normal", Reason: "PodCreated", Age: "1h", From: "podclique-controller", Message: "Created pod api-v2-1", Parent: "api-v2"},
		{Type: "Normal", Reason: "RolloutComplete", Age: "55m", From: "podclique-controller", Message: "Rollout completed successfully", Parent: "api-v2"},
		
		// api-v2 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-4", Parent: "api-v2-0"},
		{Type: "Normal", Reason: "Pulled", Age: "1h", From: "kubelet", Message: "Container image myapi:v2.0 pulled", Parent: "api-v2-0"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "api-v2-0"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "1h", From: "scheduler", Message: "Successfully assigned to node worker-5", Parent: "api-v2-1"},
		{Type: "Normal", Reason: "Pulled", Age: "1h", From: "kubelet", Message: "Container image myapi:v2.0 pulled", Parent: "api-v2-1"},
		{Type: "Normal", Reason: "Started", Age: "1h", From: "kubelet", Message: "Started container", Parent: "api-v2-1"},
		
		// api-scaler ScalingGroup
		{Type: "Normal", Reason: "Created", Age: "2h", From: "scalinggroup-controller", Message: "API scaler created", Parent: "api-scaler"},
		{Type: "Normal", Reason: "ScalingStarted", Age: "2m", From: "scalinggroup-controller", Message: "Started scaling operation", Parent: "api-scaler"},
		{Type: "Normal", Reason: "TargetLoad", Age: "2m", From: "scalinggroup-controller", Message: "Target load threshold exceeded", Parent: "api-scaler"},
		
		// api-scaler-0 PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "PodClique created", Parent: "api-scaler-0"},
		{Type: "Normal", Reason: "Ready", Age: "2h", From: "podclique-controller", Message: "All 3 pods ready", Parent: "api-scaler-0"},
		
		// api-scaler-0 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-1", Parent: "api-scaler-0-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-0-pod-0"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-2", Parent: "api-scaler-0-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-0-pod-1"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-3", Parent: "api-scaler-0-pod-2"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-0-pod-2"},
		
		// api-scaler-1 PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "PodClique created", Parent: "api-scaler-1"},
		{Type: "Normal", Reason: "Ready", Age: "2h", From: "podclique-controller", Message: "All 3 pods ready", Parent: "api-scaler-1"},
		
		// api-scaler-1 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-4", Parent: "api-scaler-1-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-1-pod-0"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-5", Parent: "api-scaler-1-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-1-pod-1"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to node worker-6", Parent: "api-scaler-1-pod-2"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Started container", Parent: "api-scaler-1-pod-2"},
		
		// api-scaler-2 PodClique (scaling in progress)
		{Type: "Normal", Reason: "Created", Age: "2m", From: "podclique-controller", Message: "PodClique created during scale-up", Parent: "api-scaler-2"},
		{Type: "Normal", Reason: "Scaling", Age: "2m", From: "podclique-controller", Message: "Scaling to 4 pods", Parent: "api-scaler-2"},
		{Type: "Normal", Reason: "PodScheduled", Age: "1m", From: "scheduler", Message: "Successfully scheduled pod api-scaler-2-0", Parent: "api-scaler-2"},
		{Type: "Normal", Reason: "Pulling", Age: "30s", From: "kubelet", Message: "Pulling image for api-scaler-2-1", Parent: "api-scaler-2"},
		
		// api-scaler-2 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "1m", From: "scheduler", Message: "Successfully assigned to node worker-7", Parent: "api-scaler-2-pod-0"},
		{Type: "Normal", Reason: "Pulling", Age: "1m", From: "kubelet", Message: "Pulling container image", Parent: "api-scaler-2-pod-0"},
		
		{Type: "Normal", Reason: "Scheduled", Age: "30s", From: "scheduler", Message: "Successfully assigned to node worker-8", Parent: "api-scaler-2-pod-1"},
		{Type: "Normal", Reason: "Pulling", Age: "30s", From: "kubelet", Message: "Pulling container image", Parent: "api-scaler-2-pod-1"},
		
		// ===== ml-training PodCliqueSet =====
		{Type: "Normal", Reason: "Deployed", Age: "3h", From: "podcliqueset-controller", Message: "ML training cluster deployed", Parent: "ml-training"},
		{Type: "Normal", Reason: "ResourceQuota", Age: "1h", From: "podcliqueset-controller", Message: "Within quota limits", Parent: "ml-training"},
		{Type: "Normal", Reason: "GPUCheck", Age: "30m", From: "podcliqueset-controller", Message: "All GPU nodes operational", Parent: "ml-training"},
		
		// gpu-workers ScalingGroup
		{Type: "Normal", Reason: "Created", Age: "3h", From: "scalinggroup-controller", Message: "GPU workers scaling group created", Parent: "gpu-workers"},
		{Type: "Normal", Reason: "GPUAllocated", Age: "2h", From: "scalinggroup-controller", Message: "GPU resources allocated", Parent: "gpu-workers"},
		{Type: "Normal", Reason: "AutoscalingEnabled", Age: "2h", From: "scalinggroup-controller", Message: "Autoscaling based on GPU utilization", Parent: "gpu-workers"},
		
		// gpu-workers-0 PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "GPU worker PodClique created", Parent: "gpu-workers-0"},
		{Type: "Normal", Reason: "TrainingStarted", Age: "2h", From: "ml-operator", Message: "ML training job started", Parent: "gpu-workers-0"},
		{Type: "Normal", Reason: "Checkpoint", Age: "30m", From: "ml-operator", Message: "Training checkpoint saved", Parent: "gpu-workers-0"},
		{Type: "Normal", Reason: "Progress", Age: "10m", From: "ml-operator", Message: "Training 75% complete", Parent: "gpu-workers-0"},
		
		// gpu-workers-0 Pods (8 pods)
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-1", Parent: "gpu-workers-0-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-0"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-1", Parent: "gpu-workers-0-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-1"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-2", Parent: "gpu-workers-0-pod-2"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-2"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-2", Parent: "gpu-workers-0-pod-3"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-3"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-3", Parent: "gpu-workers-0-pod-4"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-4"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-3", Parent: "gpu-workers-0-pod-5"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-5"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-4", Parent: "gpu-workers-0-pod-6"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-6"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-4", Parent: "gpu-workers-0-pod-7"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-0-pod-7"},
		
		// gpu-workers-1 PodClique
		{Type: "Normal", Reason: "Created", Age: "2h", From: "podclique-controller", Message: "GPU worker PodClique created", Parent: "gpu-workers-1"},
		{Type: "Normal", Reason: "Ready", Age: "2h", From: "podclique-controller", Message: "All 2 pods ready", Parent: "gpu-workers-1"},
		
		// gpu-workers-1 Pods
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-5", Parent: "gpu-workers-1-pod-0"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-1-pod-0"},
		{Type: "Normal", Reason: "Scheduled", Age: "2h", From: "scheduler", Message: "Successfully assigned to gpu-node-5", Parent: "gpu-workers-1-pod-1"},
		{Type: "Normal", Reason: "Started", Age: "2h", From: "kubelet", Message: "Container started with GPU access", Parent: "gpu-workers-1-pod-1"},
		
		// coordinator PodClique
		{Type: "Normal", Reason: "Created", Age: "3h", From: "podclique-controller", Message: "Coordinator PodClique created", Parent: "coordinator"},
		{Type: "Normal", Reason: "LeaderElected", Age: "3h", From: "ml-operator", Message: "Elected as training coordinator", Parent: "coordinator"},
		{Type: "Normal", Reason: "SyncComplete", Age: "2h", From: "ml-operator", Message: "Synchronized with all workers", Parent: "coordinator"},
		
		// coordinator-0 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "3h", From: "scheduler", Message: "Successfully assigned to master-node-1", Parent: "coordinator-0"},
		{Type: "Normal", Reason: "Pulled", Age: "3h", From: "kubelet", Message: "Container image ml-coordinator:latest pulled", Parent: "coordinator-0"},
		{Type: "Normal", Reason: "Started", Age: "3h", From: "kubelet", Message: "Started container coordinator", Parent: "coordinator-0"},
		{Type: "Normal", Reason: "ConfigLoaded", Age: "3h", From: "kubelet", Message: "Training configuration loaded", Parent: "coordinator-0"},
		
		// ===== db-cluster PodCliqueSet =====
		{Type: "Normal", Reason: "Deployed", Age: "4h", From: "podcliqueset-controller", Message: "Database cluster deployed", Parent: "db-cluster"},
		{Type: "Normal", Reason: "HealthCheck", Age: "10m", From: "podcliqueset-controller", Message: "All database nodes healthy", Parent: "db-cluster"},
		{Type: "Normal", Reason: "BackupComplete", Age: "1h", From: "backup-controller", Message: "Database backup completed successfully", Parent: "db-cluster"},
		{Type: "Normal", Reason: "ReplicationHealthy", Age: "5m", From: "postgres-operator", Message: "Replication lag within acceptable range", Parent: "db-cluster"},
		
		// postgres-primary PodClique
		{Type: "Normal", Reason: "Created", Age: "4h", From: "podclique-controller", Message: "Primary database PodClique created", Parent: "postgres-primary"},
		{Type: "Normal", Reason: "LeaderElected", Age: "4h", From: "postgres-operator", Message: "Elected as primary database node", Parent: "postgres-primary"},
		{Type: "Normal", Reason: "PodReady", Age: "4h", From: "podclique-controller", Message: "Primary database pod is ready", Parent: "postgres-primary"},
		{Type: "Normal", Reason: "ConnectionPool", Age: "3h", From: "postgres-operator", Message: "Connection pool established", Parent: "postgres-primary"},
		
		// postgres-primary-0 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "4h", From: "scheduler", Message: "Successfully assigned to db-node-1", Parent: "postgres-primary-0"},
		{Type: "Normal", Reason: "VolumeAttached", Age: "4h", From: "attachdetach-controller", Message: "Persistent volume attached", Parent: "postgres-primary-0"},
		{Type: "Normal", Reason: "Pulled", Age: "4h", From: "kubelet", Message: "Container image postgres:14 pulled", Parent: "postgres-primary-0"},
		{Type: "Normal", Reason: "Started", Age: "4h", From: "kubelet", Message: "Started postgres container", Parent: "postgres-primary-0"},
		{Type: "Normal", Reason: "DatabaseReady", Age: "4h", From: "postgres-operator", Message: "Database initialized and ready", Parent: "postgres-primary-0"},
		{Type: "Warning", Reason: "DiskPressure", Age: "1h", From: "kubelet", Message: "Database disk usage at 75%", Parent: "postgres-primary-0"},
		
		// postgres-replicas PodClique
		{Type: "Normal", Reason: "Created", Age: "3h", From: "podclique-controller", Message: "Replica PodClique created", Parent: "postgres-replicas"},
		{Type: "Normal", Reason: "ReplicationStarted", Age: "3h", From: "postgres-operator", Message: "Started replication from primary", Parent: "postgres-replicas"},
		{Type: "Normal", Reason: "SyncComplete", Age: "3h", From: "postgres-operator", Message: "Initial sync completed", Parent: "postgres-replicas"},
		{Type: "Normal", Reason: "HealthCheck", Age: "5m", From: "postgres-operator", Message: "Replication health check passed", Parent: "postgres-replicas"},
		
		// postgres-replicas-0 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "3h", From: "scheduler", Message: "Successfully assigned to db-node-2", Parent: "postgres-replicas-0"},
		{Type: "Normal", Reason: "VolumeAttached", Age: "3h", From: "attachdetach-controller", Message: "Persistent volume attached", Parent: "postgres-replicas-0"},
		{Type: "Normal", Reason: "Pulled", Age: "3h", From: "kubelet", Message: "Container image postgres:14 pulled", Parent: "postgres-replicas-0"},
		{Type: "Normal", Reason: "Started", Age: "3h", From: "kubelet", Message: "Started postgres container", Parent: "postgres-replicas-0"},
		{Type: "Normal", Reason: "ReplicationSync", Age: "3h", From: "postgres-operator", Message: "Replication synchronized", Parent: "postgres-replicas-0"},
		
		// postgres-replicas-1 Pod
		{Type: "Normal", Reason: "Scheduled", Age: "3h", From: "scheduler", Message: "Successfully assigned to db-node-3", Parent: "postgres-replicas-1"},
		{Type: "Normal", Reason: "VolumeAttached", Age: "3h", From: "attachdetach-controller", Message: "Persistent volume attached", Parent: "postgres-replicas-1"},
		{Type: "Normal", Reason: "Pulled", Age: "3h", From: "kubelet", Message: "Container image postgres:14 pulled", Parent: "postgres-replicas-1"},
		{Type: "Normal", Reason: "Started", Age: "3h", From: "kubelet", Message: "Started postgres container", Parent: "postgres-replicas-1"},
		{Type: "Normal", Reason: "ReplicationLag", Age: "5m", From: "postgres-operator", Message: "Replication lag is 2ms", Parent: "postgres-replicas-1"},
	}
}

// getCurrentViewKey returns the key for looking up resources in allResources map
func (a *App) getCurrentViewKey() string {
	switch a.viewState.viewType {
	case ForestView:
		return "forest"
	case PodCliqueSetView:
		return "PodCliqueSet/" + a.viewState.selectedPodCliqueSet
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
	case PodCliqueScalingGroupView:
		return fmt.Sprintf("Forest > %s > [purple]%s[-]", a.viewState.selectedPodCliqueSet, a.viewState.selectedScalingGroup)
	case PodCliqueView:
		parent := a.viewState.selectedPodCliqueSet
		if a.viewState.selectedScalingGroup != "" {
			parent = fmt.Sprintf("%s > %s", a.viewState.selectedPodCliqueSet, a.viewState.selectedScalingGroup)
		}
		return fmt.Sprintf("Forest > %s > [aqua]%s[-]", parent, a.viewState.selectedPodClique)
	case PodView:
		parent := a.viewState.selectedPodCliqueSet
		if a.viewState.selectedScalingGroup != "" {
			parent = fmt.Sprintf("%s > %s", a.viewState.selectedPodCliqueSet, a.viewState.selectedScalingGroup)
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
	
	// Update title with breadcrumb
	if a.activePane == ResourcesPane {
		table.SetTitle(fmt.Sprintf(" [yellow::b]Resources[-] [dimgray]|[-] %s ", a.getViewTitle()))
	} else {
		table.SetTitle(fmt.Sprintf(" [dimgray]Resources |[-] %s ", a.getViewTitle()))
	}
	
	// Headers with expansion settings
	headers := []header{
		{"NAMESPACE", 2, tview.AlignLeft},
		{"TYPE", 2, tview.AlignLeft},
		{"NAME", 3, tview.AlignLeft},
		{"READY", 1, tview.AlignCenter},
		{"SCHEDULED", 2, tview.AlignLeft},
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
		"PodCliqueScalingGroup": tcell.ColorPurple,
		"Pod":                   tcell.ColorLime,
	}

	// Get resources for current view
	viewKey := a.getCurrentViewKey()
	resources, exists := a.allResources[viewKey]
	if !exists {
		resources = []Resource{}
	}

	// Add data rows
	for row, resource := range resources {
		rowData := []string{
			resource.Namespace,
			resource.Type,
			resource.Name,
			resource.Ready,
			resource.Status,
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
			case 3: // STATUS column
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
		
		if a.activePane == ResourcesPane && row > 0 {
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
	
	// In table views, check if there's a selection
	row, _ := a.resourcesTable.GetSelection()
	if row < 1 || row >= a.resourcesTable.GetRowCount() {
		// No selection, filter by current view
		filterKey := ""
		switch a.viewState.viewType {
		case ForestView:
			return a.allEvents // Show all events in Forest view
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
	// Can't navigate if in Pod view
	if a.viewState.viewType == PodView {
		return
	}
	
	row, _ := a.resourcesTable.GetSelection()
	if row < 1 {
		return
	}
	
	selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)
	selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
	
	// Determine the next view based on current view and selected type
	switch selectedType {
	case "PodCliqueSet":
		a.viewState.viewType = PodCliqueSetView
		a.viewState.selectedPodCliqueSet = selectedName
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case "PodCliqueScalingGroup":
		a.viewState.viewType = PodCliqueScalingGroupView
		a.viewState.selectedScalingGroup = selectedName
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case "PodClique":
		a.viewState.viewType = PodCliqueView
		a.viewState.selectedPodClique = selectedName
		a.viewState.selectedPod = ""
	case "Pod":
		// Enter Pod detail view
		a.viewState.viewType = PodView
		a.viewState.selectedPod = selectedName
		a.switchToPodView()
	}
	
	a.refreshResourcesView()
	a.refreshEventsTable()
	a.updateStatusBar()
}

// navigateBack goes up one level in the hierarchy
func (a *App) navigateBack() {
	switch a.viewState.viewType {
	case ForestView:
		// Already at root, nothing to do
		return
	case PodCliqueSetView:
		// Go back to Forest
		a.viewState.viewType = ForestView
		a.viewState.selectedPodCliqueSet = ""
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case PodCliqueScalingGroupView:
		// Go back to PodCliqueSet
		a.viewState.viewType = PodCliqueSetView
		a.viewState.selectedScalingGroup = ""
		a.viewState.selectedPodClique = ""
		a.viewState.selectedPod = ""
	case PodCliqueView:
		// Go back to parent (either PodCliqueSet or PodCliqueScalingGroup)
		if a.viewState.selectedScalingGroup != "" {
			a.viewState.viewType = PodCliqueScalingGroupView
		} else {
			a.viewState.viewType = PodCliqueSetView
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
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> scroll <[white]Esc[dimgray]> back <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Pod Status[-] [dimgray]|[-] Viewing: [white]%s[-] [dimgray]|[-] %s", a.viewState.selectedPod, shortcuts)
		} else {
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate <[white]Esc[dimgray]> back <[white]q[dimgray]> quit"
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
			canDrillDown := true  // All resources can be drilled down now
			canGoBack := a.viewState.viewType != ForestView
			
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if canDrillDown {
				shortcuts += " <[white]Enter[dimgray]> drill down"
			}
			if canGoBack {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			
			text = fmt.Sprintf(" [yellow]Resources[-] [dimgray]|[-] Selected: [white]%s[-] [dimgray](%s)[-] [dimgray]|[-] %s", name, resourceType, shortcuts)
		} else {
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
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
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if a.viewState.viewType != ForestView {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Events[-] [dimgray]|[-] Selected: [white]%s[-] [dimgray]|[-] %s", eventType, shortcuts)
		} else {
			shortcuts := "<[white]Tab[dimgray]> switch pane <[white]↑↓[dimgray]> navigate"
			if a.viewState.viewType != ForestView {
				shortcuts += " <[white]Esc[dimgray]> back"
			}
			shortcuts += " <[white]q[dimgray]> quit"
			text = fmt.Sprintf(" [yellow]Events[-] [dimgray]|[-] %s", shortcuts)
		}
	}
	a.statusBar.SetText(text)
}

func (a *App) setupKeyBindings() {
	// Global key handler
	a.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			a.switchPane()
			return nil
		case tcell.KeyEsc:
			// Go back in hierarchy
			a.navigateBack()
			return nil
		case tcell.KeyCtrlC:
			a.Stop()
			return nil
		case tcell.KeyRune:
			if event.Rune() == 'q' {
				a.Stop()
				return nil
			}
		}
		return event
	})

	// Resources table handler
	a.resourcesTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			// Drill down into selected resource
			a.navigateInto()
			return nil
		}
		return event
	})

	// Events table handler - no special enter behavior needed
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

	// Create header bar
	headerBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	headerBar.SetText(" [yellow::b]🌳 Forest[-] [dimgray]|[-] [green]Grove Operator[-] [dimgray]|[-] [cyan]Hierarchical Resource Viewer[-]")
	headerBar.SetBackgroundColor(tcell.ColorBlack)

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

	// Create overall layout
	rootFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(headerBar, 1, 0, false).
		AddItem(a.mainFlex, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

	// Populate tables with initial data
	a.refreshResourcesView()
	a.refreshEventsTable()

	// Initial status
	a.updateStatusBar()

	// Set root and run
	return a.SetRoot(rootFlex, true).SetFocus(a.resourcesTable).Run()
}

func main() {
	app := NewApp()
	if err := app.Run(); err != nil {
		panic(err)
	}
}
