package k8s

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// splitReplicaKey
// ---------------------------------------------------------------------------

func TestSplitReplicaKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want []string
	}{
		{name: "normal key", key: "pcs-name/0", want: []string{"pcs-name", "0"}},
		{name: "hyphenated pcs name", key: "my-cool-pcs/12", want: []string{"my-cool-pcs", "12"}},
		{name: "no slash", key: "pcs-only", want: []string{"pcs-only"}},
		{name: "multiple slashes picks last", key: "ns/pcs-name/0", want: []string{"ns/pcs-name", "0"}},
		{name: "trailing slash", key: "pcs-name/", want: []string{"pcs-name", ""}},
		{name: "leading slash", key: "/0", want: []string{"", "0"}},
		{name: "empty string", key: "", want: []string{""}},
		{name: "slash only", key: "/", want: []string{"", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitReplicaKey(tt.key)
			if len(got) != len(tt.want) {
				t.Fatalf("splitReplicaKey(%q) = %v (len %d), want %v (len %d)", tt.key, got, len(got), tt.want, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("splitReplicaKey(%q)[%d] = %q, want %q", tt.key, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// containsString
// ---------------------------------------------------------------------------

func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  bool
	}{
		{name: "found", slice: []string{"a", "b", "c"}, s: "b", want: true},
		{name: "not found", slice: []string{"a", "b", "c"}, s: "d", want: false},
		{name: "empty slice", slice: []string{}, s: "a", want: false},
		{name: "nil slice", slice: nil, s: "a", want: false},
		{name: "empty string in slice", slice: []string{"", "a"}, s: "", want: true},
		{name: "single element match", slice: []string{"x"}, s: "x", want: true},
		{name: "single element no match", slice: []string{"x"}, s: "y", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.slice, tt.s)
			if got != tt.want {
				t.Errorf("containsString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseGPURequests (unstructured pod object)
// ---------------------------------------------------------------------------

func TestParseGPURequests(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]interface{}
		want int64
	}{
		{
			name: "no containers",
			obj:  map[string]interface{}{},
			want: 0,
		},
		{
			name: "containers without GPU requests",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "single container with string GPU requests",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": "4",
								},
							},
						},
					},
				},
			},
			want: 4,
		},
		{
			name: "single container with int64 GPU requests",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": int64(8),
								},
							},
						},
					},
				},
			},
			want: 8,
		},
		{
			name: "single container with float64 GPU requests",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": float64(2),
								},
							},
						},
					},
				},
			},
			want: 2,
		},
		{
			name: "multiple containers sum GPUs",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": "2",
								},
							},
						},
						map[string]interface{}{
							"name": "sidecar",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": "1",
								},
							},
						},
					},
				},
			},
			want: 3,
		},
		{
			name: "container with non-numeric GPU string",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": "abc",
								},
							},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "zero GPU requests",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "main",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"nvidia.com/gpu": "0",
								},
							},
						},
					},
				},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGPURequests(tt.obj)
			if got != tt.want {
				t.Errorf("parseGPURequests() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseNodeGPUCapacity (unstructured node object)
// ---------------------------------------------------------------------------

func TestParseNodeGPUCapacity(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]interface{}
		want int64
	}{
		{
			name: "no status",
			obj:  map[string]interface{}{},
			want: 0,
		},
		{
			name: "no allocatable",
			obj: map[string]interface{}{
				"status": map[string]interface{}{},
			},
			want: 0,
		},
		{
			name: "no GPU in allocatable",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"allocatable": map[string]interface{}{
						"cpu": "32",
					},
				},
			},
			want: 0,
		},
		{
			name: "GPU as string",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"allocatable": map[string]interface{}{
						"nvidia.com/gpu": "8",
					},
				},
			},
			want: 8,
		},
		{
			name: "GPU as int64",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"allocatable": map[string]interface{}{
						"nvidia.com/gpu": int64(4),
					},
				},
			},
			want: 4,
		},
		{
			name: "GPU as float64",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"allocatable": map[string]interface{}{
						"nvidia.com/gpu": float64(16),
					},
				},
			},
			want: 16,
		},
		{
			name: "GPU as non-numeric string",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"allocatable": map[string]interface{}{
						"nvidia.com/gpu": "abc",
					},
				},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNodeGPUCapacity(tt.obj)
			if got != tt.want {
				t.Errorf("parseNodeGPUCapacity() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// convertK8sEventToEvent
// ---------------------------------------------------------------------------

func TestConvertK8sEventToEvent(t *testing.T) {
	now := time.Now()

	t.Run("event with LastTimestamp", func(t *testing.T) {
		k8sEvent := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "my-event"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "my-pod",
			},
			Type:          "Warning",
			Reason:        "Failed",
			Message:       "container crashed",
			Source:        corev1.EventSource{Component: "kubelet"},
			LastTimestamp:  metav1.NewTime(now.Add(-5 * time.Minute)),
		}

		evt := convertK8sEventToEvent(k8sEvent)

		if evt.Type != "Warning" {
			t.Errorf("Type = %q, want %q", evt.Type, "Warning")
		}
		if evt.Kind != "Pod" {
			t.Errorf("Kind = %q, want %q", evt.Kind, "Pod")
		}
		if evt.Reason != "Failed" {
			t.Errorf("Reason = %q, want %q", evt.Reason, "Failed")
		}
		if evt.Message != "container crashed" {
			t.Errorf("Message = %q, want %q", evt.Message, "container crashed")
		}
		if evt.From != "kubelet" {
			t.Errorf("From = %q, want %q", evt.From, "kubelet")
		}
		if evt.Parent != "my-pod" {
			t.Errorf("Parent = %q, want %q", evt.Parent, "my-pod")
		}
		if evt.Age != "5m" {
			t.Errorf("Age = %q, want %q", evt.Age, "5m")
		}
	})

	t.Run("event with EventTime only", func(t *testing.T) {
		k8sEvent := corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Kind: "PodClique",
				Name: "my-pc",
			},
			Type:      "Normal",
			Reason:    "Created",
			Message:   "created pod",
			Source:    corev1.EventSource{Component: "controller"},
			EventTime: metav1.NewMicroTime(now.Add(-2 * time.Hour)),
		}

		evt := convertK8sEventToEvent(k8sEvent)

		if evt.Age != "2h" {
			t.Errorf("Age = %q, want %q", evt.Age, "2h")
		}
	})

	t.Run("event with both timestamps prefers LastTimestamp", func(t *testing.T) {
		k8sEvent := corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "my-pod",
			},
			Type:          "Normal",
			Reason:        "Scheduled",
			LastTimestamp:  metav1.NewTime(now.Add(-30 * time.Second)),
			EventTime:     metav1.NewMicroTime(now.Add(-2 * time.Hour)),
		}

		evt := convertK8sEventToEvent(k8sEvent)

		if evt.Age != "30s" {
			t.Errorf("Age = %q, want %q", evt.Age, "30s")
		}
	})

	t.Run("event with zero timestamps", func(t *testing.T) {
		k8sEvent := corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "my-pod",
			},
			Type:   "Normal",
			Reason: "Unknown",
		}

		evt := convertK8sEventToEvent(k8sEvent)

		if evt.Age != "unknown" {
			t.Errorf("Age = %q, want %q", evt.Age, "unknown")
		}
	})
}

// NOTE: FormatAge tests live in data/cache_test.go (data.FormatAge is the canonical implementation).
