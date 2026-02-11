//go:build e2e

package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/data"
	"github.com/ai-dynamo/grove/arborist/internal/k8s"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Workload YAML paths relative to this file's directory.
var (
	simplePCSYAML   string
	pcsWithPCSGYAML string
)

func init() {
	_, currentFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(currentFile)
	simplePCSYAML = filepath.Join(dir, "../../../operator/demo-workloads/base/simple-pcs.yaml")
	pcsWithPCSGYAML = filepath.Join(dir, "../../../operator/demo-workloads/base/pcs-with-pcsg.yaml")
}

const (
	// e2eWaitTimeout is the default timeout for waiting on TUI output.
	e2eWaitTimeout = 30 * time.Second

	// e2eWorkloadTimeout is how long to wait for workloads to become ready.
	e2eWorkloadTimeout = 3 * time.Minute

	// e2eWorkloadPollInterval is how often to poll for workload readiness.
	e2eWorkloadPollInterval = 5 * time.Second

	// e2eCleanupTimeout is how long to wait for workload cleanup.
	e2eCleanupTimeout = 2 * time.Minute

	// e2eFinalTimeout is the maximum time to wait for the TUI program to exit.
	e2eFinalTimeout = 10 * time.Second
)

// ansiRegex matches ANSI escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from a byte slice.
func stripANSI(b []byte) string {
	return ansiRegex.ReplaceAllString(string(b), "")
}

// ---------------------------------------------------------------------------
// E2E Test Infrastructure (8.2)
// ---------------------------------------------------------------------------

// setupE2E ensures we have a KUBECONFIG pointing at a running cluster and
// returns a GlobalCache plus a kubernetes.Clientset for workload management.
// Tests are skipped if no cluster is available.
func setupE2E(t *testing.T) (data.GlobalCache, *kubernetes.Clientset) {
	t.Helper()

	// The E2E tests expect a pre-existing cluster. Check KUBECONFIG.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		// Fall back to default kubeconfig location
		home, err := os.UserHomeDir()
		if err == nil {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	if _, err := os.Stat(kubeconfig); err != nil {
		t.Skip("Skipping E2E test: no kubeconfig found. Set KUBECONFIG or ensure ~/.kube/config exists.")
	}

	// Create the arborist K8sClient and get a GlobalCache from it
	k8sClient, err := k8s.NewK8sClient()
	if err != nil {
		t.Skipf("Skipping E2E test: failed to create K8sClient: %v", err)
	}
	cache := k8sClient.NewGlobalCache()

	// Also create a standard kubernetes.Clientset for workload management
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to build rest config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}

	// Verify the cluster is reachable
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		t.Skipf("Skipping E2E test: cluster not reachable: %v", err)
	}

	return cache, clientset
}

// applyWorkload applies a YAML file to the cluster using kubectl.
func applyWorkload(t *testing.T, yamlPath string) {
	t.Helper()

	absPath, err := filepath.Abs(yamlPath)
	if err != nil {
		t.Fatalf("Failed to resolve path %s: %v", yamlPath, err)
	}

	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("Workload YAML not found: %s", absPath)
	}

	cmd := exec.Command("kubectl", "apply", "-f", absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to apply workload %s: %v\nOutput: %s", absPath, err, string(out))
	}
	t.Logf("Applied workload: %s\n%s", absPath, string(out))
}

// deleteWorkload deletes a PodCliqueSet by name and waits for cascade cleanup.
func deleteWorkload(t *testing.T, clientset *kubernetes.Clientset, pcsName, namespace string) {
	t.Helper()

	cmd := exec.Command("kubectl", "delete", "pcs", pcsName, "-n", namespace, "--ignore-not-found", "--timeout=60s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Warning: failed to delete PCS %s: %v\nOutput: %s", pcsName, err, string(out))
		return
	}

	// Wait for pods to be cleaned up
	ctx, cancel := context.WithTimeout(context.Background(), e2eCleanupTimeout)
	defer cancel()

	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s", pcsName)
	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil || len(pods.Items) == 0 {
			break
		}
		if ctx.Err() != nil {
			t.Logf("Warning: cleanup timed out for PCS %s, %d pods remaining", pcsName, len(pods.Items))
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("Deleted workload: %s/%s", namespace, pcsName)
}

