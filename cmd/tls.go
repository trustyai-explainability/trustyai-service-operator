package main

import (
	"crypto/tls"

	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// configureManagerTLS applies the same validated policy (observed or permitted
// Intermediate fallback) to every operator
// TLS listener. Keep certificate and metrics authentication settings supplied by
// the caller; TLSOpts alone does not enable secure metrics serving.
//
// Resolve must succeed before this is called. Operand proxy arguments are
// published separately, only after the complete startup result is validated.
func configureManagerTLS(options *ctrl.Options, resolved pkgtls.Result, enableWebhook bool) {
	// Independent slices prevent one server's option changes from affecting the
	// other. Profile callbacks include the component's explicit h2 ALPN policy.
	options.Metrics.TLSOpts = append([]func(*tls.Config){}, resolved.TLSOpts...)
	if enableWebhook {
		options.WebhookServer = webhook.NewServer(webhook.Options{
			Port:    9443,
			TLSOpts: append([]func(*tls.Config){}, resolved.TLSOpts...),
		})
	}
}
