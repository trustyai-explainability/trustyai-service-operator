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
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

var options *serviceOptions = &serviceOptions{
	DriverImage:         DefaultDriverImage,
	PodImage:            DefaultPodImage,
	PodCheckingInterval: DefaultPodCheckingInterval,
	ImagePullPolicy:     DefaultImagePullPolicy,
	MaxBatchSize:        DefaultMaxBatchSize,
	DetectDevice:        DefaultDetectDevice,
	DefaultBatchSize:    DefaultBatchSize,
}

type serviceOptions struct {
	PodImage            string
	DriverImage         string
	PodCheckingInterval time.Duration
	ImagePullPolicy     corev1.PullPolicy
	MaxBatchSize        int
	DefaultBatchSize    int
	DetectDevice        bool
}

func constructOptionsFromConfigMap(log *logr.Logger, configmap *corev1.ConfigMap) error {

	rv := reflect.ValueOf(options).Elem()
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
