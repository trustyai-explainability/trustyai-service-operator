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
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/api/v1beta1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	scheme             = runtime.NewScheme()
	progressMegPattern = regexp.MustCompile(`^(.*?:\s*?\d*?%)\|`)
)

const (
	GrpcClientKeyEnv            = "GRPC_CLIENT_KEY"
	GrpcClientCertEnv           = "GRPC_CLIENT_CERT"
	GrpcServerCaEnv             = "GRPC_SERVER_CA"
	DefaultDriverReportInterval = time.Second * 10
	DefaultTaskRecipesPath      = "/opt/app-root/src/my_tasks"
	DefaultCatalogPath          = "/opt/app-root/src/my_catalogs"
	TaskRecipePrefix            = "tr"
	CustomCardPrefix            = "custom"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(lmesv1alpha1.AddToScheme(scheme))
}

type DriverOption struct {
	Context         context.Context
	JobNamespace    string
	JobName         string
	GrpcService     string
	GrpcPort        int
	OutputPath      string
	DetectDevice    bool
	TaskRecipesPath string
	TaskRecipes     []string
	CatalogPath     string
	CustomCards     []string
	Logger          logr.Logger
	Args            []string
	ReportInterval  time.Duration
}

type Driver interface {
	Run() error
	Cleanup()
}

type driverImpl struct {
	client          v1beta1.LMEvalJobUpdateServiceClient
	grpcConn        *grpc.ClientConn
	Option          *DriverOption
	lastReportTime  time.Time
	lastProgressMsg string
}

func NewDriver(opt *DriverOption) (Driver, error) {
	if opt == nil {
		return nil, nil
	}

	if opt.ReportInterval == 0 {
		opt.ReportInterval = DefaultDriverReportInterval
	}
	if opt.Context == nil {
		return nil, fmt.Errorf("context is nil")
	}

	if opt.JobNamespace == "" || opt.JobName == "" {
		return nil, fmt.Errorf("JobNamespace or JobName is empty")
	}

	if opt.TaskRecipesPath == "" {
		opt.TaskRecipesPath = DefaultTaskRecipesPath
	}

	if opt.CatalogPath == "" {
		opt.CatalogPath = DefaultCatalogPath
	}

	conn, err := getGRPCClientConn(opt)
	if err != nil {
		return nil, err
	}

	return &driverImpl{
		client:   v1beta1.NewLMEvalJobUpdateServiceClient(conn),
		grpcConn: conn,
		Option:   opt,
	}, nil
}

// Run implements Driver.
func (d *driverImpl) Run() error {
	if err := d.updateStatus(lmesv1alpha1.RunningJobState,
		"update status from the driver: running"); err != nil {

		return err
	}

	execErr := d.exec()

	// dump stderr and stdout to the console
	var toConsole = func(file string) {
		if data, err := os.ReadFile(file); err == nil {
			os.Stdout.Write(data)
		}
	}
	toConsole(filepath.Join(d.Option.OutputPath, "stdout.log"))
	toConsole(filepath.Join(d.Option.OutputPath, "stderr.log"))

	return d.updateCompleteStatus(execErr)
}

func (d *driverImpl) Cleanup() {
	if d != nil && d.grpcConn != nil {
		d.grpcConn.Close()
	}
}