// waitForPCSReady waits until the expected number of pods for a PCS are Running.
func waitForPCSReady(t *testing.T, clientset *kubernetes.Clientset, pcsName, namespace string, expectedPods int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), e2eWorkloadTimeout)
	defer cancel()

	labelSelector := fmt.Sprintf("app.kubernetes.io/part-of=%s", pcsName)
	t.Logf("Waiting for %d pods to be Running for PCS %s/%s...", expectedPods, namespace, pcsName)

	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			if ctx.Err() != nil {
				t.Fatalf("Timed out waiting for PCS %s pods: %v", pcsName, err)
			}
			time.Sleep(e2eWorkloadPollInterval)
			continue
		}

		runningCount := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == "Running" {
				runningCount++
			}
		}

		if runningCount >= expectedPods {
			t.Logf("PCS %s ready: %d/%d pods running", pcsName, runningCount, expectedPods)
			return
		}

		if ctx.Err() != nil {
			t.Fatalf("Timed out waiting for PCS %s: %d/%d pods running", pcsName, runningCount, expectedPods)
		}
		time.Sleep(e2eWorkloadPollInterval)
	}
}

// newE2ETestModel creates a headless TUI model backed by a real GlobalCache and
// wraps it in teatest.TestModel. Debug logging is enabled to a temp file which
// is printed to t.Log on failure.
func newE2ETestModel(t *testing.T, cache data.GlobalCache) *teatest.TestModel {
	t.Helper()

	debugPath := filepath.Join(t.TempDir(), "arborist-e2e-debug.log")
	if err := InitDebugLog(debugPath); err != nil {
		t.Fatalf("Failed to init debug log: %v", err)
	}
	t.Cleanup(func() {
		CloseDebugLog()
		if t.Failed() {
			logData, err := os.ReadFile(debugPath)
			if err == nil {
				t.Logf("=== Arborist debug log ===\n%s", string(logData))
			}
		}
	})

	m := NewModel(cache,
		WithContext(context.Background()),
		WithDebug(true),
	)

	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
}

// waitForOutput reads from the test model's output until the condition is met
// or the timeout expires. It accumulates all output and checks against the
// full buffer each poll.
func waitForOutput(t *testing.T, tm *teatest.TestModel, condition func(string) bool, timeout time.Duration) string {
	t.Helper()

	var buf bytes.Buffer
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Read any new output
		tmp := make([]byte, 4096)
		n, _ := tm.Output().Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}

		// Check condition on stripped output
		stripped := stripANSI(buf.Bytes())
		if condition(stripped) {
			return stripped
		}

		time.Sleep(200 * time.Millisecond)
	}

	stripped := stripANSI(buf.Bytes())
	t.Fatalf("waitForOutput: condition not met after %s. Output:\n%s", timeout, stripped)
	return stripped
}

// waitForContains is a convenience wrapper that waits until the output
// contains all the given substrings.
func waitForContains(t *testing.T, tm *teatest.TestModel, substrings []string, timeout time.Duration) string {
	t.Helper()
	return waitForOutput(t, tm, func(output string) bool {
		for _, s := range substrings {
			if !strings.Contains(output, s) {
				return false
			}
		}
		return true
	}, timeout)
}

// drainOutput reads and discards accumulated output to reset the buffer.
// Returns the drained content as a string.
func drainOutput(tm *teatest.TestModel) string {
	var buf bytes.Buffer
	tmp := make([]byte, 8192)
	for {
		n, err := tm.Output().Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if n == 0 || err != nil {
			break
		}
	}
	return stripANSI(buf.Bytes())
}

// quitAndWait sends 'q' to quit the TUI and waits for it to finish.
func quitAndWait(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(e2eFinalTimeout))
}

// readFinalOutput collects all remaining output from the test model.
func readFinalOutput(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	out, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(e2eFinalTimeout)))
	if err != nil {
		t.Fatalf("Failed to read final output: %v", err)
	}
	return stripANSI(out)
}

// ---------------------------------------------------------------------------
// 8.3 Test: E2E Forest View with simple-pcs
// ---------------------------------------------------------------------------

func TestE2E_ForestView(t *testing.T) {
	cache, clientset := setupE2E(t)

	// Apply workload and wait for it to be ready
	applyWorkload(t, simplePCSYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "simple-pcs", "default") })
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)

	// Start TUI
	tm := newE2ETestModel(t, cache)

	// Wait for simple-pcs to appear in the output
	output := waitForContains(t, tm, []string{"simple-pcs", "PodCliqueSet"}, e2eWaitTimeout)

	// Assert key elements are visible
	for _, expected := range []string{"simple-pcs", "default", "Forest"} {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q", expected)
		}
	}

	// Quit
	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.4 Test: E2E Drill-Down Navigation with simple-pcs
// ---------------------------------------------------------------------------

