##@ TrustyAI Operator Module Controller (trustyai-operator-module)

TRUSTYAI_MODULE_IMG ?= trustyai-operator-module-controller
MODULE_DIR         := trustyai-operator-module
MODULE_TAG         ?= latest
ENGINE             ?= $(BUILD_TOOL)

.PHONY: docker-build-tom
docker-build-tom: ## Build the trustyai-operator-module-controller image
	$(ENGINE) buildx build \
		--load \
		-t $(TRUSTYAI_MODULE_IMG):$(MODULE_TAG) \
		--build-arg VERSION=$(MODULE_TAG) \
		-f $(MODULE_DIR)/Dockerfile \
		$(MODULE_DIR)

.PHONY: docker-push-tom
docker-push-tom: docker-build-tom ## Build and push the trustyai-operator-module-controller image
	$(ENGINE) push $(TRUSTYAI_MODULE_IMG):$(MODULE_TAG)

.PHONY: deploy-tom
deploy-tom: ## Deploy the trustyai-operator-module-controller to the cluster
	cd $(MODULE_DIR)/config/default && \
		$(KUSTOMIZE) edit set image trustyai-operator-module-controller=$(TRUSTYAI_MODULE_IMG):$(MODULE_TAG)
	kubectl apply -k $(MODULE_DIR)/config/default --server-side=true

.PHONY: undeploy-tom
undeploy-tom: ## Remove the trustyai-operator-module-controller from the cluster
	kubectl delete -k $(MODULE_DIR)/config/default --ignore-not-found=true

.PHONY: manifests-tom
manifests-tom: ## Generate CRD manifests for trustyai-operator-module
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook \
		paths="./$(MODULE_DIR)/pkg/apis/..." \
		paths="./$(MODULE_DIR)/pkg/trustyaimodule/..." \
		output:crd:artifacts:config=$(MODULE_DIR)/config/crd/bases \
		output:rbac:artifacts:config=$(MODULE_DIR)/config/rbac

.PHONY: generate-tom
generate-tom: ## Generate DeepCopy methods for trustyai-operator-module
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" \
		paths="./$(MODULE_DIR)/pkg/apis/..."

.PHONY: precommit-tom
precommit-tom: ## Run pre-commit checks for trustyai-operator-module
	cd $(MODULE_DIR) && go mod tidy && go vet ./... && go build ./...

.PHONY: test-tom
test-tom: ## Run tests for trustyai-operator-module
	cd $(MODULE_DIR) && go test ./... -v
