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

	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsGangTerminationEnabled(t *testing.T) {
	tests := []struct {
		name     string
		pcs      *grovecorev1alpha1.PodCliqueSet
		expected bool
	}{
		{
			name: "PCS terminationDelay nil - gang termination disabled",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: nil,
					},
				},
			},
			expected: false,
		},
		{
			name: "PCS terminationDelay set - gang termination enabled",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
					},
				},
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsGangTerminationEnabled(tc.pcs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetEffectiveTerminationDelayForPCLQ(t *testing.T) {
	tests := []struct {
		name             string
		pcs              *grovecorev1alpha1.PodCliqueSet
		pclq             *grovecorev1alpha1.PodClique
		expectedDuration time.Duration
		expectedEnabled  bool
	}{
		{
			name: "PCS terminationDelay nil - returns (0, false)",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: nil,
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					TerminationDelay: &metav1.Duration{Duration: 1 * time.Hour},
				},
			},
			expectedDuration: 0,
			expectedEnabled:  false,
		},
		{
			name: "PCLQ with own terminationDelay - uses PCLQ value",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					TerminationDelay: &metav1.Duration{Duration: 1 * time.Hour},
				},
			},
			expectedDuration: 1 * time.Hour,
			expectedEnabled:  true,
		},
		{
			name: "PCLQ without terminationDelay - uses PCS value",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
					},
				},
			},
			pclq: &grovecorev1alpha1.PodClique{
				Spec: grovecorev1alpha1.PodCliqueSpec{
					TerminationDelay: nil,
				},
			},
			expectedDuration: 4 * time.Hour,
			expectedEnabled:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			duration, enabled := GetEffectiveTerminationDelayForPCLQ(tc.pcs, tc.pclq)
			assert.Equal(t, tc.expectedDuration, duration)
			assert.Equal(t, tc.expectedEnabled, enabled)
		})
	}
}

func TestGetEffectiveTerminationDelayForPCSG(t *testing.T) {
	tests := []struct {
		name             string
		pcs              *grovecorev1alpha1.PodCliqueSet
		pcsg             *grovecorev1alpha1.PodCliqueScalingGroup
		expectedDuration time.Duration
		expectedEnabled  bool
	}{
		{
			name: "PCS terminationDelay nil - returns (0, false)",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: nil,
					},
				},
			},
			pcsg: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					TerminationDelay: &metav1.Duration{Duration: 2 * time.Hour},
				},
			},
			expectedDuration: 0,
			expectedEnabled:  false,
		},
		{
			name: "PCSG with own terminationDelay - uses PCSG value",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
					},
				},
			},
			pcsg: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					TerminationDelay: &metav1.Duration{Duration: 2 * time.Hour},
				},
			},
			expectedDuration: 2 * time.Hour,
			expectedEnabled:  true,
		},
		{
			name: "PCSG without terminationDelay - uses PCS value",
			pcs: &grovecorev1alpha1.PodCliqueSet{
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
					},
				},
			},
			pcsg: &grovecorev1alpha1.PodCliqueScalingGroup{
				Spec: grovecorev1alpha1.PodCliqueScalingGroupSpec{
					TerminationDelay: nil,
				},
			},
			expectedDuration: 4 * time.Hour,
			expectedEnabled:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			duration, enabled := GetEffectiveTerminationDelayForPCSG(tc.pcs, tc.pcsg)
			assert.Equal(t, tc.expectedDuration, duration)
			assert.Equal(t, tc.expectedEnabled, enabled)
		})
	}
}
