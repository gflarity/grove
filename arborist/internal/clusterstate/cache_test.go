// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package clusterstate

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// FormatAge (data package version)
// ---------------------------------------------------------------------------

func TestFormatAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{name: "zero time", t: time.Time{}, want: "unknown"},
		{name: "5 seconds ago", t: now.Add(-5 * time.Second), want: "5s"},
		{name: "59 seconds ago", t: now.Add(-59 * time.Second), want: "59s"},
		{name: "1 minute ago", t: now.Add(-1 * time.Minute), want: "1m"},
		{name: "59 minutes ago", t: now.Add(-59 * time.Minute), want: "59m"},
		{name: "1 hour ago", t: now.Add(-1 * time.Hour), want: "1h"},
		{name: "23 hours ago", t: now.Add(-23 * time.Hour), want: "23h"},
		{name: "1 day ago", t: now.Add(-24 * time.Hour), want: "1d"},
		{name: "7 days ago", t: now.Add(-7 * 24 * time.Hour), want: "7d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAge(tt.t)
			if got != tt.want {
				t.Errorf("FormatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}
