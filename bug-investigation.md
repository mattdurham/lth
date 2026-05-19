# Bug Investigation: Set Nested Blocks with Sensitive Attributes Always Detect Changes

## Problem Summary

In Terraform v1.8.0-rc1, set nested blocks with sensitive attributes are always detected as changed even when there are no actual changes. This is a regression from v1.7.5.

**Symptoms:**
- v1.7.5 stores: `"sensitive_attributes": []` (empty array)
- v1.8.0-rc1 stores: `"sensitive_attributes": [[{"type": "get_attr", "value": "set_nested_block"}]]` (path to the entire set block)
- The path is marked on the entire set block, not on individual sensitive elements
- Every subsequent plan detects spurious changes

## Root Cause Analysis

### The Regression Mechanism

The regression is in how `SensitivePaths` is called and applied for set blocks during planning. The issue occurs in two locations:

1. **File:** `/home/matt/source/terraform/internal/configs/configschema/marks.go` (lines 75-88)
2. **File:** `/home/matt/source/terraform/internal/terraform/node_resource_abstract_instance.go` (lines 1117-1118)

### Detailed Explanation

#### 1. SensitivePaths Logic for Set Blocks (`marks.go:75-88`)

When computing sensitive paths for `NestingSet` blocks:

```go
case NestingSet:
    // For set blocks we cannot mark individual element sub-attributes
    // using cty.IndexPath because set elements are indexed by their
    // value hash. Marking a sub-attribute within a set element changes
    // the element's hash, so cty ends up marking the entire set instead.
    // This produces a path that cannot be stably round-tripped through
    // state serialization, causing a spurious diff on every subsequent
    // plan. Instead, when the set is non-empty we mark the whole set
    // block path, which is consistent with what state serialization
    // actually stores for a set that contains sensitive attributes.
    blockV, _ = blockV.Unmark() // peel off one level of marking so we can check length
    if blockV.IsKnown() && !blockV.IsNull() && blockV.LengthInt() > 0 {
        ret = append(ret, blockPath)
    }
```

**The code is intentionally marking the entire set block path** (not individual elements) when the set contains sensitive attributes and is non-empty. This is correct behavior to avoid hash instability.

#### 2. Double-Marking Problem (`node_resource_abstract_instance.go:1117-1118`)

The issue is in the planning code where sensitive paths are applied:

```go
// Line 1116: Already marked with provider-returned marks
plannedNewVal = plannedNewVal.MarkWithPaths(unmarkedPaths)

// Lines 1117-1118: PROBLEM - SensitivePaths called AGAIN on already-marked value
if sensitivePaths := schema.Body.SensitivePaths(plannedNewVal, nil); len(sensitivePaths) != 0 {
    plannedNewVal = marks.MarkPaths(plannedNewVal, marks.Sensitive, sensitivePaths)
}
```

**The problem:** When `SensitivePaths(plannedNewVal, nil)` is called at line 1117, the value `plannedNewVal` is already marked with the provider's marks (line 1116). For set blocks:

1. The value arrives with marks already applied from the provider response
2. `SensitivePaths` is called again on this marked value
3. When iterating through the set elements to find sensitive sub-attributes, the code encounters the marked set value
4. Since the set has sensitive attributes and is non-empty, it appends the entire `blockPath` to the sensitive paths
5. This entire-set-block path is then stored in state's `sensitive_attributes`

#### 3. Why This Causes a Spurious Diff

When the state is read back:
- The state file contains `"sensitive_attributes": [[{"type": "get_attr", "value": "set_nested_block"}]]`
- This path points to the entire set block
- During planning, `SensitivePaths` is called again on the proposed new value
- Since the set is still non-empty with sensitive elements, the entire set path is returned again
- The marks are applied to the entire set value
- These entire-set-block marks don't match the individual element marks from the provider response
- This triggers a marks inequality check at line 2644 in the apply code, causing an Update action

### Comparison with v1.7.5

In v1.7.5, the code likely didn't call `SensitivePaths` twice or handled set blocks differently, preventing the entire-set-block path from being stored. This allowed subsequent plans to not detect spurious changes.

## Key Components

### 1. SensitivePaths Computation
- **File:** `internal/configs/configschema/marks.go`
- **Lines:** 19-94 (Block.SensitivePaths) and 96-151 (Object.SensitivePaths)
- **Purpose:** Identifies which paths in a value should be marked as sensitive based on schema declarations
- **Special Case (Lines 75-88):** For NestingSet blocks with sensitive attributes, marks the entire set block path instead of individual elements to avoid hash instability

