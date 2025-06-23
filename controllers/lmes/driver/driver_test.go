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
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
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

type testInfo struct {
	outputPath  string
	catalogPath string
	taskPath    string
	port        int
	tearDown    func(*testing.T)
}

func setupTest(t *testing.T, hasOutput bool) testInfo {
	folderSuffix := rand.Intn(100)
	outputPath := fmt.Sprintf("outputs%d", folderSuffix)
	catalogPath := fmt.Sprintf("mycatalogs%d", folderSuffix)
	taskPath := fmt.Sprintf("mytasks%d", folderSuffix)
	os.Mkdir(outputPath, 0750)
	os.Mkdir(catalogPath, 0750)
	os.Mkdir(taskPath, 0750)

	testInfo := testInfo{
		outputPath:  outputPath,
		catalogPath: catalogPath,
		taskPath:    taskPath,
		port:        rand.Intn(1000) + 18080,
		tearDown: func(t *testing.T) {
			if hasOutput {
				assert.Nil(t, os.Remove(filepath.Join(outputPath, "stderr.log")))
				assert.Nil(t, os.Remove(filepath.Join(outputPath, "stdout.log")))
			}
			assert.Nil(t, os.RemoveAll(taskPath))
			assert.Nil(t, os.RemoveAll(outputPath))
			assert.Nil(t, os.RemoveAll(catalogPath))
		},
	}
	return testInfo
}

func runDriverAndWait4Complete(t *testing.T, driver Driver, returnError bool) (progressMsgs []string, results string) {
	go func() {
		if returnError {
			assert.NotNil(t, driver.Run())
		} else {
			assert.Nil(t, driver.Run())
		}
	}()

	for {
		time.Sleep(time.Second)
		status, err := driver.GetStatus()
		assert.Nil(t, err)
		if len(progressMsgs) == 0 || progressMsgs[len(progressMsgs)-1] != status.Message {
			progressMsgs = append(progressMsgs, status.Message)
		}
		if status.State == v1alpha1.CompleteJobState {
			results = status.Results
			break
		}
	}
	return progressMsgs, results
}

func Test_Driver(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:     context.Background(),
		OutputPath:  info.outputPath,
		CatalogPath: info.catalogPath,
		Logger:      driverLog,
		Args:        []string{"sh", "-ec", "echo tttttttttttttttttttt"},
		CommPort:    info.port,
	})
	assert.Nil(t, err)
	runDriverAndWait4Complete(t, driver, false)
	assert.Nil(t, driver.Shutdown())
}

func Test_Wait4Shutdown(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:     context.Background(),
		OutputPath:  info.outputPath,
		CatalogPath: info.catalogPath,
		Logger:      driverLog,
		Args:        []string{"sh", "-ec", "echo test"},
		CommPort:    info.port,
	})
	assert.Nil(t, err)

	runDriverAndWait4Complete(t, driver, false)

	// can still get the status even the user program finishes
	time.Sleep(time.Second * 3)
	status, err := driver.GetStatus()
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.CompleteJobState, status.State)

	assert.Nil(t, driver.Shutdown())

	_, err = driver.GetStatus()
	assert.ErrorContains(t, err, "connection refused")
}

func Test_ProgressUpdate(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:     context.Background(),
		OutputPath:  info.outputPath,
		CatalogPath: info.catalogPath,
		Logger:      driverLog,
		Args:        []string{"sh", "-ec", "sleep 2; echo 'testing progress: 100%|' >&2; sleep 4"},
		CommPort:    info.port,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, false)

	assert.Equal(t, []string{
		"initializing the evaluation job",
		"testing progress: 100%",
		"job completed",
	}, msgs)

	assert.Nil(t, driver.Shutdown())
}

func Test_DetectDeviceError(t *testing.T) {
	info := setupTest(t, false)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:      context.Background(),
		OutputPath:   info.outputPath,
		CatalogPath:  info.catalogPath,
		DetectDevice: true,
		Logger:       driverLog,
		Args:         []string{"sh", "-ec", "python -m lm_eval --output_path ./output --model test --model_args arg1=value1 --tasks task1,task2"},
		CommPort:     info.port,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, true)
	assert.Equal(t, []string{
		"failed to detect available device(s): exit status 1",
	}, msgs)

	assert.Nil(t, driver.Shutdown())

	// the following files don't exist for this case
	assert.NotNil(t, os.Remove(filepath.Join(info.outputPath, "stderr.log")))
	assert.NotNil(t, os.Remove(filepath.Join(info.outputPath, "stdout.log")))
}

