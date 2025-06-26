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
	"errors"
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
	// from https://gist.github.com/andenacitelli/20a98f8c45499fe21d55266b776d3071 -> regex pattern to extract tqdm fields
	progressMsgPattern = regexp.MustCompile(`(.*): *(\d+%).*?(\d+\/\d+) +\[(\d+:\d+:?\d+)<(\d+:\d+:?\d+), +(\d+.\d+.*\/s)\]`)
)

const (
	// the default port for the driver to listen on
	DefaultPort            = 18080
	DefaultTaskRecipesPath = "/opt/app-root/src/my_tasks"
	DefaultCatalogPath     = "/opt/app-root/src/my_catalogs"
	TaskRecipePrefix       = "tr"
	CustomCardPrefix       = "custom"
	ShutdownURI            = "/Shutdown"
	GetStatusURI           = "/GetStatus"
	DefaultGitBranch       = "main"
)

type DriverOption struct {
	Context             context.Context
	OutputPath          string
	DetectDevice        bool
	TaskRecipesPath     string
	TaskRecipes         []string
	CatalogPath         string
	CustomArtifacts     []CustomArtifact
	Logger              logr.Logger
	Args                []string
	CommPort            int
	DownloadAssetsS3    bool
	CustomTaskGitURL    string
	CustomTaskGitBranch string
	CustomTaskGitCommit string
	CustomTaskGitPath   string
	TaskNames           []string
	AllowOnline         bool
}

type ArtifactType string

const (
	Card         ArtifactType = "card"
	Template     ArtifactType = "template"
	Metric       ArtifactType = "metric"
	Task         ArtifactType = "task"
	SystemPrompt ArtifactType = "system_prompt"
)

type CustomArtifact struct {
	Type  ArtifactType
	Name  string
	Value string
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
	port       int
}