func getGRPCClientConn(option *DriverOption) (clientConn *grpc.ClientConn, err error) {
	// Set up a connection to the server.
	if option.GrpcPort == 0 || option.GrpcService == "" {
		return nil, fmt.Errorf("GrpcService or GrpcPort is not valid")
	}

	serverAddr := fmt.Sprintf("%s:%d", option.GrpcService, option.GrpcPort)

	if viper.IsSet(GrpcServerCaEnv) {
		serverCAPath := viper.GetString(GrpcServerCaEnv)

		if viper.IsSet(GrpcClientCertEnv) && viper.IsSet(GrpcClientKeyEnv) {
			// mTLS
			certPath, keyPath := viper.GetString(GrpcClientCertEnv), viper.GetString(GrpcClientKeyEnv)
			var cert tls.Certificate
			cert, err = tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				return nil, err
			}

			ca := x509.NewCertPool()
			var caBytes []byte
			caBytes, err = os.ReadFile(serverCAPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read server CA %q: %v", serverCAPath, err)
			}
			if ok := ca.AppendCertsFromPEM(caBytes); !ok {
				return nil, fmt.Errorf("failed to parse server CA %q", serverCAPath)
			}

			tlsConfig := &tls.Config{
				ServerName:   serverAddr,
				Certificates: []tls.Certificate{cert},
				RootCAs:      ca,
			}

			clientConn, err = grpc.NewClient(serverAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		} else {
			// TLS
			creds, err := credentials.NewClientTLSFromFile(serverCAPath, serverAddr)
			if err != nil {
				return nil, fmt.Errorf("failed to load server CA: %v", err)
			}

			clientConn, err = grpc.NewClient(serverAddr, grpc.WithTransportCredentials(creds))
			if err != nil {
				return nil, fmt.Errorf("failed to connect to GRPC server: %v", err)
			}
		}
	} else {
		clientConn, err = grpc.NewClient(
			serverAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	return
}

func (d *driverImpl) detectDevice() error {
	if d == nil || !d.Option.DetectDevice {
		return nil
	}

	// assuming python and torch python package are available.
	// use torch python API to detect CUDA's availability
	out, err := exec.Command(
		"python",
		"-c",
		"import torch; print('=={}:{}=='.format(torch.cuda.is_available(), torch.cuda.device_count()));",
	).Output()
	if err != nil {
		return fmt.Errorf("failed to detect available device(s): %v", err)
	}

	re := regexp.MustCompile(`(?m)^==(True|False):(\d+?)==$`)
	matches := re.FindStringSubmatch(string(out))
	if matches == nil {
		return fmt.Errorf("failed to find the matched output")
	}

	patchDevice(d.Option.Args, matches[1] == "True")

	return nil
}

func patchDevice(args []string, hasCuda bool) {
	var device = "cpu"
	if hasCuda {
		device = "cuda"
	}
	// patch the python command in the Option.Arg by adding the `--device cuda` option
	// find the string with the `python -m lm_eval` prefix. usually it should be the last one
	for idx, arg := range args {
		if strings.HasPrefix(arg, "python -m lm_eval") {
			if !strings.Contains(arg, "--device") {
				args[idx] = fmt.Sprintf("%s --device %s", arg, device)
			}
			break
		}
	}
}

func (d *driverImpl) exec() error {
	// create Unitxt task recipes
	if err := d.createTaskRecipes(); err != nil {
		return fmt.Errorf("failed to create task recipes: %v", err)
	}

	if err := d.createCustomCards(); err != nil {
		return fmt.Errorf("failed to create custom cards: %v", err)
	}

	// Detect available devices if needed
	if err := d.detectDevice(); err != nil {
		return err
	}

	// Run user program.
	var args []string
	if len(d.Option.Args) > 1 {
		args = d.Option.Args[1:]
	}

	stdout, err := os.Create(filepath.Join(d.Option.OutputPath, "stdout.log"))
	if err != nil {
		return err
	}
	bout := bufio.NewWriter(stdout)

	stderr, err := os.Create(filepath.Join(d.Option.OutputPath, "stderr.log"))
	if err != nil {
		return err
	}

	// have a pipe to check the output and report progress
	// lm-eval's outputs are in the stderr
	pr, pw := io.Pipe()
	mwriter := io.MultiWriter(stderr, pw)
	berr := bufio.NewWriter(mwriter)
	scanner := bufio.NewScanner(pr)

	executor := exec.Command(d.Option.Args[0], args...)
	stdin, err := executor.StdinPipe()
	if err != nil {
		return err
	}
	executor.Stdout = bout
	executor.Stderr = berr
	executor.Env = append(os.Environ(),
		"UNITXT_ALLOW_UNVERIFIED_CODE=True",
	)

	var freeRes = func() {
		stdin.Close()
		bout.Flush()
		stdout.Sync()
		stdout.Close()
		berr.Flush()
		stderr.Sync()
		stderr.Close()
		pr.Close()
	}

	// temporally fix the trust_remote_code issue
	io.WriteString(stdin, "y\n")
	if err := executor.Start(); err != nil {
		freeRes()
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for scanner.Scan() {
			msg := scanner.Text()
			if err := d.reportProgress(msg); err != nil {
				d.Option.Logger.Error(err, "report progress failed")
			}
		}
		wg.Done()
	}()

	finalError := executor.Wait()
	freeRes()
	wg.Wait()
	return finalError
}

func (d *driverImpl) updateStatus(state lmesv1alpha1.JobState, msg string) error {
	ctx, cancel := context.WithTimeout(d.Option.Context, time.Second*10)
	defer cancel()

	r, err := d.client.UpdateStatus(ctx, &v1beta1.JobStatus{
		JobName:       d.Option.JobName,
		JobNamespace:  d.Option.JobNamespace,
		State:         string(state),
		Reason:        string(lmesv1alpha1.NoReason),
		StatusMessage: msg,
	})

	if r != nil && err == nil {
		d.Option.Logger.Info(fmt.Sprintf("UpdateStatus done: %s", r.Message))
		d.lastReportTime = time.Now()
	}

	return err
}

func (d *driverImpl) updateCompleteStatus(err error) error {
	ctx, cancel := context.WithTimeout(d.Option.Context, time.Second*10)
	defer cancel()
	newStatus := v1beta1.JobStatus{
		JobName:       d.Option.JobName,
		JobNamespace:  d.Option.JobNamespace,
		State:         string(lmesv1alpha1.CompleteJobState),
		Reason:        string(lmesv1alpha1.SucceedReason),
		StatusMessage: "update status from the driver: completed",
	}

	var setErr = func(err error) {
		newStatus.Reason = string(lmesv1alpha1.FailedReason)
		newStatus.StatusMessage = err.Error()
	}

	if err != nil {
		setErr(err)
	} else {
		results, err := d.getResults()
		if err != nil {
			setErr(err)
		} else {
			newStatus.Results = &results
		}
	}

	r, err := d.client.UpdateStatus(ctx, &newStatus)
	if r != nil && err == nil {
		d.Option.Logger.Info(fmt.Sprintf("UpdateStatus with the results: %s", r.Message))
	}

	return err
}

func (d *driverImpl) getResults() (string, error) {
	var results string
	pattern := "*result*.json"
	if err := filepath.WalkDir(d.Option.OutputPath, func(path string, dir fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			bytes, err := os.ReadFile(path)
			if err != nil {
				d.Option.Logger.Error(err, "failed to retrieve the results")
			} else {
				results = string(bytes)
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	return results, nil
}

func (d *driverImpl) reportProgress(msg string) error {
	msg = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		// replace control chars to new line
		if unicode.IsControl(r) {
			return 10
		}
		return -1
	}, msg)

	// get multiple lines and only use the last one
	msglist := strings.Split(msg, "\n")

	if matches := progressMegPattern.FindStringSubmatch(msglist[len(msglist)-1]); len(matches) == 2 {
		if matches[1] != d.lastProgressMsg {
			d.lastProgressMsg = strings.Trim(matches[1], " \r")
		}
	}
	if time.Since(d.lastReportTime) >= d.Option.ReportInterval {
		if err := d.updateStatus(lmesv1alpha1.RunningJobState, d.lastProgressMsg); err != nil {
			return err
		}
	}
	return nil
}

func (d *driverImpl) createTaskRecipes() error {
	for i, taskRecipe := range d.Option.TaskRecipes {
		err := os.WriteFile(
			filepath.Join(d.Option.TaskRecipesPath, fmt.Sprintf("%s_%d.yaml", TaskRecipePrefix, i)),
			[]byte(fmt.Sprintf(
				"task: %s\ninclude: unitxt\nrecipe: %s",
				fmt.Sprintf("%s_%d", TaskRecipePrefix, i),
				taskRecipe,
			)),
			0666,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *driverImpl) createCustomCards() error {
	for i, customCard := range d.Option.CustomCards {
		err := os.WriteFile(
			filepath.Join(d.Option.CatalogPath, "cards", fmt.Sprintf("%s_%d.json", CustomCardPrefix, i)),
			[]byte(customCard),
			0666,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
