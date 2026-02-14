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

func TestFilterRecentEvents(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)

	events := []corev1.Event{
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "recent-event"},
			LastTimestamp: metav1.Time{Time: now.Add(-5 * time.Minute)},
		},
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "old-event"},
			LastTimestamp: metav1.Time{Time: now.Add(-20 * time.Minute)},
		},
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "boundary-event"},
			LastTimestamp: metav1.Time{Time: cutoff.Add(-1 * time.Second)},
		},
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "just-after-cutoff"},
			LastTimestamp: metav1.Time{Time: cutoff.Add(1 * time.Second)},
		},
	}

	result := filterRecentEvents(events, cutoff)

	if len(result) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(result))
	}
	if result[0].Name != "recent-event" {
		t.Errorf("expected first event to be 'recent-event', got %q", result[0].Name)
	}
	if result[1].Name != "just-after-cutoff" {
		t.Errorf("expected second event to be 'just-after-cutoff', got %q", result[1].Name)
	}
}

func TestFilterRecentEvents_Empty(t *testing.T) {
	result := filterRecentEvents(nil, time.Now())
	if len(result) != 0 {
		t.Fatalf("expected 0 events for nil input, got %d", len(result))
	}
}

func TestFilterRecentEvents_AllOld(t *testing.T) {
	now := time.Now()
	events := []corev1.Event{
		{LastTimestamp: metav1.Time{Time: now.Add(-1 * time.Hour)}},
		{LastTimestamp: metav1.Time{Time: now.Add(-2 * time.Hour)}},
	}

	result := filterRecentEvents(events, now.Add(-30*time.Minute))
	if len(result) != 0 {
		t.Fatalf("expected 0 recent events, got %d", len(result))
	}
}

func TestSortEventsByTime(t *testing.T) {
	now := time.Now()
	events := []corev1.Event{
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "third"},
			LastTimestamp: metav1.Time{Time: now.Add(2 * time.Minute)},
		},
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "first"},
			LastTimestamp: metav1.Time{Time: now.Add(-5 * time.Minute)},
		},
		{
			ObjectMeta:    metav1.ObjectMeta{Name: "second"},
			LastTimestamp: metav1.Time{Time: now},
		},
	}

	sortEventsByTime(events)

	expected := []string{"first", "second", "third"}
	for i, name := range expected {
		if events[i].Name != name {
			t.Errorf("position %d: expected %q, got %q", i, name, events[i].Name)
		}
	}
}

func TestGetEventTime_LastTimestamp(t *testing.T) {
	now := time.Now()
	event := &corev1.Event{
		LastTimestamp: metav1.Time{Time: now},
		EventTime:    metav1.MicroTime{Time: now.Add(-1 * time.Hour)},
	}

	result := getEventTime(event)
	if !result.Equal(now) {
		t.Errorf("expected LastTimestamp %v, got %v", now, result)
	}
}

func TestGetEventTime_FallbackToEventTime(t *testing.T) {
	now := time.Now()
	event := &corev1.Event{
		// LastTimestamp is zero
		EventTime: metav1.MicroTime{Time: now},
	}

	result := getEventTime(event)
	if !result.Equal(now) {
		t.Errorf("expected EventTime %v, got %v", now, result)
	}
}

func TestGetEventTime_BothZero(t *testing.T) {
	event := &corev1.Event{}
	result := getEventTime(event)
	if !result.IsZero() {
		t.Errorf("expected zero time, got %v", result)
	}
}

