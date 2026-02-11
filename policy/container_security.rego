package main

import rego.v1

# Deny containers running as root
deny contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.securityContext.runAsNonRoot
    msg := sprintf("Container '%s' in Deployment '%s' must set runAsNonRoot to true", [container.name, input.metadata.name])
}

# Deny containers that allow privilege escalation (missing or explicitly true)
deny contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    not container.securityContext.allowPrivilegeEscalation == false
    msg := sprintf("Container '%s' in Deployment '%s' must set allowPrivilegeEscalation to false", [container.name, input.metadata.name])
}

# Deny privileged containers
deny contains msg if {
    input.kind == "Deployment"
    container := input.spec.template.spec.containers[_]
    container.securityContext.privileged == true
    msg := sprintf("Container '%s' in Deployment '%s' must not run in privileged mode", [container.name, input.metadata.name])
}
