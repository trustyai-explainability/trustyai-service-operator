package main

import rego.v1

# Warn about missing resource limits
warn contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.resources.limits
    msg := sprintf("Container '%s' in Deployment '%s' should define resource limits", [container.name, input.metadata.name])
}

# Warn about missing resource requests
warn contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.resources.requests
    msg := sprintf("Container '%s' in Deployment '%s' should define resource requests", [container.name, input.metadata.name])
}

# Warn about missing memory limits
warn contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    container.resources.limits
    not container.resources.limits.memory
    msg := sprintf("Container '%s' in Deployment '%s' should define memory limit", [container.name, input.metadata.name])
}

# Warn about missing CPU limits
warn contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    container.resources.limits
    not container.resources.limits.cpu
    msg := sprintf("Container '%s' in Deployment '%s' should define CPU limit", [container.name, input.metadata.name])
}