func TestBuildEventRow(t *testing.T) {
	now := time.Now()
	event := &corev1.Event{
		LastTimestamp: metav1.Time{Time: now},
		Type:         "Warning",
		Reason:       "FailedScheduling",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "my-pod",
		},
		Message: "0/3 nodes available",
	}

	row := buildEventRow(event)

	if len(row) != 5 {
		t.Fatalf("expected 5 columns, got %d", len(row))
	}
	if row[0] != now.Format(time.RFC3339) {
		t.Errorf("time column: expected %q, got %q", now.Format(time.RFC3339), row[0])
	}
	if row[1] != "Warning" {
		t.Errorf("type column: expected 'Warning', got %q", row[1])
	}
	if row[2] != "FailedScheduling" {
		t.Errorf("reason column: expected 'FailedScheduling', got %q", row[2])
	}
	if row[3] != "Pod/my-pod" {
		t.Errorf("object column: expected 'Pod/my-pod', got %q", row[3])
	}
	if row[4] != "0/3 nodes available" {
		t.Errorf("message column: expected '0/3 nodes available', got %q", row[4])
	}
}

func TestBuildEventRow_LongMessage(t *testing.T) {
	longMsg := "This is a very long message that exceeds the 80 character limit and should be truncated by the buildEventRow function"
	event := &corev1.Event{
		LastTimestamp: metav1.Time{Time: time.Now()},
		Type:         "Normal",
		Reason:       "Scheduled",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "test-pod",
		},
		Message: longMsg,
	}

	row := buildEventRow(event)

	if len(row[4]) != 80 {
		t.Errorf("expected message to be truncated to 80 chars, got %d", len(row[4]))
	}
	if row[4][77:] != "..." {
		t.Errorf("expected message to end with '...', got %q", row[4][77:])
	}
}

func TestBuildEventRow_LongReason(t *testing.T) {
	event := &corev1.Event{
		LastTimestamp: metav1.Time{Time: time.Now()},
		Type:         "Normal",
		Reason:       "ThisIsAVeryLongReasonThatExceedsTwentyFiveChars",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "test-pod",
		},
		Message: "test",
	}

	row := buildEventRow(event)

	if len(row[2]) > 25 {
		t.Errorf("expected reason to be truncated to 25 chars, got %d: %q", len(row[2]), row[2])
	}
}

func TestCollectEvents_RecentEvents(t *testing.T) {
	now := time.Now()
	cs := kubefake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:    metav1.ObjectMeta{Name: "recent", Namespace: "test-ns"},
			LastTimestamp: metav1.Time{Time: now.Add(-2 * time.Minute)},
			Type:         "Warning",
			Reason:       "FailedScheduling",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "my-pod"},
			Message:      "0/3 nodes available",
		},
		&corev1.Event{
			ObjectMeta:    metav1.ObjectMeta{Name: "old", Namespace: "test-ns"},
			LastTimestamp: metav1.Time{Time: now.Add(-1 * time.Hour)},
			Type:         "Normal",
			Reason:       "Scheduled",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "other-pod"},
			Message:      "scheduled to node-1",
		},
	)

	dc := &DiagnosticContext{
		Ctx:       context.Background(),
		Clientset: cs,
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectEvents(dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.sections) == 0 {
		t.Fatal("expected at least one section header")
	}
	if out.tables != 1 {
		t.Errorf("expected 1 table, got %d", out.tables)
	}
}

func TestCollectEvents_NoEvents(t *testing.T) {
	cs := kubefake.NewSimpleClientset()

	dc := &DiagnosticContext{
		Ctx:       context.Background(),
		Clientset: cs,
		Namespace: "empty-ns",
	}
	out := &mockOutput{}

	err := CollectEvents(dc, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.tables != 0 {
		t.Errorf("expected 0 tables for no events, got %d", out.tables)
	}
	found := false
	for _, line := range out.lines {
		if strings.Contains(line, "No events found") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'No events found' message")
	}
}

func TestCollectEvents_NilClientset(t *testing.T) {
	dc := &DiagnosticContext{
		Ctx:       context.Background(),
		Clientset: nil,
		Namespace: "test-ns",
	}
	out := &mockOutput{}

	err := CollectEvents(dc, out)
	if err == nil {
		t.Fatal("expected error for nil clientset")
	}
	if !strings.Contains(err.Error(), "clientset is nil") {
		t.Errorf("expected 'clientset is nil' error, got: %v", err)
	}
}