type driverImpl struct {
	Option       *DriverOption
	lastProgress lmesv1alpha1.ProgressBar
	status       lmesv1alpha1.LMEvalJobStatus
	err          error
	comm         *driverComm
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

	if opt.CommPort == 0 {
		opt.CommPort = DefaultPort
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
	// TODO: Remove so that log is not printed again at job's completion
	// toConsole(filepath.Join(d.Option.OutputPath, "stderr.log"))

	d.updateCompleteStatus(execErr)

	// wait for shutdown signal then properly clean up the resources
	d.comm.wait4Sutdownload()
	d.comm.close()
	return d.err
}

func (d *driverImpl) GetStatus() (*lmesv1alpha1.LMEvalJobStatus, error) {
	client := &http.Client{}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/%s", d.Option.CommPort, GetStatusURI))
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
	client := &http.Client{}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/%s", d.Option.CommPort, ShutdownURI), "application/json", nil)
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

	d.Option.Args = patchDevice(d.Option.Args, matches[1] == "True")

	return nil
}

func (d *driverImpl) downloadS3Assets() error {
	if d == nil || !d.Option.DownloadAssetsS3 {
		return nil
	}

	fmt.Println("Downloading assets from S3")
	output, err := exec.Command(
		"python",
		"/opt/app-root/src/scripts/s3_downloader.py",
	).Output()
	fmt.Println(string(output))
	if err != nil {
		return fmt.Errorf("failed to download assets from S3: %v", err)
	}

	return nil
}

func patchDevice(args []string, hasCuda bool) []string {
	device := "cpu"
	if hasCuda {
		device = "cuda"
	}

	// Check if --device already exists
	for _, arg := range args {
		if arg == "--device" {
			return args // already has device specified
		}
	}

	// If we reach here, --device doesn't exist, so add it
	return append(args, "--device", device)
}

// Create a domain socket and use HTTP protocal to handle communication
func (d *driverImpl) setupComm() error {

	serve := http.NewServeMux()
	d.comm = &driverComm{
		server:     &http.Server{Handler: serve},
		connection: make(chan int),
		port:       d.Option.CommPort,
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

func (dc *driverComm) wait4Sutdownload() {
	<-dc.connection
}

func (dc *driverComm) serve() error {
	socket, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", dc.port))
	if err != nil {
		return err
	}

	return dc.server.Serve(socket)
}

func (dc *driverComm) close() {
	if dc.server != nil && dc.connection != nil {
		dc.server.Shutdown(context.Background())
		close(dc.connection)
	}
}

func (dc *driverComm) notifyShutdownWait() {
	dc.connection <- 1
}

type PodLogAndPipeWriter struct {
	pw *io.PipeWriter
}

func (f PodLogAndPipeWriter) Write(p []byte) (n int, err error) {
	// format lm-eval output to stdout and the pipewriter
	line := string(p)
	res, _ := fmt.Print(line)
	if _, err := f.pw.Write(p); err != nil {
		return 0, err
	}

	// carriage returns do not correctly display, so replace with newlines
	// carriage returns also break the pipewriter, so make sure we get newlines as well.
	if strings.ContainsAny(line, "\r") && !strings.ContainsAny(line, "\n") {
		fmt.Print("\n")
		if _, err := f.pw.Write([]byte("\n")); err != nil {
			return 0, err
		}
	}
	return res, nil
}

func (d *driverImpl) exec() error {
	// create Unitxt task recipes
	if err := d.createTaskRecipes(); err != nil {
		return fmt.Errorf("failed to create task recipes: %v", err)
	}

	if err := d.prepDir4CustomArtifacts(); err != nil {
		return fmt.Errorf("failed to create the directories for custom artifacts: %v", err)
	}

	if err := d.fetchGitCustomTasks(); err != nil {
		return fmt.Errorf("failed to set up custom tasks: %v", err)
	}

	// Copy S3 assets if needed
	if err := d.downloadS3Assets(); err != nil {
		return err
	}

	if err := d.createCustomArtifacts(); err != nil {
		return fmt.Errorf("failed to create custom artifact: %v", err)
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
	pr, pw := io.Pipe()
	mwriter := io.MultiWriter(stderr, PodLogAndPipeWriter{pw})
	scanner := bufio.NewScanner(pr)

	executor := exec.Command(d.Option.Args[0], args...)
	stdin, err := executor.StdinPipe()
	if err != nil {
		return err
	}
	executor.Stdout = stdout
	executor.Stderr = mwriter

	env := append(os.Environ(), "UNITXT_ALLOW_UNVERIFIED_CODE=True")

	executor.Env = env

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

func (d *driverImpl) updateProgressStatus(state lmesv1alpha1.JobState, reason lmesv1alpha1.Reason, latestProgress lmesv1alpha1.ProgressBar) {
	d.status.State = state
	d.status.Reason = reason
	d.status.Message = latestProgress.Message // for backwards compatibility

	progressLen := len(d.status.ProgressBars)

	// if no progress bars yet reported
	if progressLen == 0 {
		d.status.ProgressBars = append(d.status.ProgressBars, latestProgress)
	} else {
		last := &d.status.ProgressBars[progressLen-1]

		// are we updating an existing progress bar?
		if last.Message == latestProgress.Message {
			d.status.ProgressBars[progressLen-1] = latestProgress
		} else {
			// if it's a new bar, append it to the progress bar list
			d.status.ProgressBars = append(d.status.ProgressBars, latestProgress)
			// Ensure ProgressBars is never longer than 10 items
			if len(d.status.ProgressBars) > 10 {
				d.status.ProgressBars = d.status.ProgressBars[1:]
			}
		}
	}
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

	// gather tqdm fields
	if matches := progressMsgPattern.FindStringSubmatch(msglist[len(msglist)-1]); len(matches) == 7 {
		message := strings.Trim(matches[1], "\r")
		percent := strings.Trim(matches[2], "\r")
		count := strings.Trim(matches[3], "\r")
		elapsedTime := strings.Trim(matches[4], "\r")
		remainingTimeEstimate := strings.Trim(matches[5], "\r")

		// standardize time format
		if strings.Count(elapsedTime, ":") == 1 {
			elapsedTime = "0:" + elapsedTime
		}
		if strings.Count(remainingTimeEstimate, ":") == 1 {
			remainingTimeEstimate = "0:" + remainingTimeEstimate
		}

		newMessage := message != d.lastProgress.Message
		newPercent := percent != d.lastProgress.Percent
		newCount := count != d.lastProgress.Count

		// if any of the run percent, message, count has changed, update the CR status
		if newPercent || newCount || newMessage {
			d.lastProgress.Message = message
			d.lastProgress.Percent = percent
			d.lastProgress.Count = count
			d.lastProgress.ElapsedTime = elapsedTime
			d.lastProgress.RemainingTimeEstimate = remainingTimeEstimate

			d.updateProgressStatus(lmesv1alpha1.RunningJobState, lmesv1alpha1.NoReason, d.lastProgress)
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

func (d *driverImpl) prepDir4CustomArtifacts() error {
	subDirs := []string{"cards", "templates", "system_prompts"}
	var errs []error
	for _, dir := range subDirs {
		errs = append(errs, createDirectory(filepath.Join(d.Option.CatalogPath, dir)))
	}
	return errors.Join(errs...)
}

func (d *driverImpl) createCustomArtifacts() error {
	for _, customArtifact := range d.Option.CustomArtifacts {
		switch customArtifact.Type {
		case Card, Metric, Template, Task:
			if err := createCustomArtifact(filepath.Join(d.Option.CatalogPath, fmt.Sprintf("%ss", customArtifact.Type)),
				customArtifact.Name, customArtifact.Value); err != nil {
				return err
			}
		case SystemPrompt:
			if err := createCustomArtifact(filepath.Join(d.Option.CatalogPath, fmt.Sprintf("%ss", customArtifact.Type)),
				customArtifact.Name,
				fmt.Sprintf(`{ "__type__": "textual_system_prompt", "text": "%s" }`, customArtifact.Value)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid custom artifact type:%s", customArtifact.Type)
		}
	}
	return nil
}

func createCustomArtifact(rootDir, artifactName, artifactValue string) error {
	dirs := []string{rootDir}
	tokens := strings.Split(artifactName, ".")
	if len(tokens) > 1 {
		dirs = append(dirs, tokens[0:len(tokens)-1]...)
	}
	fullPath := filepath.Join(dirs...)
	if err := os.MkdirAll(fullPath, 0770); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(fullPath, fmt.Sprintf("%s.json", tokens[len(tokens)-1])),
		[]byte(artifactValue),
		0666,
	)
}

func createDirectory(path string) error {
	fi, err := os.Stat(path)
	if err == nil && !fi.IsDir() {
		return fmt.Errorf("%s is a file. can not create a directory", path)
	}
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0770)
	}
	return nil
}

func (d *driverImpl) fetchGitCustomTasks() error {
	// No-op if git url not set
	if d.Option.CustomTaskGitURL == "" {
		return nil
	}

	// If online is disabled, also disable fetching external tasks
	if !d.Option.AllowOnline {
		return fmt.Errorf("fetching external git tasks is not allowed when allowOnline is false")
	}

	repositoryDestination := filepath.Join("/tmp", "custom_tasks")
	if err := createDirectory(repositoryDestination); err != nil {
		return err
	}

	// #nosec G204 -- CustomTaskGitURL is validated by ValidateGitURL() in the controller
	cloneCommand := exec.Command("git", "clone", d.Option.CustomTaskGitURL, repositoryDestination)
	if output, err := cloneCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone git repository: %v, output: %s", err, string(output))
	}

	clonedDirectory := fmt.Sprintf("--git-dir=%s", filepath.Join(repositoryDestination, ".git"))
	workTree := fmt.Sprintf("--work-tree=%s", repositoryDestination)

	// Checkout a specific branch, if specified
	if d.Option.CustomTaskGitBranch != "" {
		// #nosec G204 -- CustomTaskGitBranch is validated by ValidateGitBranch() in the controller
		checkoutCommand := exec.Command("git", clonedDirectory, workTree, "checkout", d.Option.CustomTaskGitBranch)
		if output, err := checkoutCommand.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %v, output: %s",
				d.Option.CustomTaskGitBranch, err, string(output))
		}
	} else {
		// #nosec G204 -- DefaultGitBranch is a constant value, not user input
		checkoutCmd := exec.Command("git", clonedDirectory, workTree, "checkout", DefaultGitBranch)
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			d.Option.Logger.Info("failed to checkout main branch, using default branch from clone",
				"error", err, "output", string(output))
		}
	}

	// Checkout a specific commit, if specified
	if d.Option.CustomTaskGitCommit != "" {
		// #nosec G204 -- CustomTaskGitCommit is validated by ValidateGitCommit() in the controller
		checkoutCommand := exec.Command("git", clonedDirectory, workTree, "checkout", d.Option.CustomTaskGitCommit)
		if output, err := checkoutCommand.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to checkout commit %s: %v, output: %s",
				d.Option.CustomTaskGitCommit, err, string(output))
		}
	}

	// Use the specified repository path for copying
	taskPath := repositoryDestination
	if d.Option.CustomTaskGitPath != "" {
		taskPath = filepath.Join(repositoryDestination, d.Option.CustomTaskGitPath)
		if _, err := os.Stat(taskPath); os.IsNotExist(err) {
			return fmt.Errorf("specified path '%s' does not exist in the repository", d.Option.CustomTaskGitPath)
		}
	}

	// Create destination path for copy
	if err := createDirectory(d.Option.TaskRecipesPath); err != nil {
		return err
	}

	// #nosec G204 -- taskPath is derived from validated CustomTaskGitPath, TaskRecipesPath is controlled by the application
	copyCmd := exec.Command("cp", "-r", taskPath+"/.", d.Option.TaskRecipesPath)
	output, err := copyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to copy tasks to %s: %v, output: %s", d.Option.TaskRecipesPath, err, string(output))
	}

	return nil
}
