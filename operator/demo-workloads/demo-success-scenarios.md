# Success Scenarios for Error Visibility Demos

This document defines positive test cases that demonstrate error resolution and successful operations in the Grove Operator.

## Overview

Success scenarios complement error scenarios by showing:
- How errors are resolved
- State transitions from error states to healthy states
- Recovery mechanisms and their visibility
- Successful operations after error conditions

## Success Scenario S1: Image Error Resolution

### Description
Demonstrates recovery from ImagePullBackOff errors by fixing the image reference.

### Steps
1. **Initial State**: Deploy PodCliqueSet with invalid image (e.g., `registr:5001/nginx:latest`)
2. **Observe Error**: 
   - Pods enter ImagePullBackOff state
   - PodClique status shows image pull errors
   - OperationalSummary shows Degraded phase
   - Events show ImagePullBackOff reason
3. **Fix Image**: Update PodCliqueSet spec with correct image (e.g., `nginx:latest`)
4. **Observe Recovery**:
   - Pods successfully pull image
   - Pods transition to Running state
   - Status transitions from Degraded → Healthy
   - Conditions update to reflect healthy state
   - Events show successful image pull

### Expected Status Changes
- **OperationalSummary.Phase**: Degraded → Healthy
- **Conditions**: 
  - `ImagePullError` condition transitions: True → False
  - `Ready` condition transitions: False → True
- **PodGangStatuses**: All pods show Running state
- **LastErrors**: Image pull errors cleared

### CLI Commands to Showcase
```bash
# Before fix
grovectl describe pcs <name>
grovectl analyze <name>

# After fix
grovectl describe pcs <name>  # Show status transition
kubectl get events --field-selector involvedObject.name=<pod-name>
```

### YAML Files
- **Error State**: `demo-workloads/errors/group1-image/01-typo-registry.yaml`
- **Fixed State**: Same file with corrected image reference

## Success Scenario S2: Gang Scheduling Unblocked

### Description
Demonstrates recovery from gang scheduling blockages by resolving scheduling constraints.

### Steps
1. **Initial State**: Deploy PodCliqueSet with impossible scheduling constraints
   - Option A: Node affinity to non-existent label
   - Option B: All nodes cordoned
   - Option C: Resource requests exceed cluster capacity
2. **Observe Blockage**:
   - Pods remain in Pending state
   - Gang scheduling blocked
   - Status shows blocking reason
   - OperationalSummary shows Blocked phase
   - Events show scheduling failures
3. **Resolve Constraints**:
   - Option A: Add required node labels
   - Option B: Uncordon nodes using `CordonNode()` helper
   - Option C: Reduce resource requests or add resources
4. **Observe Recovery**:
   - Pods become schedulable
   - Gang scheduling succeeds
   - Pods transition to Running state
   - Status transitions from Blocked → Healthy
   - Events show successful scheduling

### Expected Status Changes
- **OperationalSummary.Phase**: Blocked → Healthy
- **Conditions**:
  - `SchedulingBlocked` condition transitions: True → False
  - `Ready` condition transitions: False → True
- **PodGangStatuses**: All pods show Running state
- **BlockingReasons**: Cleared from status

### CLI Commands to Showcase
```bash
# Before resolution
grovectl describe pcs <name>
grovectl debug <name>  # Show blocking reasons

# After resolution
grovectl describe pcs <name>  # Show unblocking
kubectl get pods -l <selector>  # Show pod state transitions
```

### YAML Files
- **Blocked State**: `demo-workloads/errors/group3-scheduling/01-node-affinity.yaml` or `03-cordoned-nodes.yaml`
- **Resolved State**: Same file with constraints removed or resolved

## Success Scenario S3: Successful Rolling Update

### Description
Demonstrates a successful rolling update without errors, showing normal update progression.

### Steps
1. **Initial State**: Deploy PodCliqueSet with valid configuration
2. **Trigger Update**: Update PodCliqueSet spec (e.g., change image tag, add environment variable)
3. **Observe Update**:
   - Rolling update starts
   - Update progresses through replicas
   - Old pods terminate gracefully
   - New pods start successfully
   - Status shows update progress
4. **Completion**:
   - All replicas updated
   - RollingUpdateProgress shows completion
   - Status reflects new generation
   - OperationalSummary remains Healthy

### Expected Status Changes
- **RollingUpdateProgress**:
  - `UpdateStartedAt`: Set when update begins
  - `UpdatedReplicas`: Increments as replicas update
  - `UpdateEndedAt`: Set when update completes
- **CurrentGenerationHash**: Updates to reflect new spec
- **UpdatedReplicas**: Matches desired replicas
- **OperationalSummary.Phase**: Remains Healthy throughout

### CLI Commands to Showcase
```bash
# During update
grovectl describe pcs <name>  # Show update progress
kubectl get pods -l <selector> -w  # Watch pod transitions

# After completion
grovectl describe pcs <name>  # Show completion
```

### YAML Files
- **Base Template**: `demo-workloads/base/simple-pcs.yaml`
- **Updated Template**: Same file with modified spec

