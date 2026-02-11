package main

import rego.v1

# Deny pods using host network
deny contains msg if {
    input.kind == "Deployment"
    input.spec.template.spec.hostNetwork == true
    msg := sprintf("Deployment '%s' must not use hostNetwork", [input.metadata.name])
}

# Deny pods using host PID
deny contains msg if {
    input.kind == "Deployment"
    input.spec.template.spec.hostPID == true
    msg := sprintf("Deployment '%s' must not use hostPID", [input.metadata.name])
}

# Deny pods using host IPC
deny contains msg if {
    input.kind == "Deployment"
    input.spec.template.spec.hostIPC == true
    msg := sprintf("Deployment '%s' must not use hostIPC", [input.metadata.name])
}
