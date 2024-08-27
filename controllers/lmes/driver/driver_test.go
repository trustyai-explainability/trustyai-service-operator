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
		Args:           []string{"sh", "-ec", "echo 'testing progress: 100%|' >&2; sleep 10"},
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
