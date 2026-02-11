package main

import rego.v1

# Deny dangerous capabilities
dangerous_capabilities := {
    "SYS_ADMIN",
    "SYS_MODULE",
    "SYS_RAWIO",
    "SYS_PTRACE",
    "SYS_BOOT",
    "MAC_ADMIN",
    "MAC_OVERRIDE",
    "PERFMON",
    "BPF",
}

deny contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    capability := container.securityContext.capabilities.add[_]
    dangerous_capabilities[capability]
    msg := sprintf("Container '%s' in Deployment '%s' adds dangerous capability '%s'", [container.name, input.metadata.name, capability])
}

# Warn if all capabilities are not dropped
warn contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not drops_all_capabilities(container)
    msg := sprintf("Container '%s' in Deployment '%s' should drop all capabilities and only add required ones", [container.name, input.metadata.name])
}

# Helper function
drops_all_capabilities(container) if {
    container.securityContext.capabilities.drop[_] == "ALL"
}
