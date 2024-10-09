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

package driver

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/api/v1beta1"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	driverLog = ctrl.Log.WithName("driver-test")
)

func TestMain(m *testing.M) {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()
	log.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	m.Run()
}

type DummyUpdateServer struct {
	v1beta1.UnimplementedLMEvalJobUpdateServiceServer
}

func (*DummyUpdateServer) UpdateStatus(context.Context, *v1beta1.JobStatus) (*v1beta1.Response, error) {
	return &v1beta1.Response{
		Code:    v1beta1.ResponseCode_OK,
		Message: "updated the job status successfully",
	}, nil
}

func Test_Driver(t *testing.T) {
	server := grpc.NewServer()
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &DummyUpdateServer{})
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:      context.Background(),
		JobNamespace: "fms-lm-eval-service-system",
		JobName:      "evaljob-sample",
		GrpcService:  "localhost",
		GrpcPort:     8082,
		OutputPath:   ".",
		Logger:       driverLog,
		Args:         []string{"sh", "-ec", "echo tttttttttttttttttttt"},
	})
	assert.Nil(t, err)

	assert.Nil(t, driver.Run())

	server.Stop()
	assert.Nil(t, os.Remove("./stderr.log"))
	assert.Nil(t, os.Remove("./stdout.log"))
}

type ProgressUpdateServer struct {
	v1beta1.UnimplementedLMEvalJobUpdateServiceServer
	progressMsgs []string
}

func (s *ProgressUpdateServer) UpdateStatus(_ context.Context, status *v1beta1.JobStatus) (*v1beta1.Response, error) {
	if status.StatusMessage != "" {
		s.progressMsgs = append(s.progressMsgs, status.StatusMessage)
	}
	return &v1beta1.Response{
		Code:    v1beta1.ResponseCode_OK,
		Message: "updated the job status successfully",
	}, nil
}

func Test_ProgressUpdate(t *testing.T) {
	server := grpc.NewServer()
	progresssServer := ProgressUpdateServer{}
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &progresssServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:        context.Background(),
		JobNamespace:   "fms-lm-eval-service-system",
		JobName:        "evaljob-sample",
		GrpcService:    "localhost",
		GrpcPort:       8082,
		OutputPath:     ".",
		Logger:         driverLog,
		Args:           []string{"sh", "-ec", "echo 'testing progress: 100%|' >&2; sleep 6"},
		ReportInterval: time.Second * 5,
	})
	assert.Nil(t, err)

	assert.Nil(t, driver.Run())
	assert.Equal(t, []string{
		"update status from the driver: running",
		"testing progress: 100%",
		"update status from the driver: completed",
	}, progresssServer.progressMsgs)

	server.Stop()
	assert.Nil(t, os.Remove("./stderr.log"))
	assert.Nil(t, os.Remove("./stdout.log"))
}

func Test_DetectDeviceError(t *testing.T) {
	server := grpc.NewServer()
	progresssServer := ProgressUpdateServer{}
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &progresssServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:        context.Background(),
		JobNamespace:   "fms-lm-eval-service-system",
		JobName:        "evaljob-sample",
		GrpcService:    "localhost",
		GrpcPort:       8082,
		OutputPath:     ".",
		DetectDevice:   true,
		Logger:         driverLog,
		Args:           []string{"sh", "-ec", "python -m lm_eval --output_path ./output --model test --model_args arg1=value1 --tasks task1,task2"},
		ReportInterval: time.Second * 5,
	})
	assert.Nil(t, err)

	assert.Nil(t, driver.Run())
	assert.Equal(t, []string{
		"update status from the driver: running",
		"failed to detect available device(s): exit status 1",
	}, progresssServer.progressMsgs)

	server.Stop()

	// the following files don't exist for this case
	assert.NotNil(t, os.Remove("./stderr.log"))
	assert.NotNil(t, os.Remove("./stdout.log"))
}

func Test_PatchDevice(t *testing.T) {
	driverOpt := DriverOption{
		Context:        context.Background(),
		JobNamespace:   "fms-lm-eval-service-system",
		JobName:        "evaljob-sample",
		GrpcService:    "localhost",
		GrpcPort:       8082,
		OutputPath:     ".",
		DetectDevice:   true,
		Logger:         driverLog,
		Args:           []string{"sh", "-ec", "python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2"},
		ReportInterval: time.Second * 5,
	}

	// append `--device cuda`
	patchDevice(driverOpt.Args, true)
	assert.Equal(t,
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --device cuda",
		driverOpt.Args[2],
	)

	// append `--device cpu`
	driverOpt.Args = []string{"sh", "-ec", "python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2"}
	patchDevice(driverOpt.Args, false)
	assert.Equal(t,
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --device cpu",
		driverOpt.Args[2],
	)

	// no change because `--device cpu` exists
	driverOpt.Args = []string{"sh", "-ec", "python -m lm_eval --device cpu --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2"}
	patchDevice(driverOpt.Args, true)
	assert.Equal(t,
		"python -m lm_eval --device cpu --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2",
		driverOpt.Args[2],
	)
}

