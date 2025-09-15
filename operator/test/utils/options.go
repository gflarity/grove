/*
Copyright 2025 The Grove Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package utils provides test utilities for modifying Grove CRD resources.
// It contains option functions that help set up test scenarios by modifying
// resource states, conditions, and metadata.
package utils

import (
	"time"

	grovecorev1alpha1 "github.com/NVIDIA/grove/operator/api/core/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodCliqueScalingGroup test options.
// These functions modify PodCliqueScalingGroup resources for testing scenarios.

// PCSGOption defines a function type that modifies a PodCliqueScalingGroup.
// It follows the functional options pattern for configuring test objects.
type PCSGOption func(*grovecorev1alpha1.PodCliqueScalingGroup)

// WithPCSGMinAvailableBreached returns an option that sets MinAvailableBreached condition
// to True and simulates insufficient available replicas by setting AvailableReplicas
// to one less than MinAvailable.
func WithPCSGMinAvailableBreached() PCSGOption {
	return func(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
		pcsg.Status.Conditions = []metav1.Condition{
			{
				Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
				Status: metav1.ConditionTrue,
				Reason: grovecorev1alpha1.ConditionReasonInsufficientAvailablePCSGReplicas,
			},
		}
		// Set AvailableReplicas to be less than MinAvailable to simulate breach
		minAvailable := pcsg.Spec.Replicas
		if pcsg.Spec.MinAvailable != nil {
			minAvailable = *pcsg.Spec.MinAvailable
		}
		if minAvailable > 0 {
			pcsg.Status.AvailableReplicas = minAvailable - 1
		} else {
			pcsg.Status.AvailableReplicas = 0
		}
	}
}

// WithPCSGUnknownCondition returns an option that sets MinAvailableBreached condition
// to Unknown and sets AvailableReplicas to 0 to simulate an indeterminate state.
func WithPCSGUnknownCondition() PCSGOption {
	return func(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
		pcsg.Status.Conditions = []metav1.Condition{
			{
				Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
				Status: metav1.ConditionUnknown,
				Reason: "UnknownState",
			},
		}
		// For unknown condition, set AvailableReplicas to 0 to simulate unavailable state
		pcsg.Status.AvailableReplicas = 0
	}
}

// WithPCSGObservedGeneration returns an option that sets the ObservedGeneration
// field to the specified value, enabling status mutation testing.
func WithPCSGObservedGeneration(generation int64) PCSGOption {
	return func(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
		pcsg.Status.ObservedGeneration = &generation
	}
}

// PodClique test options.
// These functions modify PodClique resources for testing scenarios.

// PCLQOption defines a function type that modifies a PodClique.
// It follows the functional options pattern for configuring test objects.
type PCLQOption func(*grovecorev1alpha1.PodClique)

// WithPCLQAvailable returns an option that sets the PodClique to a healthy state
// by setting MinAvailableBreached=False and PodCliqueScheduled=True, with ReadyReplicas
// matching the specified replicas.
func WithPCLQAvailable() PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		pclq.Status.Conditions = []metav1.Condition{
			{
				Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
				Status: metav1.ConditionFalse,
				Reason: "SufficientReadyReplicas",
			},
			{
				Type:   grovecorev1alpha1.ConditionTypePodCliqueScheduled,
				Status: metav1.ConditionTrue,
				Reason: "ScheduledSuccessfully",
			},
		}
		pclq.Status.ReadyReplicas = pclq.Spec.Replicas
	}
}

// WithPCLQTerminating returns an option that marks the PodClique for deletion
// by setting a DeletionTimestamp and adding a test finalizer.
func WithPCLQTerminating() PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		now := metav1.NewTime(time.Now())
		pclq.DeletionTimestamp = &now
		pclq.Finalizers = []string{"test-finalizer"}
	}
}

// WithPCLQMinAvailableBreached returns an option that sets MinAvailableBreached=True
// while keeping PodCliqueScheduled=True to simulate a scheduled but unavailable state.
func WithPCLQMinAvailableBreached() PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		pclq.Status.Conditions = []metav1.Condition{
			{
				Type:   grovecorev1alpha1.ConditionTypeMinAvailableBreached,
				Status: metav1.ConditionTrue,
				Reason: "InsufficientReadyReplicas",
			},
			{
				Type:   grovecorev1alpha1.ConditionTypePodCliqueScheduled,
				Status: metav1.ConditionTrue,
				Reason: "ScheduledSuccessfully",
			},
		}
	}
}

// WithPCLQNotScheduled returns an option that sets PodCliqueScheduled=False
// to simulate a scheduling failure state.
func WithPCLQNotScheduled() PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		pclq.Status.Conditions = []metav1.Condition{
			{
				Type:   grovecorev1alpha1.ConditionTypePodCliqueScheduled,
				Status: metav1.ConditionFalse,
				Reason: "SchedulingFailed",
			},
		}
	}
}

// WithPCLQScheduledAndAvailable returns an option that sets both scheduled and
// available conditions to their positive states. This is an alias for WithPCLQAvailable.
func WithPCLQScheduledAndAvailable() PCLQOption {
	return WithPCLQAvailable()
}

// WithPCLQScheduledButBreached returns an option that simulates a scheduled but
// unavailable state. This is an alias for WithPCLQMinAvailableBreached.
func WithPCLQScheduledButBreached() PCLQOption {
	return WithPCLQMinAvailableBreached()
}

// WithPCLQNoConditions returns an option that removes all conditions from
// the PodClique status to simulate a fresh or reset state.
func WithPCLQNoConditions() PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		pclq.Status.Conditions = []metav1.Condition{}
	}
}

// WithPCSGAvailableReplicas returns an option that sets a specific AvailableReplicas count
// for PCSG without modifying any conditions. Useful for testing scaling behavior.
func WithPCSGAvailableReplicas(available int32) PCSGOption {
	return func(pcsg *grovecorev1alpha1.PodCliqueScalingGroup) {
		pcsg.Status.AvailableReplicas = available
	}
}

// WithPCLQReplicaReadyStatus returns an option that sets a specific ReadyReplicas count
// for PodClique without modifying any conditions. Useful for testing availability states.
func WithPCLQReplicaReadyStatus(ready int32) PCLQOption {
	return func(pclq *grovecorev1alpha1.PodClique) {
		pclq.Status.ReadyReplicas = ready
	}
}

// PodGangSet test options.
// These functions modify PodGangSet resources for testing scenarios.

// PGSOption defines a function type that modifies a PodGangSet.
// It follows the functional options pattern for configuring test objects.
type PGSOption func(*grovecorev1alpha1.PodGangSet)
