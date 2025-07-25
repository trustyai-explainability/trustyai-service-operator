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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/spf13/viper"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
)

const (
	OutputPath = "/opt/app-root/src/output"
)

type strArrayArg []string

func (t *strArrayArg) Set(value string) error {
	*t = append(*t, value)
	return nil
}

func (t *strArrayArg) String() string {
	// supposedly, use ":" as the separator for task recipe should be safe
	return strings.Join(*t, ":")
}

var (
	taskRecipes         strArrayArg
	customArtifactArgs  strArrayArg
	taskNames           strArrayArg
	copy                = flag.String("copy", "", "copy this binary to specified destination path")
	getStatus           = flag.Bool("get-status", false, "Get current status")
	shutdown            = flag.Bool("shutdown", false, "Shutdown the driver")
	outputPath          = flag.String("output-path", OutputPath, "output path")
	detectDevice        = flag.Bool("detect-device", false, "detect available device(s), CUDA or CPU")
	commPort            = flag.Int("listen-port", driver.DefaultPort, "driver serves APIs on the port")
	downloadAssetsS3    = flag.Bool("download-assets-s3", false, "Download assets from S3")
	uploadToOCI         = flag.Bool("upload-to-oci", false, "Upload results to OCI registry")
	customTaskGitURL    = flag.String("custom-task-git-url", "", "Git repository URL for custom tasks")
	customTaskGitBranch = flag.String("custom-task-git-branch", "", "Git repository branch for custom tasks")
	customTaskGitCommit = flag.String("custom-task-git-commit", "", "Git commit for custom tasks")
	customTaskGitPath   = flag.String("custom-task-git-path", "", "Custom task path")
	allowOnline         = flag.Bool("allow-online", false, "Allow LMEval online access")
	driverLog           = ctrl.Log.WithName("driver")
)

func init() {
	flag.Var(&taskRecipes, "task-recipe", "task recipe")
	flag.Var(&customArtifactArgs, "custom-artifact", "A string contains an artifact's type, name and value. Use | as separator")
	flag.Var(&taskNames, "task-name", "A task name for custom tasks")
}

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
		if err := copyExec(*copy); err != nil {
			driverLog.Error(err, "failed to copy  binary")
			os.Exit(1)
			return
		}
		os.Exit(0)
		return
	}

	if *getStatus {
		getStatusOrDie(ctx)
		return
	}

	if *shutdown {
		shutdownOrDie(ctx)
		return
	}

	if len(args) == 0 {
		driverLog.Error(fmt.Errorf("no user program"), "empty args")
		os.Exit(1)
	}

	customArtifacts := make([]driver.CustomArtifact, 0, len(customArtifactArgs))
	for _, artifact := range customArtifactArgs {
		values := strings.SplitN(artifact, "|", 3)
		customArtifacts = append(customArtifacts, driver.CustomArtifact{Type: driver.ArtifactType(values[0]), Name: values[1], Value: values[2]})
	}

	driverOpt := driver.DriverOption{
		Context:             ctx,
		OutputPath:          *outputPath,
		DetectDevice:        *detectDevice,
		Logger:              driverLog,
		TaskRecipes:         taskRecipes,
		CustomArtifacts:     customArtifacts,
		Args:                args,
		CommPort:            *commPort,
		DownloadAssetsS3:    *downloadAssetsS3,
		UploadToOCI:         *uploadToOCI,
		CustomTaskGitURL:    *customTaskGitURL,
		CustomTaskGitBranch: *customTaskGitBranch,
		CustomTaskGitCommit: *customTaskGitCommit,
		CustomTaskGitPath:   *customTaskGitPath,
		TaskNames:           taskNames,
		AllowOnline:         *allowOnline,
	}

	driver, err := driver.NewDriver(&driverOpt)
	if err != nil {
		driverLog.Error(err, "Driver.NewDriver failed")
		os.Exit(1)
	}

	var exitCode = 0
	if err := driver.Run(); err != nil {
		driverLog.Error(err, "Driver.Run failed")
		exitCode = 1
	}
	os.Exit(exitCode)
}

func copyExec(destination string) (err error) {
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

func getStatusOrDie(ctx context.Context) {
	driver, err := driver.NewDriver(&driver.DriverOption{
		Context:      ctx,
		OutputPath:   *outputPath,
		DetectDevice: *detectDevice,
		Logger:       driverLog,
	})

	if err != nil {
		driverLog.Error(err, "failed to initialize the driver")
		os.Exit(1)
	}

	status, err := driver.GetStatus()
	if err != nil {
		driverLog.Error(err, "failed to get status", "error", err.Error())
		os.Exit(1)
	}

	b, err := json.Marshal(status)
	if err != nil {
		driverLog.Error(err, "json serialization failed", "error", err.Error())
		os.Exit(1)
	}

	fmt.Print(string(b))
	os.Exit(0)
}

func shutdownOrDie(ctx context.Context) {
	driver, err := driver.NewDriver(&driver.DriverOption{
		Context:      ctx,
		OutputPath:   *outputPath,
		DetectDevice: *detectDevice,
		Logger:       driverLog,
	})

	if err != nil {
		driverLog.Error(err, "failed to initialize the driver")
		os.Exit(1)
	}

	err = driver.Shutdown()
	if err != nil {
		driverLog.Error(err, "failed to shutdown", "error", err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
