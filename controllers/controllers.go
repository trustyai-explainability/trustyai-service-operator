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

package controllers

import (
	"errors"
	"fmt"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"slices"
	"strings"
)

// to set up a controller. may include webhook or not
type ControllerSetupFunc func(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error

var (
	// to store all controllers and their set up function
	TasServices = map[string]ControllerSetupFunc{}
	// convenient list to store all registered services
	AllTasServices = []string{}
)

type EnabledServices []string

// register a service. it's a private function for now.
// add a file in the same folder to call this function.
func registerService(name string, setupf ControllerSetupFunc) {
	TasServices[name] = setupf
	AllTasServices = append(AllTasServices, name)
}

func SetupControllers(enabledServices []string, mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	var errs []error
	for _, service := range enabledServices {
		errs = append(errs, TasServices[service](mgr, ns, configmap, recorder))
	}
	return errors.Join(errs...)
}

func (es *EnabledServices) Set(services string) error {
	for _, service := range strings.Split(services, ",") {
		if slices.Contains(*es, service) {
			return fmt.Errorf("specify the same service twice: %s", service)
		}
		if _, ok := TasServices[service]; ok {
			*es = append(*es, service)
		} else {
			return fmt.Errorf(
				"service %s is not supported. available services: %s",
				service,
				strings.Join(AllTasServices, ","),
			)
		}
	}

	return nil
}

func (es *EnabledServices) Empty() bool {
	return len(*es) == 0
}

func (es *EnabledServices) String() string {
	return strings.Join(*es, ",")
}
