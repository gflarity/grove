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

package utils

import (
	"testing"
	"time"

	"github.com/ai-dynamo/grove/operator/api/common/constants"
	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetMinAvailableBreachedPCLQInfo(t *testing.T) {
	now := time.Now()
	terminationDelay := 10 * time.Second
	gracePeriod := 5 * time.Minute

	tests := []struct {
		name              string
		pclqs             []grovecorev1alpha1.PodClique
		terminationDelay  time.Duration
		gracePeriod       time.Duration
		since             time.Time
		wantNames         []string
		wantMinWaitFor    time.Duration
	}{
		{
			name: "no breached cliques",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "healthy-pclq",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionFalse,
								LastTransitionTime: metav1.NewTime(now.Add(-5 * time.Minute)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        nil,
			wantMinWaitFor:   0,
		},
		{
			name: "breached but within grace period",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "new-pclq",
						CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute)), // Created 2 minutes ago
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-1 * time.Minute)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod, // 5 minutes
			since:            now,
			wantNames:        nil, // Not returned because still within 5-minute grace period
			wantMinWaitFor:   0,
		},
		{
			name: "breached and past grace period",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "old-pclq",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)), // Created 10 minutes ago
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-5 * time.Second)), // Breached 5 seconds ago
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay, // 10 seconds
			gracePeriod:      gracePeriod,      // 5 minutes
			since:            now,
			wantNames:        []string{"old-pclq"},
			wantMinWaitFor:   5 * time.Second, // 10s delay - 5s elapsed = 5s to wait
		},
		{
			name: "multiple replicas with mixed grace period status",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "new-breached",
						CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute)), // Within grace
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-1 * time.Minute)),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "old-breached",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)), // Past grace
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-3 * time.Second)),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "old-healthy",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)), // Past grace but not breached
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionFalse,
								LastTransitionTime: metav1.NewTime(now.Add(-1 * time.Minute)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        []string{"old-breached"}, // Only the one past grace period
			wantMinWaitFor:   7 * time.Second,          // 10s delay - 3s elapsed = 7s to wait
		},
		{
			name: "zero grace period - immediate consideration",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "new-pclq",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Second)), // Just created
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-5 * time.Second)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      0, // No grace period
			since:            now,
			wantNames:        []string{"new-pclq"}, // Considered immediately
			wantMinWaitFor:   5 * time.Second,
		},
		{
			name: "boundary - exactly at grace period expiration",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "boundary-pclq",
						CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)), // Exactly 5 minutes ago
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Second)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        []string{"boundary-pclq"}, // At exactly grace period, should be considered
			wantMinWaitFor:   8 * time.Second,
		},
		{
			name: "no MinAvailableBreached condition",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "no-condition",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        nil,
			wantMinWaitFor:   0,
		},
		{
			name: "termination delay already expired",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "expired-delay",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Second)), // More than delay
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        []string{"expired-delay"},
			wantMinWaitFor:   -20 * time.Second, // Negative means already expired
		},
		{
			name: "multiple breached past grace period - returns minimum wait time",
			pclqs: []grovecorev1alpha1.PodClique{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pclq-1",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-8 * time.Second)),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pclq-2",
						CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
					},
					Status: grovecorev1alpha1.PodCliqueStatus{
						Conditions: []metav1.Condition{
							{
								Type:               constants.ConditionTypeMinAvailableBreached,
								Status:             metav1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(now.Add(-3 * time.Second)),
							},
						},
					},
				},
			},
			terminationDelay: terminationDelay,
			gracePeriod:      gracePeriod,
			since:            now,
			wantNames:        []string{"pclq-1", "pclq-2"},
			wantMinWaitFor:   2 * time.Second, // Minimum of (10-8=2s, 10-3=7s) = 2s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNames, gotMinWaitFor := GetMinAvailableBreachedPCLQInfo(
				tt.pclqs,
				tt.terminationDelay,
				tt.gracePeriod,
				tt.since,
			)

			assert.ElementsMatch(t, tt.wantNames, gotNames, "returned clique names mismatch")
			assert.Equal(t, tt.wantMinWaitFor, gotMinWaitFor, "minimum wait time mismatch")
		})
	}
}
