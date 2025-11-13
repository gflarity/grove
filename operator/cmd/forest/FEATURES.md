# Grove TUI Features

## K9s-Style Interface

Both demos now feature a k9s-inspired interface with:

### Visual Design
- **Full Terminal Usage**: No wasted space, uses entire terminal
- **Dark Theme**: Black background like k9s
- **No Table Borders**: Clean, space-efficient display
- **Row Highlighting**: Selected row has darker background
- **Color Coding**: 
  - Green for running/healthy resources
  - Yellow for pending/warning states
  - Red for errors/failures
  - Dim gray for headers and unavailable data

### Layout (Top to Bottom)
1. **Header Bar**: Shows current resource type, namespace, and cluster
2. **Info Bar**: Displays available keyboard shortcuts
3. **Table**: Main content area with resources
4. **Status Bar**: Shows resource counts and selected item

### Simple Demo
Displays pod-like resources with:
- Namespace
- Name
- Ready state (e.g., 1/1, 0/1)
- Status (Running, Pending, CrashLoopBackOff)
- Resource usage (CPU, Memory)
- Node assignment
- Age

### Advanced Demo
Adds multiple views:

**Table View**: Grove-specific resources
- PodCliqueSets
- PodCliques  
- ScalingGroups
- Resource usage and replica counts

**Detail View** (Press Enter or 'd'):
- Full resource information
- Labels and annotations
- Recent events
- Formatted like `kubectl describe`

**Log View** (Press 'l'):
- Streaming logs with timestamps
- Color-coded log levels (INFO, WARN, ERROR)
- Follows k9s log formatting

### Keyboard Shortcuts
- `↑/↓` - Navigate rows
- `Enter` or `d` - Show resource details
- `l` - Show logs
- `q` or `Esc` - Quit/go back
- `?` - Show help (advanced demo)

## Integration Ideas

To integrate with real Grove operator:

1. **Data Source**: Replace mock data with Kubernetes client calls
2. **Resource Types**: Show actual PodCliques, PodCliqueSets
3. **Real-time Updates**: Use watchers to update table
4. **Actions**: Implement delete, scale, edit operations
5. **Filtering**: Add namespace and label selectors
6. **Search**: Add fuzzy search like k9s ('/')

The demos provide a solid foundation that looks and feels like k9s while being customized for Grove-specific resources.