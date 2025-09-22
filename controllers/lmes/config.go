/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lmes

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/dsc"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// set by job_mgr controllerSetup func
var JobMgrEnabled bool
var Options *serviceOptions = &serviceOptions{
	DriverImage:         DefaultDriverImage,
	PodImage:            DefaultPodImage,
	PodCheckingInterval: DefaultPodCheckingInterval,
	ImagePullPolicy:     DefaultImagePullPolicy,
	MaxBatchSize:        DefaultMaxBatchSize,
	DetectDevice:        DefaultDetectDevice,
	DefaultBatchSize:    DefaultBatchSize,
	AllowOnline:         false,
	AllowCodeExecution:  false,
	DriverPort:          driver.DefaultPort,
}

type serviceOptions struct {
	PodImage            string
	DriverImage         string
	PodCheckingInterval time.Duration
	ImagePullPolicy     corev1.PullPolicy
	MaxBatchSize        int
	DefaultBatchSize    string
	DetectDevice        bool
	AllowOnline         bool
	AllowCodeExecution  bool
	DriverPort          int
}

func constructOptionsFromConfigMap(log *logr.Logger, configmap *corev1.ConfigMap) error {

	rv := reflect.ValueOf(Options).Elem()
	var msgs []string

	for idx, cap := 0, rv.NumField(); idx < cap; idx++ {
		frv := rv.Field(idx)
		fname := rv.Type().Field(idx).Name
		configKey, ok := optionKeys[fname]
		if !ok {
			continue
		}

		if v, found := configmap.Data[configKey]; found {
			var err error
			switch frv.Type().Name() {
			case "string":
				frv.SetString(v)
			case "bool":
				val, err := strconv.ParseBool(v)
				if err != nil {
					val = DefaultDetectDevice
					msgs = append(msgs, fmt.Sprintf("invalid setting for %v: %v, use default setting instead", optionKeys[fname], val))
				}
				frv.SetBool(val)
			case "int":
				var intVal int
				intVal, err = strconv.Atoi(v)
				if err == nil {
					frv.SetInt(int64(intVal))
				}
			case "Duration":
				var d time.Duration
				d, err = time.ParseDuration(v)
				if err == nil {
					frv.Set(reflect.ValueOf(d))
				}
			case "PullPolicy":
				if p, found := pullPolicyMap[corev1.PullPolicy(v)]; found {
					frv.Set(reflect.ValueOf(p))
				} else {
					err = fmt.Errorf("invalid PullPolicy")
				}
			default:
				return fmt.Errorf("can not handle the config %v, type: %v", optionKeys[fname], frv.Type().Name())
			}

			if err != nil {
				msgs = append(msgs, fmt.Sprintf("invalid setting for %v: %v, use default setting instead", optionKeys[fname], v))
			}
		}
	}

	if len(msgs) > 0 && log != nil {
		log.Error(fmt.Errorf("some settings in the configmap are invalid"), strings.Join(msgs, "\n"))
	}

	return nil
}

// ApplyDSCConfig applies DSC configuration to the LMES Options
// Only applies configuration if DSC config is available and not nil
func ApplyDSCConfig(dscConfig *dsc.DSCConfig) {
	if dscConfig != nil {
		Options.AllowOnline = dscConfig.AllowOnline
		Options.AllowCodeExecution = dscConfig.AllowCodeExecution
	}
}

// PermissionConfig holds the effective permissions for LMEval jobs
type PermissionConfig struct {
	AllowOnline        bool
	AllowCodeExecution bool
}

// ReadEffectivePermissions reads both the default configmap and DSC config,
// returning the effective permissions (DSC config overrides defaults)
func ReadEffectivePermissions(ctx context.Context, client client.Client, namespace, configMapName string, log *logr.Logger) (*PermissionConfig, error) {
	// Start with default values
	config := NewDefaultPermissionConfig()

	// Read default configmap first
	var cm corev1.ConfigMap
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: configMapName}, &cm); err != nil {
		log.Error(err, "failed to get default configmap", "namespace", namespace, "name", configMapName)
		return nil, err
	}

	// Apply default config values for permissions
	if allowOnlineStr, found := cm.Data[AllowOnline]; found {
		if allowOnline, err := strconv.ParseBool(allowOnlineStr); err == nil {
			config.AllowOnline = allowOnline
		}
	}
	if allowCodeExecutionStr, found := cm.Data[AllowCodeExecution]; found {
		if allowCodeExecution, err := strconv.ParseBool(allowCodeExecutionStr); err == nil {
			config.AllowCodeExecution = allowCodeExecution
		}
	}

	// Read DSC configuration if available and override defaults
	dscReader := dsc.NewDSCConfigReader(client, namespace)
	if dscConfig, err := dscReader.ReadDSCConfig(ctx, log); err != nil {
		log.Error(err, "failed to read DSC configuration, using default config")
		// Continue with default config - DSC config is optional
	} else if dscConfig != nil {
		config.AllowOnline = dscConfig.AllowOnline
		config.AllowCodeExecution = dscConfig.AllowCodeExecution
		log.V(1).Info("Applied DSC configuration overrides",
			"allowOnline", dscConfig.AllowOnline,
			"allowCodeExecution", dscConfig.AllowCodeExecution)
	}

	return config, nil
}

// NewDefaultPermissionConfig creates a permission config with default values
func NewDefaultPermissionConfig() *PermissionConfig {
	return &PermissionConfig{
		AllowOnline:        false,
		AllowCodeExecution: false,
	}
}
