package main

import rego.v1

# Deny wildcard permissions in ClusterRoles
deny contains msg if {
    input.kind == "ClusterRole"
    rule := input.rules[_]
    rule.resources[_] == "*"
    msg := sprintf("ClusterRole '%s' grants wildcard (*) resource permissions, which should be avoided", [input.metadata.name])
}

deny contains msg if {
    input.kind == "ClusterRole"
    rule := input.rules[_]
    rule.verbs[_] == "*"
    msg := sprintf("ClusterRole '%s' grants wildcard (*) verb permissions, which should be avoided", [input.metadata.name])
}

# Deny wildcard permissions in Roles
deny contains msg if {
    input.kind == "Role"
    rule := input.rules[_]
    rule.resources[_] == "*"
    msg := sprintf("Role '%s' grants wildcard (*) resource permissions, which should be avoided", [input.metadata.name])
}

deny contains msg if {
    input.kind == "Role"
    rule := input.rules[_]
    rule.verbs[_] == "*"
    msg := sprintf("Role '%s' grants wildcard (*) verb permissions, which should be avoided", [input.metadata.name])
}

