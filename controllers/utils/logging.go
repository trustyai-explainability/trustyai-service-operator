package utils

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ==== ERROR MESSAGES ====

// LogErrorParsing produces a log message regarding the failed parsing of a resource template
func LogErrorParsing(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "parsing", resourceKind, resourceName, namespace)
}

// LogErrorRetrieving produces a log message regarding the failed retrieval of some resource
func LogErrorRetrieving(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "retrieving", resourceKind, resourceName, namespace)
}

// LogErrorUpdating produces a log message regarding the failed update of some resource
func LogErrorUpdating(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "updating", resourceKind, resourceName, namespace)
}

// LogErrorDefining produces a log message regarding the failed definition of some resource
func LogErrorDefining(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "defining", resourceKind, resourceName, namespace)
}

// LogErrorCreating produces a log message regarding the failed creation of some resource
func LogErrorCreating(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "creating", resourceKind, resourceName, namespace)
}

// LogErrorReconciling produces a log message regarding the failed reconciliation of some resource
func LogErrorReconciling(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "reconciling", resourceKind, resourceName, namespace)
}

// LogErrorControllerReference produces a log message regarding a failure when setting a resource's controller reference
func LogErrorControllerReference(ctx context.Context, err error, resourceKind string, resourceName string, namespace string) {
	LogErrorVerb(ctx, err, "setting controller reference for", resourceKind, resourceName, namespace)
}

// LogErrorVerb produces a log message regarding the failed $VERB of some resource
func LogErrorVerb(ctx context.Context, err error, verb string, resourceKind string, resourceName string, namespace string) {
	log.FromContext(ctx).Error(err, fmt.Sprintf("error %s %s %s in namespace %s", verb, resourceKind, resourceName, namespace))
}

// ==== INFO MESSAGES ====

// LogInfoCreating produces a log message regarding starting the creation of some resource
func LogInfoCreating(ctx context.Context, resourceKind string, resourceName string, namespace string) {
	LogInfoVerb(ctx, "creating", resourceKind, resourceName, namespace)
}

// LogInfoUpdating produces a log message regarding starting the creation of some resource
func LogInfoUpdating(ctx context.Context, resourceKind string, resourceName string, namespace string) {
	LogInfoVerb(ctx, "updating", resourceKind, resourceName, namespace)
}

// LogInfoVerb produces an info message regarding the $VERB of some resource
func LogInfoVerb(ctx context.Context, verb string, resourceKind string, resourceName string, namespace string) {
	log.FromContext(ctx).Info(fmt.Sprintf("%s %s %s in namespace %s", verb, resourceKind, resourceName, namespace))
}
