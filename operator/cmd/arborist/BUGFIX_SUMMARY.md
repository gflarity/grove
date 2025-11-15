# Bug Fix Summary - Column Index Errors

## Issues Found and Fixed

### Bug 1: Event Filtering Broken (Line 900)
**Symptom**: No events showed when selecting resources in Forest view

**Root Cause**: 
```go
// WRONG - Getting NAMESPACE (column 0) instead of NAME
selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 0).Text)
```

**Fix**:
```go
// CORRECT - Getting NAME (column 2)
selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
```

### Bug 2: Navigation Broken (Line 1024)
**Symptom**: Both resource box and event box went blank when entering PodCliqueSet view

**Root Cause**:
```go
// WRONG - Getting NAMESPACE (column 0) instead of NAME
selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 0).Text)
selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)
```

This caused `selectedPodCliqueSet` to be set to "production" (namespace) instead of "web-frontend" (name), so the lookup key "PodCliqueSet/production" didn't exist in `allResources`.

**Fix**:
```go
// CORRECT - Getting TYPE (column 1) and NAME (column 2) in correct order
selectedType := strings.TrimSpace(a.resourcesTable.GetCell(row, 1).Text)
selectedName := strings.TrimSpace(a.resourcesTable.GetCell(row, 2).Text)
```

## Root Cause Analysis

When we reordered the resource table columns from:
```
NAME | TYPE | READY | STATUS
```

To:
```
NAMESPACE | TYPE | NAME | READY | SCHEDULED
```

We updated:
- ✅ Header definitions
- ✅ Row data array order
- ✅ Column coloring logic

But we **forgot** to update:
- ❌ Event filtering code (line 900)
- ❌ Navigation code (line 1024)

Both were still assuming NAME was in column 0.

## Column Index Reference

For future reference, the current column layout is:

| Index | Column Name | Usage |
|-------|-------------|-------|
| 0 | NAMESPACE | Display only |
| 1 | TYPE | Used for navigation logic |
| 2 | NAME | **Used for selection and filtering** |
| 3 | READY | Display only |
| 4 | SCHEDULED | Display only |

## Impact

**Before Fix**:
- ❌ Selecting PodCliqueSet "web-frontend" would set state to "production" (namespace)
- ❌ Resource lookup for "PodCliqueSet/production" would fail (key doesn't exist)
- ❌ Event filtering would look for Parent: "production" (wouldn't match any events)
- ❌ Result: Blank screens

**After Fix**:
- ✅ Selecting PodCliqueSet "web-frontend" correctly sets state to "web-frontend"
- ✅ Resource lookup for "PodCliqueSet/web-frontend" succeeds
- ✅ Event filtering looks for Parent: "web-frontend" (matches 3 events)
- ✅ Result: Working navigation with proper data display

## Files Changed

- `main.go` (2 lines):
  - Line 900: Fixed event filtering
  - Line 1024: Fixed navigation

## Testing

All navigation paths should now work:
1. ✅ Forest view → Select PodCliqueSet → See child resources
2. ✅ PodCliqueSet view → Select PodClique/ScalingGroup → See details
3. ✅ PodClique view → Select Pod → See Pod YAML
4. ✅ Event filtering at all levels
5. ✅ Back navigation (Esc key)

## Lessons Learned

When changing table column order:
1. Update header definitions
2. Update row data ordering
3. **Update ALL code that reads from table cells by index**
4. Search codebase for `GetCell(row, X)` calls
5. Verify column indices match the new layout
