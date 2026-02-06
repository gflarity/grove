# Grove Demo Workloads - Quick Start Guide

This directory contains ready-to-run demos showcasing Grove Operator's error visibility features.

## 🚀 Quick Start

**Every YAML file includes quick-start instructions at the top!** Just open any scenario file to see exactly how to run it.

### Try Your First Demo (30 seconds)

```bash
# Run an image error demo
kubectl apply -f errors/group1-image/01-typo-registry.yaml

# Watch what happens
kubectl get pods -l app.kubernetes.io/part-of=image-error-typo-registry -w

# Check the error status
kubectl get pcs image-error-typo-registry -o yaml | grep -A 10 lastErrors
```

## 📁 What's Here?

```
demo-workloads/
├── base/                              # ✅ Working examples to start with
│   ├── simple-pcs.yaml               # Basic 3-pod gang
│   ├── multi-gang-pcs.yaml           # Multiple roles (web, worker, db)
│   ├── pcs-with-pcsg.yaml            # Full hierarchy demo
│   ├── topology-pcs.yaml             # Topology-aware (rack packing)
│   └── topology-multi-level-pcs.yaml # Per-clique topology constraints
│
├── errors/                  # ❌ Error scenarios organized by type
│   ├── group1-image/       # Image pull errors (wrong registry, bad tag, etc.)
│   ├── group2-container/   # Container crashes (exit errors, missing binary, etc.)
│   ├── group3-scheduling/  # Can't schedule (node affinity, anti-affinity, etc.)
│   ├── group4-resources/   # Resource issues (high CPU/memory, quotas, etc.)
│   └── group5-complex/     # Complex scenarios (mixed errors, cascading, etc.)
│
└── setup/                   # 🛠️  Optional setup files
    └── (namespace, quotas, etc.)
```

## 🎯 How to Run Any Demo

1. **Pick a scenario** - Each file is self-contained
2. **Open the YAML file** - Quick-start instructions are at the top
3. **Follow the steps** - Usually just 3-4 commands
4. **See the results** - Error details appear in the PodCliqueSet status

## 📋 Common Patterns

### Running a Basic Demo
```bash
# Apply any scenario
kubectl apply -f errors/<group>/<scenario>.yaml

# Check pod status
kubectl get pods -l app.kubernetes.io/part-of=<resource-name>

# View error details
kubectl get pcs <resource-name> -o yaml | grep -A 10 lastErrors

# Clean up
kubectl delete pcs <resource-name>
```

### Using grovectl (Optional but Better)
```bash
# Get formatted status
grovectl describe pcs <resource-name>

# Analyze errors
grovectl analyze <resource-name>
```

## 🌐 Topology Demos

The `base/` directory includes two topology-aware demos that require a cluster set up via `setup-cluster` (which provides topology labels on nodes, the kai-scheduler, and a topology-enabled Grove operator).

### Single-Level Topology (`base/topology-pcs.yaml`)
- **PCS-level** rack constraint — all pods pack into the same rack
- 4 pods total (2 worker + 2 router), with a PCSG managing workers
- **Minimum 7 worker nodes** with rack/block topology labels

### Multi-Level Topology (`base/topology-multi-level-pcs.yaml`)
- **Per-clique** topology constraints — each clique can target a different topology level
- `worker-rack` (3 pods) packs by rack, `worker-block` (4 pods) packs by block
- Demonstrates independent topology constraints within a single PodCliqueSet
- **Minimum 28 worker nodes** with rack/block topology labels

> **Note:** These demos use `kai-scheduler`, the `registry:5001/nginx:alpine-slim` image (pre-loaded by setup-cluster), and require node affinity/tolerations for the tainted worker nodes. See each YAML file for full quick-start instructions.

## 🔧 Special Setup Required

A few scenarios need extra setup:

### Resource Quota Demo (group4/03-quota-exceeded)
```bash
# First apply the quota
kubectl apply -f errors/group4-resources/setup-quota.yaml

# Then run the demo
kubectl apply -f errors/group4-resources/03-quota-exceeded.yaml
```

### Cordoned Nodes Demo (group3/03-cordoned-nodes) 
```bash
# First cordon your nodes
kubectl cordon <node-name>

# Then run the demo
kubectl apply -f errors/group3-scheduling/03-cordoned-nodes.yaml

# Don't forget to uncordon after!
kubectl uncordon <node-name>
```

## 🎪 What You'll See

Each demo shows different error visibility features:

- **Image Errors**: ImagePullBackOff with registry/tag issues
- **Container Errors**: CrashLoopBackOff with exit codes
- **Scheduling Errors**: Pending pods with gang scheduling blocked  
- **Resource Errors**: Insufficient CPU/memory or quota exceeded
- **Complex Scenarios**: Multiple errors, cascading failures, blocked updates

## 📚 More Information

- **Detailed guide**: See `HOW-TO-RUN.md` for comprehensive instructions
- **Scenario catalog**: See `docs/demo-scenarios-catalog.md` for all scenarios
- **Expected behaviors**: See `docs/demo-scenario-matrix.md` for status field mappings

## 💡 Tips

- Start with `base/` examples to see healthy workloads
- Each YAML file is standalone - no dependencies between demos
- Errors typically appear within 30-60 seconds
- Use `kubectl get events` to see detailed error messages
- The operator aggregates pod errors into the PodCliqueSet status

---

**Remember**: Every YAML file has complete instructions at the top. Just open and follow!
