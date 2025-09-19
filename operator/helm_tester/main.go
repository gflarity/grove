package main

import (
	"log"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

func main() {
	// --- Configuration ---
	// These are the values from your original CLI command.
	releaseName := "grove"
	chartRef := "oci://ghcr.io/nvidia/grove/grove-charts"
	chartVersion := "v0.1.0-alpha.1"
	namespace := "default" // Or any other namespace

	// --- Helm and Kubernetes Setup ---
	log.Println("Setting up Helm and Kubernetes configuration...")
	settings := cli.New()

	// 1. Create a new ActionConfig object.
	// This is the central object that provides configuration to all Helm actions.
	actionConfig := new(action.Configuration)
	// The second argument `namespace` is the namespace to install the chart into.
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
		log.Fatalf("❌ Failed to initialize Helm action configuration: %v", err)
	}

	// Initialize an OCI registry client for pulling charts via OCI.
	regClient, err := registry.NewClient()
	if err != nil {
		log.Fatalf("❌ Failed to create Helm registry client: %v", err)
	}
	actionConfig.RegistryClient = regClient

	// --- Locate and Pull the Chart ---
	log.Printf("Locating and pulling chart %s version %s...", chartRef, chartVersion)

	// 2. Create a new Upgrade action client.
	upgradeClient := action.NewUpgrade(actionConfig)

	// 3. Set the options for the upgrade action.
	//upgradeClient.Namespace = namespace
	//upgradeClient.Install = true // This is the equivalent of the '-i' or '--install' flag
	//upgradeClient.Version = chartVersion

	// 4. Locate the chart. This will download the chart from the OCI registry if it's not already cached.
	upgradeClient.ChartPathOptions.Version = chartVersion
	chartPath, err := upgradeClient.ChartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		log.Fatalf("❌ Failed to locate chart: %v", err)
	}
	log.Printf("✅ Chart located at: %s", chartPath)

	// 5. Load the chart from the located path.
	chart, err := loader.Load(chartPath)
	if err != nil {
		log.Fatalf("❌ Failed to load chart: %v", err)
	}

	// --- Perform the Installation or Upgrade ---
	log.Printf("🚀 Performing upgrade/install for release: %s", releaseName)

	// An empty map for values, as the original command didn't specify any.
	values := make(map[string]interface{})

	// Try an upgrade first with Install=false.
	//upgradeClient.Install = false
	//if rel, err := upgradeClient.Run(releaseName, chart, values); err == nil {
	//	log.Printf("✅ Success! Release '%s' upgraded in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	//	return
	//}

	// If upgrade failed due to missing release, perform install.
	installClient := action.NewInstall(actionConfig)
	installClient.Namespace = namespace
	installClient.ReleaseName = releaseName
	installClient.Version = chartVersion
	if rel, err := installClient.Run(chart, values); err != nil {
		log.Fatalf("❌ Helm install failed: %v", err)
	} else {
		log.Printf("✅ Success! Release '%s' installed in namespace '%s'. Status: %s", rel.Name, rel.Namespace, rel.Info.Status)
	}
}