func Test_DownloadAssetsS3Error(t *testing.T) {
	info := setupTest(t, false)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:          context.Background(),
		OutputPath:       info.outputPath,
		CatalogPath:      info.catalogPath,
		DetectDevice:     false,
		Logger:           driverLog,
		Args:             []string{"sh", "-ec", "python -m lm_eval --output_path ./output --model test --model_args arg1=value1 --tasks task1,task2"},
		CommPort:         info.port,
		DownloadAssetsS3: true,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, true)
	assert.Equal(t, []string{
		"failed to download assets from S3: exit status 2",
	}, msgs)

	assert.Nil(t, driver.Shutdown())
}

func Test_PatchDevice(t *testing.T) {
	driverOpt := DriverOption{
		Context:      context.Background(),
		OutputPath:   ".",
		DetectDevice: true,
		Logger:       driverLog,
		Args:         []string{"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2"},
	}

	// append `--device cuda`
	driverOpt.Args = patchDevice(driverOpt.Args, true)
	expected := []string{"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--device", "cuda"}
	assert.Equal(t, expected, driverOpt.Args)

	// append `--device cpu`
	driverOpt.Args = []string{"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2"}
	driverOpt.Args = patchDevice(driverOpt.Args, false)
	expected = []string{"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--device", "cpu"}
	assert.Equal(t, expected, driverOpt.Args)

	// no change because `--device` already exists
	driverOpt.Args = []string{"python", "-m", "lm_eval", "--device", "cpu", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2"}
	originalArgs := make([]string, len(driverOpt.Args))
	copy(originalArgs, driverOpt.Args)
	driverOpt.Args = patchDevice(driverOpt.Args, true)
	assert.Equal(t, originalArgs, driverOpt.Args)
}

func Test_TaskRecipes(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:         context.Background(),
		OutputPath:      info.outputPath,
		CatalogPath:     info.catalogPath,
		Logger:          driverLog,
		TaskRecipesPath: info.taskPath,
		TaskRecipes: []string{
			"card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
			"card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		},
		Args:     []string{"sh", "-ec", "sleep 2; echo 'testing progress: 100%|' >&2; sleep 4"},
		CommPort: info.port,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, false)

	assert.Equal(t, []string{
		"initializing the evaluation job",
		"testing progress: 100%",
		"job completed",
	}, msgs)

	assert.Nil(t, driver.Shutdown())

	tr0, err := os.ReadFile(filepath.Join(info.taskPath, "tr_0.yaml"))
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_0\ninclude: unitxt\nrecipe: card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr0),
	)
	tr1, err := os.ReadFile(filepath.Join(info.taskPath, "tr_1.yaml"))
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_1\ninclude: unitxt\nrecipe: card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr1),
	)
}