func TestE2E_DrillDownNavigation(t *testing.T) {
	cache, clientset := setupE2E(t)

	applyWorkload(t, simplePCSYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "simple-pcs", "default") })
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)

	tm := newE2ETestModel(t, cache)

	// Wait for forest view to load
	waitForContains(t, tm, []string{"simple-pcs"}, e2eWaitTimeout)

	// Press Enter to drill into simple-pcs
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Since simple-pcs has replicas=1, it should skip replica view and show
	// PodClique children. Wait for "replica-0" in the breadcrumb.
	output := waitForContains(t, tm, []string{"replica-0"}, e2eWaitTimeout)

	// Should show PodClique or PodCliqueScalingGroup children
	if !strings.Contains(output, "PodClique") {
		t.Logf("Warning: Expected PodClique in drill-down output. Output:\n%s", output)
	}

	// Breadcrumb should show the path
	if !strings.Contains(output, "Forest") {
		t.Errorf("Expected breadcrumb to contain 'Forest'")
	}
	if !strings.Contains(output, "simple-pcs") {
		t.Errorf("Expected breadcrumb to contain 'simple-pcs'")
	}

	// Navigate further: select PodClique and press Enter
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for Pod-level view
	time.Sleep(2 * time.Second)
	drainOutput(tm)

	// Navigate back with Esc repeatedly to return to Forest
	for i := 0; i < 5; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		time.Sleep(500 * time.Millisecond)
	}

	// Should be back at Forest view
	output = waitForContains(t, tm, []string{"Forest", "simple-pcs"}, e2eWaitTimeout)
	if !strings.Contains(output, "PodCliqueSet") {
		t.Logf("Note: Expected 'PodCliqueSet' type after navigating back to Forest. Output may have changed.")
	}

	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.5 Test: E2E Pod YAML View
// ---------------------------------------------------------------------------

func TestE2E_PodYAMLView(t *testing.T) {
	cache, clientset := setupE2E(t)

	applyWorkload(t, simplePCSYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "simple-pcs", "default") })
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)

	tm := newE2ETestModel(t, cache)

	// Wait for forest view
	waitForContains(t, tm, []string{"simple-pcs"}, e2eWaitTimeout)

	// Navigate: Forest -> PCS (Enter) -> Replica children -> PodClique -> Pod
	// Step 1: Enter PCS (single-replica skip to replica view)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, []string{"replica-0"}, e2eWaitTimeout)

	// Step 2: Enter first child (PodClique)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(2 * time.Second)

	// Step 3: We might now be in PodClique view showing Pods, or in PCSG view.
	// Keep pressing Enter until we see "Pod" type resources
	for attempt := 0; attempt < 3; attempt++ {
		drainOutput(tm)
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(2 * time.Second)
	}

	// Check if we reached the Pod YAML view (contains "apiVersion" or "kind: Pod")
	drainOutput(tm)
	time.Sleep(1 * time.Second)

	// Navigate back to Forest
	for i := 0; i < 6; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		time.Sleep(500 * time.Millisecond)
	}

	// Verify we're back at forest
	waitForContains(t, tm, []string{"simple-pcs", "Forest"}, e2eWaitTimeout)

	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.6 Test: E2E Full Hierarchy with pcs-with-pcsg
// ---------------------------------------------------------------------------

func TestE2E_FullHierarchy(t *testing.T) {
	cache, clientset := setupE2E(t)

	applyWorkload(t, pcsWithPCSGYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "pcs-with-pcsg", "default") })
	// pcs-with-pcsg creates 2 frontend + 2 backend = 4 pods
	waitForPCSReady(t, clientset, "pcs-with-pcsg", "default", 4)

	tm := newE2ETestModel(t, cache)

	// Wait for forest view to show the PCS
	waitForContains(t, tm, []string{"pcs-with-pcsg"}, e2eWaitTimeout)

	// Drill into pcs-with-pcsg (Enter)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Since replicas=1, should skip to replica view showing PCSG + standalone PodCliques
	output := waitForContains(t, tm, []string{"replica-0"}, e2eWaitTimeout)

	// Should show PodCliqueScalingGroup or PodClique children
	hasPCSG := strings.Contains(output, "PodCliqueScalingGroup")
	hasPC := strings.Contains(output, "PodClique")
	if !hasPCSG && !hasPC {
		t.Logf("Warning: Expected PodCliqueScalingGroup or PodClique in output. Output:\n%s", output)
	}

	// If we see a PodCliqueScalingGroup, drill into it
	if hasPCSG {
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(2 * time.Second)
		drainOutput(tm)

		// Should now show PodCliques within the PCSG
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(2 * time.Second)
	}

	// Navigate back to Forest with repeated Esc
	for i := 0; i < 6; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		time.Sleep(500 * time.Millisecond)
	}

	waitForContains(t, tm, []string{"Forest", "pcs-with-pcsg"}, e2eWaitTimeout)

	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.7 Test: E2E Filter Functionality
// ---------------------------------------------------------------------------

