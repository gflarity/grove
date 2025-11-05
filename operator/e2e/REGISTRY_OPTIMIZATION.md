# E2E Test Registry Optimization Investigation

**Date**: 2025-11-04  
**Branch**: `keep_registry`  
**Status**: Investigation complete, implementation partially done

## Summary

Investigated keeping the Docker registry container persistent between e2e test runs to speed up image prepulling operations.

## Benchmark Results

Tested registry creation vs reuse with `nginx:alpine-slim` image:

| Scenario | Time | Speedup |
|----------|------|---------|
| **New registry + first push** | 0.463s | Baseline |
| **Push to existing registry (layers already there)** | 0.022s | **~20x faster** |
| **Time saved per image** | **~0.44s** | - |

### Extrapolated Savings

- **1 image** (current): ~0.44s per test run
- **5 images**: ~2.2s per test run  
- **10 images**: ~4.4s per test run
- **Over 100 runs with 10 images**: ~7.3 minutes saved

## Current Implementation Status

### What Was Completed

1. **Registry existence check** - Added `registryExists()` function to detect running registry containers
2. **Manual registry creation** - Added `createRegistry()` to create registry container with k3d-compatible labels
3. **Registry preservation in cleanup** - Modified cleanup to avoid deleting registry
4. **Logging improvements** - Added clear logging to show when registry is being reused
5. **Image push optimization** - Documented that Docker push is already smart about existing layers

### Code Changes Made

- **`e2e/setup/k8s_clusters.go`**:
  - Added `registryExists()` to check for running registry
  - Added `createRegistry()` to manually create registry with k3d labels
  - Modified `ensureRegistryDoesNotExist()` to optionally preserve registry
  - Updated `SetupK3DCluster()` to detect and reuse existing registry

- **`e2e/setup/shared_cluster.go`**:
  - Enhanced `setupRegistryTestImages()` with logging
  - Added comments explaining Docker's smart push behavior

### Challenges Encountered

**k3d Registry Requirements**: k3d expects very specific labels and network configurations on registry containers:

```
Error: "container not managed by k3d: missing default label(s)"
```

k3d validates that registry containers have specific labels:
- `app: k3d`
- `k3d.cluster: <cluster-name>`
- `k3d.role: registry`
- `k3d.version: <version>`
- Plus network connectivity requirements

Our manual registry creation attempted to add these labels but still failed validation, suggesting k3d may check additional container properties (network setup, etc.).

## Recommendations

### Current Situation (1 test image)

**NOT RECOMMENDED to persist registry** because:
- Time savings: Only ~0.44s per test run
- Complexity: High (k3d label/network requirements)
- Risk: Potential for flaky tests if registry state is inconsistent

### Future Situations (5+ test images)

**RECOMMENDED to persist registry** when:
- Using 5+ test images (>2s savings per run)
- Running tests frequently in development
- CI/CD runs tests many times per day

Savings of 2-5 seconds per run ? hundreds of runs = meaningful time savings.

## Next Steps (If Revisiting)

1. **Inspect k3d-created registry labels**:
   ```bash
   # Let k3d create a registry first
   docker inspect k3d-<cluster>-registry --format='{{json .Config.Labels}}' | jq
   docker inspect k3d-<cluster>-registry --format='{{json .NetworkSettings}}' | jq
   ```

2. **Match all k3d expectations**:
   - Exact label structure
   - Network connections (k3d cluster network + bridge)
   - Volume mounts (if any)
   - Environment variables

3. **Alternative approach**: Use completely external registry
   - Run registry outside of k3d's management
   - Configure k3d to use external registry via `registries.yaml`
   - More work but cleaner separation

4. **Test with multiple images**:
   - Add 5-10 test images to validate savings
   - Measure actual test suite time difference

## Benchmark Script

The benchmark script used is saved at `/tmp/benchmark_registry.sh` and can be rerun:

```bash
bash /tmp/benchmark_registry.sh
```

## References

- k3d registry documentation: https://k3d.io/v5.8.3/usage/registries/
- k3d source code: `/home/gflarity/go/pkg/mod/github.com/k3d-io/k3d/v5@v5.8.3/pkg/client/cluster.go`
- Docker push optimization: Layers are smart-pushed (only new/changed layers uploaded)

## Decision

**For now**: Keep the simple approach - let k3d manage registry lifecycle.

**Revisit when**: 
- Test image count grows to 5+
- Test suite runtime becomes a bottleneck
- Team is frequently running e2e tests in development

---

**Note**: The branch `keep_registry` contains the partial implementation if we want to continue this work later.
