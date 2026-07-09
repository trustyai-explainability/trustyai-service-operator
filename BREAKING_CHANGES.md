# Breaking Changes

## TrustyAIService v1 CRD - Platform Contract Compliance (RHOAIENG-67659)

**Branch**: `feat/tas-crd-platform-contract`

### Changes

1. **Condition Type**: Changed from `[]common.Condition` to `[]metav1.Condition`
   - `common.Condition` had optional `reason`, `message`, and `lastTransitionTime` fields
   - `metav1.Condition` **requires** these fields to be present and non-empty

2. **CRD Schema Validation**: The OpenAPI schema now enforces:
   - `lastTransitionTime`: required (format: date-time)
   - `message`: required (min: 1 char, max: 32768 chars)
   - `reason`: required (min: 1 char, max: 1024 chars, pattern: `^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)
   - `observedGeneration`: optional (new field)

### Impact

- **Tier2 operator-chaos check**: Will fail with breaking CRD schema changes
  - 3 RequiredAdded changes detected (lastTransitionTime, message, reason)
  - This is EXPECTED and INTENTIONAL for platform contract adoption

- **Existing v1alpha1 resources**: Backward compatible via conversion webhook
  - Empty `reason` → defaults to "Unknown"
  - Empty `message` → defaults to "No message provided"
  - Missing `lastTransitionTime` → preserved from source

- **New v1 resources**: Must provide all required fields
  - All controller code updated to provide non-empty values
  - `SetStatus()` method ensures proper field population

### Justification

This breaking change is required for **RHOAIENG-67660 Platform Contract Compliance**:
- Standard Kubernetes condition format (`metav1.Condition`)
- Required by modular architecture for cross-component interoperability
- Aligns with upstream Kubernetes API conventions

### Migration Path

1. **v1alpha1 users**: No action required
   - Conversion webhook handles migration automatically
   - Default values provided for missing fields

2. **v1 users** (new in this branch): Must provide complete conditions
   - Use `SetStatus()` method which handles all required fields
   - Or manually ensure `reason`, `message`, and `lastTransitionTime` are set

### Testing

- ✅ Unit tests verify conversion with default values
- ✅ Controller status updates tested with required fields
- ✅ `go vet` passes
- ⏳ Tier2 operator-chaos: **Expected to fail** (breaking schema changes)
  - Will be resolved when this branch is merged to main (base branch update)

### Related Work

- Task: RHOAIENG-67659 - Update TrustyAIService CRD for Platform Contract Compliance
- Epic: RHOAIENG-60939 - Modular Architecture
- Previous: RHOAIENG-67660 - Create TrustyAI Module CRD (PR #801)
