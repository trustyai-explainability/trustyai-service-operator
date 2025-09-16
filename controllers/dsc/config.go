package dsc

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DSCConfigMapName is the name of the ConfigMap created by OpenDataHub operator
	DSCConfigMapName = "trustyai-dsc-config"

	// DSC Configuration keys
	DSCPermitOnlineKey        = "eval.lmeval.permitOnline"
	DSCPermitCodeExecutionKey = "eval.lmeval.permitCodeExecution"
)

// DSCConfig represents the configuration values from the DSC ConfigMap
type DSCConfig struct {
	AllowOnline        bool
	AllowCodeExecution bool
}

// DSCConfigReader reads configuration from the DSC ConfigMap created by OpenDataHub operator
type DSCConfigReader struct {
	Client    client.Client
	Namespace string
}

// ReadDSCConfig reads configuration values from the trustyai-dsc-config ConfigMap
func (r *DSCConfigReader) ReadDSCConfig(ctx context.Context, log *logr.Logger) (*DSCConfig, error) {
	if r.Namespace == "" {
		log.V(1).Info("No namespace specified, skipping DSC config reading")
		return &DSCConfig{}, nil
	}

	configMapKey := types.NamespacedName{
		Namespace: r.Namespace,
		Name:      DSCConfigMapName,
	}

	var cm corev1.ConfigMap
	if err := r.Client.Get(ctx, configMapKey, &cm); err != nil {
		if errors.IsNotFound(err) {
			log.V(1).Info("DSC ConfigMap not found, using default configuration",
				"configmap", configMapKey)
			return &DSCConfig{}, nil
		}
		return nil, fmt.Errorf("error reading DSC ConfigMap %s: %w", configMapKey, err)
	}

	log.V(1).Info("Found DSC ConfigMap, processing configuration",
		"configmap", configMapKey)

	// Process DSC-specific configuration
	config, err := r.processDSCConfig(&cm, log)
	if err != nil {
		return nil, fmt.Errorf("error processing DSC configuration: %w", err)
	}

	return config, nil
}

// processDSCConfig processes the DSC ConfigMap data and returns configuration
func (r *DSCConfigReader) processDSCConfig(cm *corev1.ConfigMap, log *logr.Logger) (*DSCConfig, error) {
	config := &DSCConfig{}
	var msgs []string

	// Process permitOnline setting
	if permitOnlineStr, found := cm.Data[DSCPermitOnlineKey]; found {
		permitOnline, err := strconv.ParseBool(permitOnlineStr)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("invalid DSC setting for %s: %s, using default",
				DSCPermitOnlineKey, permitOnlineStr))
		} else {
			config.AllowOnline = permitOnline
			log.V(1).Info("Read PermitOnline from DSC config",
				"key", DSCPermitOnlineKey, "value", permitOnline)
		}
	}

	// Process permitCodeExecution setting
	if permitCodeExecutionStr, found := cm.Data[DSCPermitCodeExecutionKey]; found {
		permitCodeExecution, err := strconv.ParseBool(permitCodeExecutionStr)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("invalid DSC setting for %s: %s, using default",
				DSCPermitCodeExecutionKey, permitCodeExecutionStr))
		} else {
			config.AllowCodeExecution = permitCodeExecution
			log.V(1).Info("Read PermitCodeExecution from DSC config",
				"key", DSCPermitCodeExecutionKey, "value", permitCodeExecution)
		}
	}

	if len(msgs) > 0 && log != nil {
		log.Error(fmt.Errorf("some DSC settings are invalid"), fmt.Sprintf("DSC config errors: %v", msgs))
	}

	return config, nil
}

// NewDSCConfigReader creates a new DSCConfigReader instance
func NewDSCConfigReader(client client.Client, namespace string) *DSCConfigReader {
	return &DSCConfigReader{
		Client:    client,
		Namespace: namespace,
	}
}