func Test_CustomCards(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:         context.Background(),
		OutputPath:      info.outputPath,
		Logger:          driverLog,
		TaskRecipesPath: info.taskPath,
		CatalogPath:     info.catalogPath,
		TaskRecipes: []string{
			"card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
			"card=cards.unitxt.card1,template=templates.mytemplate.tp_0,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		},
		CustomArtifacts: []CustomArtifact{
			{Type: Card, Name: "custom_0", Value: `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`},
			{Type: Template, Name: "mytemplate.tp_0", Value: `{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`},
			{Type: SystemPrompt, Name: "sp_0", Value: "this is a custom system prompt"},
			{Type: Metric, Name: "llm_as_judge.rating.mistral_7b_instruct_v0_2_huggingface_template_mt_bench_single_turn", Value: `{"__type__": "llm_as_judge", "inference_model": { "__type__": "hf_pipeline_based_inference_engine", "model_name": "mistralai/Mistral-7B-Instruct-v0.2", "max_new_tokens": 256, "use_fp16": true }, "template": "templates.response_assessment.rating.mt_bench_single_turn", "task": "rating.single_turn", "format": "formats.models.mistral.instruction", "main_score": "mistral_7b_instruct_v0_2_huggingface_template_mt_bench_single_turn"}`},
			{Type: Task, Name: "generation", Value: `{ "__type__": "task", "input_fields": { "input": "str", "type_of_input": "str", "type_of_output": "str" }, "reference_fields": { "output": "str" }, "prediction_type": "str", "metrics": [ "metrics.normalized_sacrebleu" ], "augmentable_inputs": [ "input" ], "defaults": { "type_of_output": "Text" } }`},
		},
		Args:     []string{"sh", "-ec", "sleep 1; echo 'testing progress: 100%|' >&2; sleep 3"},
		CommPort: info.port,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, false)

	assert.Equal(t, []string{
		"initializing the evaluation job",
		"testing progress: 100%",
		"job completed",
	}, msgs)

	assert.Nil(t, driver.Shutdown())

	tr0, err := os.ReadFile(filepath.Join(info.taskPath, "tr_0.yaml"))
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_0\ninclude: unitxt\nrecipe: card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr0),
	)
	tr1, err := os.ReadFile(filepath.Join(info.taskPath, "tr_1.yaml"))
	assert.Nil(t, err)
	assert.Equal(t,
		"task: tr_1\ninclude: unitxt\nrecipe: card=cards.unitxt.card1,template=templates.mytemplate.tp_0,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		string(tr1),
	)
	custom0, err := os.ReadFile(filepath.Join(info.catalogPath, "cards", "custom_0.json"))
	assert.Nil(t, err)
	assert.Equal(t,
		`{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		string(custom0),
	)
	template0, err := os.ReadFile(filepath.Join(info.catalogPath, "templates", "mytemplate", "tp_0.json"))
	assert.Nil(t, err)
	assert.Equal(t,
		`{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		string(template0),
	)
	prompt0, err := os.ReadFile(filepath.Join(info.catalogPath, "system_prompts", "sp_0.json"))
	assert.Nil(t, err)
	assert.Equal(t,
		`{ "__type__": "textual_system_prompt", "text": "this is a custom system prompt" }`,
		string(prompt0),
	)
	// multi-level custom metric
	metric, err := os.ReadFile(filepath.Join(info.catalogPath, "metrics", "llm_as_judge", "rating", "mistral_7b_instruct_v0_2_huggingface_template_mt_bench_single_turn.json"))
	assert.Nil(t, err)
	assert.Equal(t,
		`{"__type__": "llm_as_judge", "inference_model": { "__type__": "hf_pipeline_based_inference_engine", "model_name": "mistralai/Mistral-7B-Instruct-v0.2", "max_new_tokens": 256, "use_fp16": true }, "template": "templates.response_assessment.rating.mt_bench_single_turn", "task": "rating.single_turn", "format": "formats.models.mistral.instruction", "main_score": "mistral_7b_instruct_v0_2_huggingface_template_mt_bench_single_turn"}`,
		string(metric),
	)
	//task
	task, err := os.ReadFile(filepath.Join(info.catalogPath, "tasks", "generation.json"))
	assert.Nil(t, err)
	assert.Equal(t,
		`{ "__type__": "task", "input_fields": { "input": "str", "type_of_input": "str", "type_of_output": "str" }, "reference_fields": { "output": "str" }, "prediction_type": "str", "metrics": [ "metrics.normalized_sacrebleu" ], "augmentable_inputs": [ "input" ], "defaults": { "type_of_output": "Text" } }`,
		string(task),
	)
}

func Test_ProgramError(t *testing.T) {
	info := setupTest(t, true)
	defer info.tearDown(t)

	driver, err := NewDriver(&DriverOption{
		Context:     context.Background(),
		OutputPath:  info.outputPath,
		CatalogPath: info.catalogPath,
		Logger:      driverLog,
		Args:        []string{"sh", "-ec", "sleep 1; exit 1"},
		CommPort:    info.port,
	})
	assert.Nil(t, err)

	msgs, _ := runDriverAndWait4Complete(t, driver, true)

	assert.Equal(t, []string{
		"initializing the evaluation job",
		"exit status 1",
	}, msgs)

	assert.Nil(t, driver.Shutdown())
}
