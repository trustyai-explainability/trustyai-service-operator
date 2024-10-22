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

package main

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func Test_ArgParsing(t *testing.T) {
	os.Args = []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--detect-device",
		"--task-recipe", "card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
		"sh", "-c", "python",
	}

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	args := flag.Args()

	assert.Equal(t, true, *detectDevice)
	assert.Equal(t, strArrayArg{
		"card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
	}, taskRecipes)

	dOption := driver.DriverOption{
		Context:      context.Background(),
		OutputPath:   *outputPath,
		DetectDevice: *detectDevice,
		Logger:       driverLog,
		TaskRecipes:  taskRecipes,
		Args:         args,
	}

	assert.Equal(t, []string{
		"card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
	}, dOption.TaskRecipes)

	assert.Equal(t, []string{
		"sh", "-c", "python",
	}, dOption.Args)
}
