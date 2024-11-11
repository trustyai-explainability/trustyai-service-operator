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
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/go-logr/logr"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
)

var (
	progressMegPattern = regexp.MustCompile(`^(.*?:\s*?\d*?%)\|`)
)

const (
	// put the domain socket under /tmp. may move to emptydir to share across containers
	socketPath             = "/tmp/ta-lmes-driver.sock"
	DefaultTaskRecipesPath = "/opt/app-root/src/my_tasks"
	DefaultCatalogPath     = "/opt/app-root/src/my_catalogs"
	TaskRecipePrefix       = "tr"
	CustomCardPrefix       = "custom"
	ShutdownURI            = "/Shutdown"
	GetStatusURI           = "/GetStatus"
)

type DriverOption struct {
	Context         context.Context
	OutputPath      string
	DetectDevice    bool
	TaskRecipesPath string
	TaskRecipes     []string
	CatalogPath     string
	CustomCards     []string
	Logger          logr.Logger
	Args            []string
	SocketPath      string
}

type Driver interface {
	Run() error
	GetStatus() (*lmesv1alpha1.LMEvalJobStatus, error)
	Shutdown() error
}

// the communication server that is used by the driverImpl to
// send and recive messages using a domain socket
type driverComm struct {
	connection chan int
	server     *http.Server
	path       string
}

type driverImpl struct {
	Option          *DriverOption
	lastProgressMsg string
	status          lmesv1alpha1.LMEvalJobStatus
	err             error
	comm            *driverComm
}

func NewDriver(opt *DriverOption) (Driver, error) {
	if opt == nil {
		return nil, nil
	}

	if opt.Context == nil {
		return nil, fmt.Errorf("context is nil")
	}

	if opt.TaskRecipesPath == "" {
		opt.TaskRecipesPath = DefaultTaskRecipesPath
	}

	if opt.CatalogPath == "" {
		opt.CatalogPath = DefaultCatalogPath
	}

	if opt.SocketPath == "" {
		opt.SocketPath = socketPath
	}

	return &driverImpl{
		Option: opt,
	}, nil
}

// Run implements Driver.
func (d *driverImpl) Run() error {
	d.updateStatus(lmesv1alpha1.RunningJobState, lmesv1alpha1.NoReason, "initializing the evaluation job")

	if err := d.setupComm(); err != nil {
		d.err = err
		return d.err
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

	d.updateCompleteStatus(execErr)

	// wait for shutdown signal then properly clean up the resources
	d.comm.wait4Sutdownload()
	d.comm.close()
	return d.err
}

func (d *driverImpl) GetStatus() (*lmesv1alpha1.LMEvalJobStatus, error) {
	client := createClient(d.Option.SocketPath)
	resp, err := client.Get(fmt.Sprintf("http://unix%s", GetStatusURI))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var status lmesv1alpha1.LMEvalJobStatus
	err = json.Unmarshal(content, &status)

	return &status, err
}
func (d *driverImpl) Shutdown() error {
	client := createClient(d.Option.SocketPath)
	resp, err := client.Post(fmt.Sprintf("http://unix%s", ShutdownURI), "application/json", nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	return err
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

// Create a domain socket and use HTTP protocal to handle communication
func (d *driverImpl) setupComm() error {

	serve := http.NewServeMux()
	d.comm = &driverComm{
		server:     &http.Server{Handler: serve},
		connection: make(chan int),
		path:       d.Option.SocketPath,
	}

	// handle the `GetStatus` API: return the complete lmesv1alpha1.LMEvalJobStatus
	// or error if the JSON marshaling fails.
	serve.HandleFunc(GetStatusURI, func(w http.ResponseWriter, _ *http.Request) {
		status, err := json.Marshal(d.status)
		if err == nil {
			w.Write(status)
		} else {
			w.Write([]byte(fmt.Sprintf(`{"err": "%s"}`, err.Error())))
		}
	})

	// handle the `Shutdown` API: tear down the communication server.
	serve.HandleFunc(ShutdownURI, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"msg": "ok"}`))
		d.comm.notifyShutdownWait()
	})

	go func() {
		d.comm.serve()
	}()

	return nil
}

func createClient(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", path)
			},
		},
	}
}

func (dc *driverComm) wait4Sutdownload() {
	<-dc.connection
}

func (dc *driverComm) serve() error {
	socket, err := net.Listen("unix", dc.path)
	if err != nil {
		return err
	}

	return dc.server.Serve(socket)
}

func (dc *driverComm) close() {
	if dc.server != nil && dc.connection != nil {
		dc.server.Shutdown(context.Background())
		close(dc.connection)
		os.Remove(dc.path)
	}
}

func (dc *driverComm) notifyShutdownWait() {
	dc.connection <- 1
}

type FormattedWriter struct{}

func (f FormattedWriter) Write(p []byte) (n int, err error) {
	// format lm-eval output to stdout
	line := string(p)
	res, err := fmt.Print(line)

	// carriage returns do not correctly display, so replace with newlines
	if strings.ContainsAny(line, "\r") && !strings.ContainsAny(line, "\n") {
		fmt.Print("\n")
	}
	return res, err
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

	stderr, err := os.Create(filepath.Join(d.Option.OutputPath, "stderr.log"))
	if err != nil {
		return err
	}

	// have a pipe to check the output and report progress
	// lm-eval's outputs are in the stderr
	pr, _ := io.Pipe()
	mwriter := io.MultiWriter(stderr, FormattedWriter{})
	scanner := bufio.NewScanner(pr)

	executor := exec.Command(d.Option.Args[0], args...)
	stdin, err := executor.StdinPipe()
	if err != nil {
		return err
	}
	executor.Stdout = stdout
	executor.Stderr = mwriter
	executor.Env = append(os.Environ(),
		"UNITXT_ALLOW_UNVERIFIED_CODE=True",
	)

	var freeRes = func() {
		stdin.Close()
		stdout.Sync()
		stdout.Close()
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
			d.updateProgress(msg)
		}
		wg.Done()
	}()

	finalError := executor.Wait()
	freeRes()
	wg.Wait()
	return finalError
}

func (d *driverImpl) updateCompleteStatus(err error) {
	d.status.State = lmesv1alpha1.CompleteJobState
	d.status.Reason = lmesv1alpha1.SucceedReason
	d.status.Message = "job completed"

	if err == nil {
		var results string
		results, err = d.getResults()
		d.status.Results = results
	}

	if err != nil {
		d.status.Reason = lmesv1alpha1.FailedReason
		d.status.Message = err.Error()
		d.err = err
	}

	d.Option.Logger.Info("update status: job completed", "state", d.status)
}

func (d *driverImpl) updateStatus(state lmesv1alpha1.JobState, reason lmesv1alpha1.Reason, msg string) {
	d.status.State = state
	d.status.Reason = reason
	d.status.Message = msg
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

func (d *driverImpl) updateProgress(msg string) {
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
			d.updateStatus(lmesv1alpha1.RunningJobState, lmesv1alpha1.NoReason, d.lastProgressMsg)
		}
	}
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
