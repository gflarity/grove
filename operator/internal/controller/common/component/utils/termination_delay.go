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
	"time"

	grovecorev1alpha1 "github.com/ai-dynamo/grove/operator/api/core/v1alpha1"
)

// IsGangTerminationEnabled returns true if gang termination is enabled for the PodCliqueSet.
// Gang termination is enabled only if PCS-level terminationDelay is set (not nil).
func IsGangTerminationEnabled(pcs *grovecorev1alpha1.PodCliqueSet) bool {
	return pcs.Spec.Template.TerminationDelay != nil
}

// GetEffectiveTerminationDelayForPCLQ returns the effective termination delay for a PodClique.
// Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
// Resolution order:
// 1. If PCS-level terminationDelay is nil, gang termination is disabled
// 2. If PCLQ is standalone and has terminationDelay set, use it
// 3. Otherwise, use PCS-level terminationDelay
func GetEffectiveTerminationDelayForPCLQ(pcs *grovecorev1alpha1.PodCliqueSet, pclq *grovecorev1alpha1.PodClique) (time.Duration, bool) {
	if pcs.Spec.Template.TerminationDelay == nil {
		return 0, false // Gang termination disabled
	}
	if pclq.Spec.TerminationDelay != nil {
		return pclq.Spec.TerminationDelay.Duration, true
	}
	return pcs.Spec.Template.TerminationDelay.Duration, true
}

// GetEffectiveTerminationDelayForPCSG returns the effective termination delay for a PCSG.
// Returns (duration, true) if gang termination is enabled, (0, false) if disabled.
// Resolution order:
// 1. If PCS-level terminationDelay is nil, gang termination is disabled
// 2. If PCSG has terminationDelay set, use it
// 3. Otherwise, use PCS-level terminationDelay
func GetEffectiveTerminationDelayForPCSG(pcs *grovecorev1alpha1.PodCliqueSet, pcsg *grovecorev1alpha1.PodCliqueScalingGroup) (time.Duration, bool) {
	if pcs.Spec.Template.TerminationDelay == nil {
		return 0, false // Gang termination disabled
	}
	if pcsg.Spec.TerminationDelay != nil {
		return pcsg.Spec.TerminationDelay.Duration, true
	}
	return pcs.Spec.Template.TerminationDelay.Duration, true
}
