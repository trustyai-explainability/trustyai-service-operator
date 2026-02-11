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
2. [smoke tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/smoke.yaml): Ensure the operator passes the smoke tests
3. [unit-tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml): Ensure unit tests pass.
4. e2e-tests: Ensure OpenShift CI job for e2e tests pass.

### Security Policy Validation

The project uses security validation tools to ensure manifests and container images follow best practices:

- **[Conftest](https://www.conftest.dev/)** with OPA (Open Policy Agent) validates Kubernetes manifests against security policies, enforcing container security, RBAC best practices, network isolation, and resource management.
- **[Hadolint](https://github.com/hadolint/hadolint)** lints Dockerfiles for best practices and common issues.

**Pre-commit Hooks (Recommended)**

To automatically validate your changes before committing, set up pre-commit hooks:

```bash
# Install pre-commit (one-time)
pip install pre-commit

# Install the git hooks
pre-commit install

# Test it
pre-commit run --all-files
```

**Note:** Requires `conftest`, `kustomize`, and `hadolint` to be available in your PATH.

The pre-commit hooks will automatically validate:
- Dockerfiles (via hadolint)
- `config/base` manifests (via conftest)
- `config/overlays/odh` manifests (via conftest)
- `config/overlays/rhoai` manifests (via conftest)

**Manual Validation**

You can also run validation manually:

```bash
# Lint Dockerfiles
hadolint Dockerfile
hadolint Dockerfile.driver
hadolint Dockerfile.lmes-job
hadolint Dockerfile.orchestrator

# Validate base configuration
kustomize build config/base | conftest test -p policy/ -

# Validate ODH overlay
kustomize build config/overlays/odh | conftest test -p policy/ -

# Validate RHOAI overlay
kustomize build config/overlays/rhoai | conftest test -p policy/ -
```

**Policy Files**

Security policies are located in the `policy/` directory and written in Rego. See [policy/README.md](policy/README.md) for details on specific policies and customisation options.

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