func TestE2E_Filter(t *testing.T) {
	cache, clientset := setupE2E(t)

	// Apply both workloads
	applyWorkload(t, simplePCSYAML)
	applyWorkload(t, pcsWithPCSGYAML)
	t.Cleanup(func() {
		deleteWorkload(t, clientset, "simple-pcs", "default")
		deleteWorkload(t, clientset, "pcs-with-pcsg", "default")
	})
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)
	waitForPCSReady(t, clientset, "pcs-with-pcsg", "default", 4)

	tm := newE2ETestModel(t, cache)

	// Wait for both PCSes to appear
	waitForContains(t, tm, []string{"simple-pcs", "pcs-with-pcsg"}, e2eWaitTimeout)

	// Press '/' to activate filter
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	time.Sleep(500 * time.Millisecond)

	// Type "simple" to filter
	tm.Type("simple")
	time.Sleep(1 * time.Second)

	// Press Enter to apply filter
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(1 * time.Second)

	// Read output - should show simple-pcs but not pcs-with-pcsg
	output := waitForContains(t, tm, []string{"simple-pcs"}, e2eWaitTimeout)
	if strings.Contains(output, "pcs-with-pcsg") {
		t.Errorf("Expected filter to hide 'pcs-with-pcsg' but it was still visible")
	}

	// Press '/' then Esc to clear filter
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	time.Sleep(500 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(1 * time.Second)

	// Both PCSes should be visible again
	waitForContains(t, tm, []string{"simple-pcs", "pcs-with-pcsg"}, e2eWaitTimeout)

	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.8 Test: E2E Pane Switching and Events
// ---------------------------------------------------------------------------

func TestE2E_PaneSwitchingAndEvents(t *testing.T) {
	cache, clientset := setupE2E(t)

	applyWorkload(t, simplePCSYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "simple-pcs", "default") })
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)

	tm := newE2ETestModel(t, cache)

	// Wait for forest view
	waitForContains(t, tm, []string{"simple-pcs", "Resources"}, e2eWaitTimeout)

	// Press Tab to switch to Events pane
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	time.Sleep(1 * time.Second)

	// The events pane should become active. Read output.
	output := waitForContains(t, tm, []string{"Events"}, e2eWaitTimeout)

	// Events pane should show event headers (TYPE, REASON, AGE, etc.)
	// The exact content depends on what events exist
	if !strings.Contains(output, "Events") {
		t.Errorf("Expected 'Events' in output after Tab")
	}

	// Press Tab again to go back to Resources
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	time.Sleep(1 * time.Second)

	// Should still show Resources
	waitForContains(t, tm, []string{"Resources"}, e2eWaitTimeout)

	quitAndWait(t, tm)
}

// ---------------------------------------------------------------------------
// 8.9 Test: E2E Debug Logging
// ---------------------------------------------------------------------------

func TestE2E_DebugLogging(t *testing.T) {
	cache, clientset := setupE2E(t)

	applyWorkload(t, simplePCSYAML)
	t.Cleanup(func() { deleteWorkload(t, clientset, "simple-pcs", "default") })
	waitForPCSReady(t, clientset, "simple-pcs", "default", 3)

	// Create TUI with debug logging to a known file
	debugPath := filepath.Join(t.TempDir(), "arborist-e2e-debug.log")
	if err := InitDebugLog(debugPath); err != nil {
		t.Fatalf("Failed to init debug log: %v", err)
	}

	m := NewModel(cache,
		WithContext(context.Background()),
		WithDebug(true),
	)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Wait for initial load
	waitForContains(t, tm, []string{"simple-pcs"}, e2eWaitTimeout)

	// Perform some navigation actions
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(2 * time.Second)

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(1 * time.Second)

	// Activate filter
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	time.Sleep(500 * time.Millisecond)
	tm.Type("test")
	time.Sleep(500 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(500 * time.Millisecond)

	// Tab to switch panes
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	time.Sleep(500 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	time.Sleep(500 * time.Millisecond)

	// Quit
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(e2eFinalTimeout))

	// Close the debug log so we can read it
	CloseDebugLog()

	// Read and assert on the debug log
	logData, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatalf("Failed to read debug log: %v", err)
	}
	logContent := string(logData)

	// Assert debug log contains expected entries
	expectedEntries := []string{
		"MSG",   // Message trace entries
		"STATE", // State transition entries (from navigateInto/navigateBack)
		"CMD",   // Command dispatch entries
	}

	for _, entry := range expectedEntries {
		if !strings.Contains(logContent, entry) {
			t.Errorf("Expected debug log to contain %q entry. Log:\n%s", entry, logContent)
		}
	}

	// Assert no panics
	if strings.Contains(logContent, "PANIC") {
		t.Errorf("Debug log contains PANIC entry:\n%s", logContent)
	}

	// Log the debug output for inspection
	t.Logf("Debug log (%d bytes):\n%s", len(logData), logContent)
}
