package diagnostics

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestContainerStateString_Running(t *testing.T) {
	startTime := metav1.NewTime(time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC))
	cs := &corev1.ContainerStatus{
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{
				StartedAt: startTime,
			},
		},
	}

	result := containerStateString(cs)

	expected := "Running (started: 10:30:45)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestContainerStateString_Waiting(t *testing.T) {
	cs := &corev1.ContainerStatus{
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{
				Reason:  "CrashLoopBackOff",
				Message: "back-off 5m0s restarting failed container",
			},
		},
	}

	result := containerStateString(cs)

	expected := "Waiting (CrashLoopBackOff: back-off 5m0s restarting failed container)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestContainerStateString_Terminated(t *testing.T) {
	cs := &corev1.ContainerStatus{
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				Reason:   "OOMKilled",
				ExitCode: 137,
			},
		},
	}

	result := containerStateString(cs)

	expected := "Terminated (OOMKilled, exit: 137)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestContainerStateString_Unknown(t *testing.T) {
	cs := &corev1.ContainerStatus{}

	result := containerStateString(cs)

	if result != "Unknown" {
		t.Errorf("expected 'Unknown', got %q", result)
	}
}

func TestCollectOperatorLogs_FindsOperatorPods(t *testing.T) {
	startTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	cs := kubefake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grove-operator-abc123",
				Namespace: "grove-system",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "manager"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "manager",
						Ready:        true,
						RestartCount: 0,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: startTime,
							},
						},
					},
				},
			},
		},
	)

	dc := &DiagnosticContext{
		Clientset:                cs,
		OperatorNamespace:        "grove-system",
		OperatorDeploymentPrefix: "grove-operator",
	}
	out := &mockOutput{}

	err := CollectOperatorLogs(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.subSections) == 0 {
		t.Error("expected subsection for operator pod")
	}
	foundPodName := false
	for _, ss := range out.subSections {
		if strings.Contains(ss, "grove-operator-abc123") {
			foundPodName = true
			break
		}
	}
	if !foundPodName {
		t.Errorf("expected operator pod name in subsections, got: %v", out.subSections)
	}
}

func TestCollectOperatorLogs_NoOperatorPods(t *testing.T) {
	// Create a non-operator pod
	cs := kubefake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-other-pod",
				Namespace: "grove-system",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	dc := &DiagnosticContext{
		Clientset:                cs,
		OperatorNamespace:        "grove-system",
		OperatorDeploymentPrefix: "grove-operator",
	}
	out := &mockOutput{}

	err := CollectOperatorLogs(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, line := range out.lines {
		if strings.Contains(line, "No operator pods found") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'No operator pods found' message")
	}
}

func TestCollectOperatorLogs_WithRestarts(t *testing.T) {
	finishedTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	cs := kubefake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grove-operator-xyz789",
				Namespace: "grove-system",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "manager"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "manager",
						Ready:        true,
						RestartCount: 3,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.NewTime(time.Now()),
							},
						},
						LastTerminationState: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:     "OOMKilled",
								ExitCode:   137,
								FinishedAt: finishedTime,
								Message:    "out of memory",
							},
						},
					},
				},
			},
		},
	)

	dc := &DiagnosticContext{
		Clientset:                cs,
		OperatorNamespace:        "grove-system",
		OperatorDeploymentPrefix: "grove-operator",
	}
	out := &mockOutput{}

	err := CollectOperatorLogs(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should log restart count in subsection header
	foundRestarts := false
	for _, ss := range out.subSections {
		if strings.Contains(ss, "Restarts: 3") {
			foundRestarts = true
			break
		}
	}
	if !foundRestarts {
		t.Errorf("expected restart count in subsection, got: %v", out.subSections)
	}

	// Should log last termination state
	foundOOM := false
	for _, line := range out.lines {
		if strings.Contains(line, "OOMKilled") {
			foundOOM = true
			break
		}
	}
	if !foundOOM {
		t.Error("expected OOMKilled in last termination output")
	}
}

func TestCollectOperatorLogs_NilClientset(t *testing.T) {
	dc := &DiagnosticContext{
		Clientset: nil,
	}
	out := &mockOutput{}

	err := CollectOperatorLogs(context.Background(), dc, out)
	if err == nil {
		t.Fatal("expected error for nil clientset")
	}
	if !strings.Contains(err.Error(), "clientset is nil") {
		t.Errorf("expected 'clientset is nil' error, got: %v", err)
	}
}
