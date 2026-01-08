// /*
// Copyright 2024 The Grove Authors.
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

package defaulting

import (
	"testing"
	"time"

	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestDefaultPodCliqueSet(t *testing.T) {
	want := grovecorev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "PCS1",
			Namespace: "default",
		},
		Spec: grovecorev1alpha1.PodCliqueSetSpec{
			Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{{
					Name: "test",
					Spec: grovecorev1alpha1.PodCliqueSpec{
						Replicas: 2,
						PodSpec: corev1.PodSpec{
							RestartPolicy:                 corev1.RestartPolicyAlways,
							TerminationGracePeriodSeconds: ptr.To[int64](30),
						},
						ScaleConfig: &grovecorev1alpha1.AutoScalingConfig{
							MinReplicas: ptr.To(int32(2)),
							MaxReplicas: 3,
						},
						MinAvailable: ptr.To[int32](2),
					},
				}},
				PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{},
				HeadlessServiceConfig: &grovecorev1alpha1.HeadlessServiceConfig{
					PublishNotReadyAddresses: true,
				},
				// TerminationDelay is NOT defaulted - if nil, gang termination is disabled
				TerminationDelay: nil,
			},
		},
	}
	input := grovecorev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "PCS1",
		},
		Spec: grovecorev1alpha1.PodCliqueSetSpec{
			Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{{
					Name: "test",
					Spec: grovecorev1alpha1.PodCliqueSpec{
						Replicas: 2,
						ScaleConfig: &grovecorev1alpha1.AutoScalingConfig{
							MinReplicas: ptr.To[int32](2),
							MaxReplicas: 3,
						},
					},
				}},
			},
		},
	}
	defaultPodCliqueSet(&input)
	assert.Equal(t, want, input)
}

func TestTerminationDelayDefaulting(t *testing.T) {
	testCases := []struct {
		name                     string
		inputTerminationDelay    *metav1.Duration
		expectedTerminationDelay *metav1.Duration
	}{
		{
			name:                     "PCS without terminationDelay remains nil after defaulting (gang termination disabled)",
			inputTerminationDelay:    nil,
			expectedTerminationDelay: nil,
		},
		{
			name:                     "PCS with explicit terminationDelay is preserved",
			inputTerminationDelay:    &metav1.Duration{Duration: 2 * time.Hour},
			expectedTerminationDelay: &metav1.Duration{Duration: 2 * time.Hour},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := grovecorev1alpha1.PodCliqueSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pcs",
				},
				Spec: grovecorev1alpha1.PodCliqueSetSpec{
					Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
						Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{{
							Name: "test",
							Spec: grovecorev1alpha1.PodCliqueSpec{
								Replicas: 1,
							},
						}},
						TerminationDelay: tc.inputTerminationDelay,
					},
				},
			}

			defaultPodCliqueSet(&input)

			assert.Equal(t, tc.expectedTerminationDelay, input.Spec.Template.TerminationDelay)
		})
	}
}

func TestPCSGTerminationDelayDefaulting(t *testing.T) {
	input := grovecorev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pcs",
		},
		Spec: grovecorev1alpha1.PodCliqueSetSpec{
			Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{{
					Name: "worker",
					Spec: grovecorev1alpha1.PodCliqueSpec{
						Replicas: 1,
					},
				}},
				PodCliqueScalingGroupConfigs: []grovecorev1alpha1.PodCliqueScalingGroupConfig{
					{
						Name:             "scaling-group",
						CliqueNames:      []string{"worker"},
						Replicas:         ptr.To(int32(1)),
						TerminationDelay: nil, // Not set
					},
				},
				TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
			},
		},
	}

	defaultPodCliqueSet(&input)

	// PCSG terminationDelay should remain nil (not defaulted)
	assert.Nil(t, input.Spec.Template.PodCliqueScalingGroupConfigs[0].TerminationDelay)
}

func TestPCLQTerminationDelayDefaulting(t *testing.T) {
	input := grovecorev1alpha1.PodCliqueSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pcs",
		},
		Spec: grovecorev1alpha1.PodCliqueSetSpec{
			Template: grovecorev1alpha1.PodCliqueSetTemplateSpec{
				Cliques: []*grovecorev1alpha1.PodCliqueTemplateSpec{{
					Name: "worker",
					Spec: grovecorev1alpha1.PodCliqueSpec{
						Replicas: 1,
					},
					TerminationDelay: nil, // Not set
				}},
				TerminationDelay: &metav1.Duration{Duration: 4 * time.Hour},
			},
		},
	}

	defaultPodCliqueSet(&input)

	// PCLQ terminationDelay should remain nil (not defaulted)
	assert.Nil(t, input.Spec.Template.Cliques[0].TerminationDelay)
}
