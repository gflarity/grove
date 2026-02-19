package diagnostics

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"max 2", "hello", 2, "he"},
		{"max 1", "hello", 1, "h"},
		{"empty string", "", 5, ""},
		{"max 4", "hello world", 4, "h..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "not ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{},
			},
			want: false,
		},
		{
			name: "other conditions only",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPodReady(tt.pod)
			if got != tt.want {
				t.Errorf("isPodReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPodRow(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{Name: "main"},
				{Name: "sidecar"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "main", Ready: true},
				{Name: "sidecar", Ready: true},
			},
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	row := buildPodRow(pod)

	if len(row) != 5 {
		t.Fatalf("expected 5 columns, got %d", len(row))
	}
	if row[0] != "my-pod" {
		t.Errorf("name: expected 'my-pod', got %q", row[0])
	}
	if row[1] != "Running" {
		t.Errorf("phase: expected 'Running', got %q", row[1])
	}
	if row[2] != "2/2" {
		t.Errorf("ready: expected '2/2', got %q", row[2])
	}
	if row[3] != "node-1" {
		t.Errorf("node: expected 'node-1', got %q", row[3])
	}
	if row[4] != "OK" {
		t.Errorf("conditions: expected 'OK', got %q", row[4])
	}
}

func TestBuildPodRow_Unhealthy(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failing-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "main", Ready: false},
			},
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
			},
		},
	}

	row := buildPodRow(pod)

	if row[1] != "Pending" {
		t.Errorf("phase: expected 'Pending', got %q", row[1])
	}
	if row[2] != "0/1" {
		t.Errorf("ready: expected '0/1', got %q", row[2])
	}
	if row[3] != "<unscheduled>" {
		t.Errorf("node: expected '<unscheduled>', got %q", row[3])
	}
	if row[4] != "PodScheduled:Unschedulable" {
		t.Errorf("conditions: expected 'PodScheduled:Unschedulable', got %q", row[4])
	}
}

func TestBuildPodRow_MultipleFailedConditions(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-pod"},
		Spec: corev1.PodSpec{
			NodeName:   "node-2",
			Containers: []corev1.Container{{Name: "main"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"},
				{Type: corev1.ContainersReady, Status: corev1.ConditionFalse, Reason: "CrashLoopBackOff"},
				// True conditions should be excluded
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
			},
		},
	}

	row := buildPodRow(pod)

	if row[4] != "Ready:ContainersNotReady, ContainersReady:CrashLoopBackOff" {
		t.Errorf("conditions: got %q", row[4])
	}
}

func TestBuildPodRow_LongName(t *testing.T) {
	longName := "this-is-a-very-long-pod-name-that-exceeds-fifty-characters-limit-in-the-table"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: longName},
		Spec: corev1.PodSpec{
			NodeName:   "node-1",
			Containers: []corev1.Container{{Name: "main"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	row := buildPodRow(pod)

	if len(row[0]) > 50 {
		t.Errorf("expected name to be truncated to 50 chars, got %d", len(row[0]))
	}
}

func TestCollectPodDetails_HealthyPods(t *testing.T) {
	cs := kubefake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-pod", Namespace: "test-ns"},
			Spec: corev1.PodSpec{
				NodeName:   "node-1",
				Containers: []corev1.Container{{Name: "main"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "main", Ready: true},
				},
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	dc := &DiagnosticContext{
		Clientset: cs,
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectPodDetails(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.tables != 1 {
		t.Errorf("expected 1 pod table, got %d", out.tables)
	}
	// Healthy pods should not produce subsections
	if len(out.subSections) != 0 {
		t.Errorf("expected 0 subsections for healthy pods, got %d: %v", len(out.subSections), out.subSections)
	}
}

func TestCollectPodDetails_UnhealthyPods(t *testing.T) {
	cs := kubefake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failing-pod", Namespace: "test-ns"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "main"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "main",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "CrashLoopBackOff",
								Message: "back-off 5m",
							},
						},
						RestartCount: 5,
					},
				},
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionFalse},
				},
			},
		},
	)

	dc := &DiagnosticContext{
		Clientset: cs,
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectPodDetails(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.tables != 1 {
		t.Errorf("expected 1 pod table, got %d", out.tables)
	}
	// Unhealthy pods should produce a subsection
	if len(out.subSections) == 0 {
		t.Error("expected subsection for unhealthy pod")
	}
	foundWaiting := false
	foundRestart := false
	for _, line := range out.lines {
		if strings.Contains(line, "CrashLoopBackOff") {
			foundWaiting = true
		}
		if strings.Contains(line, "Restarts=5") {
			foundRestart = true
		}
	}
	if !foundWaiting {
		t.Error("expected waiting reason in output")
	}
	if !foundRestart {
		t.Error("expected restart count in output")
	}
}

func TestCollectPodDetails_NoPods(t *testing.T) {
	cs := kubefake.NewSimpleClientset()

	dc := &DiagnosticContext{
		Clientset: cs,
		Namespace: "empty-ns",
	}
	out := &mockOutput{}

	err := CollectPodDetails(context.Background(), dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.tables != 0 {
		t.Errorf("expected 0 tables for no pods, got %d", out.tables)
	}
	found := false
	for _, line := range out.lines {
		if strings.Contains(line, "No pods found") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'No pods found' message")
	}
}

func TestCollectPodDetails_NilClientset(t *testing.T) {
	dc := &DiagnosticContext{
		Clientset: nil,
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectPodDetails(context.Background(), dc, out)
	if err == nil {
		t.Fatal("expected error for nil clientset")
	}
	if !strings.Contains(err.Error(), "clientset is nil") {
		t.Errorf("expected 'clientset is nil' error, got: %v", err)
	}
}