### 2. Planning and Mark Application
- **File:** `internal/terraform/node_resource_abstract_instance.go`
- **Line 1116:** Marks are applied from provider response: `plannedNewVal.MarkWithPaths(unmarkedPaths)`
- **Line 1117-1118:** PROBLEMATIC - SensitivePaths called again on already-marked value, causing entire-set paths to be stored in state
- **Line 2644:** During apply, marks are compared: `!marks.MarksEqual(beforePaths, afterPaths)`, triggering spurious Update actions

### 3. Mark Comparison
- **File:** `internal/lang/marks/paths.go`
- **Function:** `MarksEqual` (lines 130-165)
- **Purpose:** Compares two sets of PathValueMarks for equality
- **Impact:** When marks differ, an Update action is triggered even if values are identical

### 4. State Serialization
- **File:** `internal/states/instance_object.go`
- **Function:** `unmarkValueForStorage` (lines 205-223)
- **Purpose:** Extracts marks from values before storage
- **Result:** Sensitive paths are stored in `AttrSensitivePaths` field of ResourceInstanceObjectSrc

## The Bug Flow

1. **During Planning:**
   - Provider returns planned state with marks from `PlanResourceChange`
   - Marks are applied via `plannedNewVal.MarkWithPaths(unmarkedPaths)` (line 1116)
   - `SensitivePaths` is called again on the marked value (line 1117)
   - For set blocks with sensitive attributes, the entire set block path is returned
   - This entire-set-block path is stored in state's `AttrSensitivePaths`

2. **During Apply (Next Run):**
   - State is loaded with `sensitive_attributes: [[{"type": "get_attr", "value": "set_nested_block"}]]`
   - During planning, the same logic applies
   - Entire-set-block marks are computed again
   - Marks comparison detects they're "different" even though they represent the same sensitivity
   - Update action is triggered incorrectly

3. **Why It Loops:**
   - Every plan applies the entire-set-block marks
   - Every apply stores them in state
   - Every subsequent plan detects the same marks and triggers an update
   - The cycle continues indefinitely

## Solution Approaches

### Approach 1: Prevent Double Calling of SensitivePaths (PREFERRED)
Only call `SensitivePaths` on values that don't already have schema-computed marks applied. The provider response should be trusted to have already returned appropriately-marked values.

**Location:** `internal/terraform/node_resource_abstract_instance.go` around line 1117

**Fix:** Skip the second `SensitivePaths` call or only apply schema marks that the provider didn't already return.

### Approach 2: Handle Set Blocks Specially
Detect when `SensitivePaths` is being called on an already-marked set block value and avoid appending the entire set path again.

**Location:** `internal/configs/configschema/marks.go` around line 86

**Issue:** This approach is fragile and adds complexity to the marking logic.

### Approach 3: Improve Mark Comparison for Sets
Special-case the mark equality check to treat entire-set marks as equivalent to element marks for set blocks.

**Location:** `internal/lang/marks/paths.go` in `MarksEqual` function

**Issue:** This masks the root cause rather than fixing it.

## Files to Review for Complete Fix

| File | Purpose | Severity |
|------|---------|----------|
| `internal/terraform/node_resource_abstract_instance.go` | Lines 1117-1118: Double call to SensitivePaths | HIGH |
| `internal/configs/configschema/marks.go` | Lines 75-88: Set block marking logic (correct, but affected) | MEDIUM |
| `internal/lang/marks/paths.go` | Mark comparison logic | LOW |
| `internal/states/instance_object.go` | Unmark for storage (affected) | MEDIUM |
| `internal/plans/changes.go` | Change encoding with marks | MEDIUM |

## Related Code Paths

- **Marks extraction:** `internal/lang/marks/paths.go:PathsWithMark` (lines 25-66)
- **Marks removal:** `internal/lang/marks/paths.go:RemoveAll` (lines 72-102)
- **Change detection:** `internal/terraform/node_resource_abstract_instance.go:1290` - checks for mark inequality
- **Apply-time mark check:** `internal/terraform/node_resource_abstract_instance.go:2644` - detects update-only-for-marks

## Testing Recommendations

Create a test case that:
1. Defines a set nested block with a sensitive attribute
2. Creates a resource with this configuration
3. Runs a second plan without config changes
4. Verifies no spurious diff is detected
5. Checks that `sensitive_attributes` in state doesn't contain the entire set block path

**Test location:** `internal/terraform/context_plan_test.go` or `internal/terraform/context_apply_test.go`

## Summary

The regression is caused by calling `SensitivePaths` a second time on values that already have provider-returned marks applied. For set nested blocks with sensitive attributes, this results in the entire set block path being stored as a sensitive path (because set element hashing makes individual element marking unstable). This entire-set-block path gets persisted to state, and on subsequent plans, the same path is detected again but causes mark inequality, triggering spurious Update actions. 

The fix should prevent the second `SensitivePaths` call or ensure it doesn't re-add paths that have already been marked by the provider.