## Success Scenario S4: Degraded → Healthy Transition

### Description
Demonstrates comprehensive recovery from multiple error conditions, showing full system restoration.

### Steps
1. **Initial State**: Deploy PodCliqueSet with multiple error conditions
   - Some pods with image errors
   - Some pods with container errors
   - Some pods blocked by scheduling constraints
2. **Observe Degraded State**:
   - OperationalSummary shows Degraded phase
   - Multiple error conditions present
   - PodGangStatuses show various error states
   - LastErrors populated with multiple errors
3. **Fix All Errors**:
   - Fix image references
   - Fix container configurations
   - Resolve scheduling constraints
4. **Observe Recovery**:
   - All errors resolve
   - All pods transition to Running
   - Status transitions Degraded → Healthy
   - All conditions become healthy
   - LastErrors cleared
   - Events show recovery

### Expected Status Changes
- **OperationalSummary.Phase**: Degraded → Healthy
- **Conditions**: All error conditions transition to False
- **PodGangStatuses**: All pods show Running state
- **LastErrors**: Cleared
- **AvailableReplicas**: Matches desired replicas

### CLI Commands to Showcase
```bash
# Before recovery
grovectl analyze <name>  # Show all errors
grovectl debug <name>    # Show detailed error information

# During recovery
watch grovectl describe pcs <name>  # Watch status transition

# After recovery
grovectl describe pcs <name>  # Show healthy state
```

### YAML Files
- **Degraded State**: `demo-workloads/errors/group5-complex/01-mixed-errors.yaml`
- **Healthy State**: `demo-workloads/base/simple-pcs.yaml` (or fixed version)

## Success Scenario S5: Container Error Recovery

### Description
Demonstrates recovery from CrashLoopBackOff by fixing container configuration.

### Steps
1. **Initial State**: Deploy PodCliqueSet with container that exits immediately (`command: ["false"]`)
2. **Observe Error**:
   - Pods enter CrashLoopBackOff state
   - Restart count increases
   - Error shows in pod status
   - Gang scheduling may be blocked
3. **Fix Container**: Update PodCliqueSet spec with working command (e.g., `command: ["sleep", "3600"]`)
4. **Observe Recovery**:
   - Pods stop crashing
   - Pods transition to Running state
   - Restart count stabilizes
   - Status transitions to Healthy

### Expected Status Changes
- **OperationalSummary.Phase**: Degraded → Healthy
- **Conditions**:
  - `ContainerError` condition transitions: True → False
  - `Ready` condition transitions: False → True
- **PodGangStatuses**: All pods show Running state

### YAML Files
- **Error State**: `demo-workloads/errors/group2-container/01-exit-error.yaml`
- **Fixed State**: Same file with corrected command

## Success Scenario S6: Resource Constraint Resolution

### Description
Demonstrates recovery from resource constraint errors by adjusting resource requests.

### Steps
1. **Initial State**: Deploy PodCliqueSet with excessive resource requests (e.g., 1000 CPU cores)
2. **Observe Blockage**:
   - Pods remain in Pending state
   - Error shows insufficient resources
   - Gang scheduling blocked
3. **Fix Resources**: Update PodCliqueSet spec with reasonable resource requests
4. **Observe Recovery**:
   - Pods become schedulable
   - Pods transition to Running state
   - Status transitions to Healthy

### Expected Status Changes
- **OperationalSummary.Phase**: Blocked → Healthy
- **Conditions**:
  - `InsufficientResources` condition transitions: True → False
  - `Ready` condition transitions: False → True

### YAML Files
- **Blocked State**: `demo-workloads/errors/group4-resources/01-high-cpu.yaml`
- **Resolved State**: Same file with reduced resource requests

## Common Patterns Across Success Scenarios

### Status Transition Patterns
1. **Error Detection**: Errors appear in LastErrors, Conditions, and PodGangStatuses
2. **Error Resolution**: Errors clear as conditions are fixed
3. **State Recovery**: OperationalSummary phase transitions reflect recovery
4. **Event Flow**: Events show both error and recovery states

### CLI Usage Patterns
1. **Before Fix**: Use `grovectl analyze` and `grovectl debug` to show errors
2. **During Fix**: Use `grovectl describe` to monitor status changes
3. **After Fix**: Use `grovectl describe` to confirm healthy state

### Timing Considerations
- Error states may take 30-60 seconds to stabilize
- Recovery typically occurs within 1-2 minutes after fixes
- Use appropriate timeouts in demo scripts (2-5 minutes)

## Integration with Error Scenarios

Success scenarios should be demonstrated:
1. **After Error Scenarios**: Show recovery from each error type
2. **As Separate Demos**: Standalone demonstrations of recovery
3. **In Interactive Mode**: Allow users to fix errors and observe recovery

## Demo Script Integration

Success scenarios can be integrated into demo scripts as:
- **Follow-up Steps**: After demonstrating error scenarios
- **Separate Scripts**: Standalone success scenario scripts
- **Interactive Mode**: User-driven error resolution demonstrations
