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
	"fmt"
	"io"
	"os"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/spf13/viper"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
)

const (
	OutputPath = "/opt/app-root/src/output"
)

var (
	copy           = flag.String("copy", "", "copy this binary to specified destination path")
	jobNameSpace   = flag.String("job-namespace", "", "Job's namespace ")
	jobName        = flag.String("job-name", "", "Job's name")
	grpcService    = flag.String("grpc-service", "", "grpc service name")
	grpcPort       = flag.Int("grpc-port", 8082, "grpc port")
	outputPath     = flag.String("output-path", OutputPath, "output path")
	detectDevice   = flag.Bool("detect-device", true, "detect available device(s), CUDA or CPU")
	reportInterval = flag.Duration("report-interval", time.Second*10, "specify the druation interval to report the progress")
	driverLog      = ctrl.Log.WithName("driver")
)

func main() {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()
	viper.AutomaticEnv()

	log.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	ctx := context.Background()
	args := flag.Args()

	if *copy != "" {
		// copy exec to destination
		if err := CopyExec(*copy); err != nil {
			driverLog.Error(err, "failed to copy  binary")
			os.Exit(1)
			return
		}
		os.Exit(0)
		return
	}

	if len(args) == 0 {
		driverLog.Error(fmt.Errorf("no user program"), "empty args")
		os.Exit(1)
	}

	driverOpt := driver.DriverOption{
		Context:        ctx,
		JobNamespace:   *jobNameSpace,
		JobName:        *jobName,
		OutputPath:     *outputPath,
		GrpcService:    *grpcService,
		GrpcPort:       *grpcPort,
		DetectDevice:   *detectDevice,
		Logger:         driverLog,
		Args:           args,
		ReportInterval: *reportInterval,
	}

	driver, err := driver.NewDriver(&driverOpt)
	if err != nil {
		driverLog.Error(err, "Driver.Run failed")
		os.Exit(1)
	}

	var exitCode = 0
	if err := driver.Run(); err != nil {
		driverLog.Error(err, "Driver.Run failed")
		exitCode = 1
	}
	driver.Cleanup()
	os.Exit(exitCode)
}

func CopyExec(destination string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("copy this binary to %s: %w", destination, err)
		}
	}()

	path, err := findThisBinary()
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o555) // 0o555 -> readable and executable by all
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err = io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

func findThisBinary() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to file executable: %w", err)
	}
	return bin, nil
}
