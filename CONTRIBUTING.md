# Contributing to the TrustyAI Operator

Thanks for your interest in the TrustyAI operator project! You can contribute to this project in various ways: filing bug reports, proposing features, submitting pull requests (PRs), and improving documentation.

Before you begin, please take a look at our contribution guidelines below to ensure that your contribuutions are aligned with the project's goals.

## Reporting Issues
Issues are tracked using [Github](https://github.com/trustyai-explainability/trustyai-service-operator/issues). If you encounter a bug or have suggestions for enhancements, please follow the steps below:

1. **Check for Existing Issues:** Before creating a new issue, search the Github project to see if a similar issue already exists.
2. **Create a Github issue:** If the issue doesn’t exist, create a new ticket in Github.
    - **For Feature Requests:**  Set the label as `feature`
    - **For Bugs:** Set the label to `kind/bug`
    - **For all other code changes:** Use the issue type `kind/enhancement`
    - Add to the "TrustyAI planning" in "Projects"
      - And specify "Component" as "Operator" 

## Pull Requests

### Workflow

1. **Fork the Repository:** Create your own fork of the repository to work on your changes.
2. **Create a Branch:** Create your own branch to include changes for the feature or a bug fix off of `main` branch.
3. **Work on Your Changes:** Commit often, and ensure your code passes all the test for the operator.
4. **Testing:** Make sure your code passes all the tests, including any new tests you've added. And that your changes do not decrease the test coverage as shown on report. Every new feature should come with unit tests that cover that new part of the code.

### Open a Pull Request:

1. **Link to Github Issue**: Include the Github issue link in your PR description.
2. **Description**: Provide a detailed description of the changes and what they fix or implement.
3. **Add Testing Steps**: Provide information on how the PR has been tested, and list out testing steps if any for reviewers.
4. **Review Request**: Tag the relevant maintainers(`@trustyai-explainability/developers`) for a review.
5. **Resolve Feedback**: Be open to feedback and iterate on your changes.

### Quality Gates

To ensure the contributed code adheres to the project goals, we have set up some automated quality gates:

1. [linters](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/lint-yaml.yaml): Ensure the check for linters is successful.
2. [manifest policy check](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/conftest.yaml): Ensure rendered manifests pass all OPA policies (see `policy/README.md`).
3. [smoke tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/smoke.yaml): Ensure the operator passes the smoke tests
4. [unit-tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml): Ensure unit tests pass.
5. [operator-chaos](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/operator-chaos.yml): Ensure no breaking upgrade changes are introduced.
6. e2e-tests: Ensure OpenShift CI job for e2e tests pass.

### Shift-left Upgrade Validation (operator-chaos)

This operator uses [operator-chaos](https://github.com/opendatahub-io/operator-chaos) for shift-left upgrade validation. A GitHub Actions workflow runs on every PR that touches operator code, CRDs, or the knowledge model to catch breaking changes before merge.

**What it checks:**
- Knowledge model validity and structural regressions
- CRD schema breaking changes across all 5 CRDs
- Simulated upgrade dry-run

**Maintaining the knowledge model:**

The knowledge model at `chaos/knowledge/trustyai.yaml` describes the operator's control plane topology. Update it when:
- Adding or removing RBAC resources
- Renaming the operator deployment or service account
- Changing the leader election lease name
- Adding new operator-level resources (not per-CR workloads)

```bash
# Validate locally
go install github.com/opendatahub-io/operator-chaos/cmd/operator-chaos@9e6ac9668b9aaca2f0f2ddf169867862b7925b80
operator-chaos validate --knowledge chaos/knowledge/trustyai.yaml
operator-chaos preflight --knowledge chaos/knowledge/trustyai.yaml --local
```

### Manifest Policies (Conftest/OPA)

Rendered kustomize manifests are checked against [OPA/Rego](https://www.openpolicyagent.org/) policies in `policy/` using [Conftest](https://www.conftest.dev/). The CI workflow tests **all** kustomize entry points (base + every overlay).

**Current policies:**
- **RBAC allowlist** (`policy/rbac.rego`): a closed allowlist of expected `ClusterRoleBinding` resources. Any new or unexpected CRB will fail CI.

**If your PR introduces a new `ClusterRoleBinding`:**
1. Add the post-kustomize CRB name and its expected `ClusterRole` to `expected_crbs` in `policy/rbac.rego`.
2. Run `make policy-check` locally to verify.
3. Explain in the PR description why a `ClusterRoleBinding` is required rather than a namespace-scoped `RoleBinding`.

See `policy/README.md` for full details on running and extending policies.

```bash
# Run locally
make policy-test    # OPA unit tests
make policy-check   # Full check against all overlays
```

### Code Style Guidelines

1. Follow the Go community’s best practices, which can be found in the official [Effective Go](https://go.dev/doc/effective_go) guide.
2. Follow the best practices defined by the [Operator SDK](https://sdk.operatorframework.io/docs/best-practices/).
3. Use `go fmt` to automatically format your code.
4. Ensure you write clear and concise comments, especially for exported functions.
5. Always check and handle errors appropriately. Avoid ignoring errors by using _.
6. Make sure to run `go mod tidy` before submitting a PR to ensure the `go.mod` and `go.sum` files are up-to-date.

### Commit Messages

We follow the conventional commits format for writing commit messages. A good commit message should include:
1. **Type:** `fix`, `feat`, `docs`, `chore`, etc. **Note:** All `fix` and `feat` commits require an associated issue. Please add link to your Github issue. 
   1. Security fixes should be in the format `fix(CVE-xxx): `
2. **Scope:** A short description of the area affected.
3. **Summary:** A brief explanation of what the commit does.

## Communication

For general questions, feel free to open a discussion in our repository or communicate via:

- **Comments**: Feel free to discuss issues directly on Github issues.
- **Discussions**: Alternatively, use [Github discussions](https://github.com/orgs/trustyai-explainability/discussions)