func Test_TaskRecipes(t *testing.T) {
	server := grpc.NewServer()
	progresssServer := ProgressUpdateServer{}
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &progresssServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:         context.Background(),
		JobNamespace:    "fms-lm-eval-service-system",
		JobName:         "evaljob-sample",
		GrpcService:     "localhost",
		GrpcPort:        8082,
		OutputPath:      ".",
		Logger:          driverLog,
		TaskRecipesPath: "./",
		TaskRecipes: []string{
			"card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
			"card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		},
		Args:           []string{"sh", "-ec", "echo 'testing progress: 100%|' >&2; sleep 6"},
		ReportInterval: time.Second * 5,
	})
	assert.Nil(t, err)

	assert.Nil(t, driver.Run())
	assert.Equal(t, []string{
		"update status from the driver: running",
		"testing progress: 100%",
		"update status from the driver: completed",
	}, progresssServer.progressMsgs)

	server.Stop()

	tr0, err := os.ReadFile("./tr_0.yaml")
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_0\ninclude: unitxt\nrecipe: card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr0),
	)
	tr1, err := os.ReadFile("./tr_1.yaml")
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_1\ninclude: unitxt\nrecipe: card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr1),
	)
	assert.Nil(t, os.Remove("./stderr.log"))
	assert.Nil(t, os.Remove("./stdout.log"))
	assert.Nil(t, os.Remove("./tr_0.yaml"))
	assert.Nil(t, os.Remove("./tr_1.yaml"))
}

func Test_CustomCards(t *testing.T) {
	server := grpc.NewServer()
	progresssServer := ProgressUpdateServer{}
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &progresssServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:         context.Background(),
		JobNamespace:    "fms-lm-eval-service-system",
		JobName:         "evaljob-sample",
		GrpcService:     "localhost",
		GrpcPort:        8082,
		OutputPath:      ".",
		Logger:          driverLog,
		TaskRecipesPath: "./",
		CatalogPath:     "./",
		TaskRecipes: []string{
			"card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		},
		CustomCards: []string{
			`{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		},
		Args:           []string{"sh", "-ec", "echo 'testing progress: 100%|' >&2; sleep 6"},
		ReportInterval: time.Second * 5,
	})
	assert.Nil(t, err)

	os.Mkdir("cards", 0750)

	assert.Nil(t, driver.Run())
	assert.Equal(t, []string{
		"update status from the driver: running",
		"testing progress: 100%",
		"update status from the driver: completed",
	}, progresssServer.progressMsgs)

	server.Stop()

	tr0, err := os.ReadFile("./tr_0.yaml")
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_0\ninclude: unitxt\nrecipe: card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr0),
	)
	custom0, err := os.ReadFile("./cards/custom_0.json")
	assert.Nil(t, err)
	assert.Equal(t,
		`{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		string(custom0),
	)
	assert.Nil(t, os.Remove("./stderr.log"))
	assert.Nil(t, os.Remove("./stdout.log"))
	assert.Nil(t, os.Remove("./tr_0.yaml"))
	assert.Nil(t, os.Remove("./cards/custom_0.json"))
	assert.Nil(t, os.Remove("./cards"))
}

func Test_ProgramError(t *testing.T) {
	server := grpc.NewServer()
	progresssServer := ProgressUpdateServer{}
	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &progresssServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 8082))
	assert.Nil(t, err)
	go server.Serve(lis)

	driver, err := NewDriver(&DriverOption{
		Context:        context.Background(),
		JobNamespace:   "fms-lm-eval-service-system",
		JobName:        "evaljob-sample",
		GrpcService:    "localhost",
		GrpcPort:       8082,
		OutputPath:     ".",
		Logger:         driverLog,
		Args:           []string{"sh", "-ec", "exit 1"},
		ReportInterval: time.Second * 5,
	})
	assert.Nil(t, err)

	assert.Nil(t, driver.Run())
	assert.Equal(t, []string{
		"update status from the driver: running",
		"exit status 1",
	}, progresssServer.progressMsgs)

	server.Stop()
	assert.Nil(t, os.Remove("./stderr.log"))
	assert.Nil(t, os.Remove("./stdout.log"))
}
