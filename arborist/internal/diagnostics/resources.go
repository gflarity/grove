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

package diagnostics

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// CollectGroveResources lists all Grove CRDs via the dynamic client and dumps them as YAML.
func CollectGroveResources(ctx context.Context, dc *DiagnosticContext, output DiagnosticOutput) error {
	if dc.DynamicClient == nil {
		return fmt.Errorf("dynamic client is nil, cannot list Grove resources")
	}

	if err := output.WriteSection("GROVE RESOURCES"); err != nil {
		return err
	}

	for _, rt := range GroveResourceTypes {
		_ = output.WriteLinef("[INFO] Listing %s in namespace %s...", rt.Name, dc.Namespace)
		resources, err := dc.DynamicClient.Resource(rt.GVR).Namespace(dc.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			_ = output.WriteLinef("[ERROR] Failed to list %s: %v", rt.Name, err)
			continue
		}

		if len(resources.Items) == 0 {
			_ = output.WriteLinef("[INFO] No %s found in namespace %s", rt.Name, dc.Namespace)
			continue
		}

		_ = output.WriteLinef("[INFO] Found %d %s", len(resources.Items), rt.Name)
		for _, resource := range resources.Items {
			yamlBytes, err := yaml.Marshal(resource.Object)
			if err != nil {
				_ = output.WriteLinef("[ERROR] Failed to marshal %s %s: %v", rt.Singular, resource.GetName(), err)
				continue
			}

			if err := output.WriteYAML(rt.Filename, resource.GetName(), yamlBytes); err != nil {
				_ = output.WriteLinef("[ERROR] Failed to write YAML for %s %s: %v", rt.Singular, resource.GetName(), err)
			}
		}
	}

	return nil
}
