# Forest Quick Start

## What is Forest?

Forest is a hierarchical TUI (Terminal User Interface) for the Grove Operator that lets you navigate through PodCliqueSets, PodCliqueScalingGroups, PodCliques, and Pods with drill-down navigation and automatic event filtering.

## Quick Run

```bash
cd /home/gflarity/git/grove_error_ux/operator/cmd/forest

# Run directly
make run

# Or build and run
make build
./forest
```

## Quick Navigation

1. **Start**: See all PodCliqueSets (Forest view)
2. **Press Enter**: Drill into selected PodCliqueSet
3. **See**: PodCliques and PodCliqueScalingGroups
4. **Press Enter**: Drill into a PodClique
5. **See**: Individual Pods
6. **Press Enter**: View Pod YAML + pod-specific events! 🎉
7. **Press Esc**: Go back up one level
8. **Press q**: Quit

## Key Features

- ✅ **Hierarchical Navigation**: Forest → PodCliqueSet → PodClique → Pod
- ✅ **Pod Detail View**: See full Pod YAML and pod-specific events
- ✅ **Event Filtering**: Events filter automatically based on selection
- ✅ **Breadcrumb Navigation**: Always know where you are
- ✅ **Color Coding**: Resource types are color-coded for easy identification

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Drill down into selected resource |
| `Esc` | Go back up one level |
| `Tab` | Switch between top and bottom panes |
| `↑`/`↓` | Navigate/scroll |
| `q` or `Ctrl+C` | Quit |

## Example Session

```
Forest
├─ web-frontend (PodCliqueSet) ← Press Enter
   ├─ web-frontend-primary (PodClique) ← Press Enter
   │  ├─ web-frontend-primary-0 (Pod) ← Press Enter
   │  │  └─ 📄 YAML Status + Events for this pod!
```

## Documentation

- **README.md** - Full feature list and overview
- **USAGE.md** - Comprehensive usage guide with examples
- **IMPLEMENTATION.md** - Technical details and architecture
- **FEATURES.md** - Feature comparison and roadmap

## Prototype Data

Currently uses hardcoded data for:
- 4 PodCliqueSets
- Multiple PodCliques and PodCliqueScalingGroups
- 5 Pods with full YAML
- ~20 events across all resources

## What's New

### Pod View! 🎉

The biggest new feature is **Pod View**:
- Press Enter on ANY pod to see its details
- Top pane shows full Pod YAML (metadata, spec, status)
- Bottom pane shows ONLY events for that specific pod
- Scroll through YAML with arrow keys
- Press Esc to go back

### Automatic Event Filtering

Events now filter intelligently:
- In Forest view → See all events
- Select PodCliqueSet → See events for that set and children
- Select PodClique → See events for that clique and its pods
- **Enter Pod View → See ONLY that pod's events!**

## Next Steps

To connect to a real cluster:
1. Add client-go for K8s API access
2. Replace hardcoded data with live queries
3. Implement watch/informer for real-time updates
4. Add pod logs viewer
5. Add namespace filtering

For now, enjoy exploring the hierarchical navigation prototype! 🌳
