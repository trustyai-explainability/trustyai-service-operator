package utils

import (
	"bytes"
	"embed"
	"reflect"
)

import (
	"text/template"

	"sigs.k8s.io/yaml"
)

// executeTemplate parses the template file and executes it with the provided data.
func executeTemplate(templatePath string, data interface{}, templateFS embed.FS) (bytes.Buffer, error) {
	var processed bytes.Buffer
	tmpl, err := template.ParseFS(templateFS, templatePath)
	if err != nil {
		return processed, err
	}
	err = tmpl.Execute(&processed, data)
	return processed, err
}

// unmarshallResource unmarshal YAML bytes into the specified Kubernetes resource.
func unmarshallResource(yamlBytes []byte, outType reflect.Type) (interface{}, error) {
	outValue := reflect.New(outType.Elem()).Interface()
	err := yaml.Unmarshal(yamlBytes, outValue)
	return outValue, err
}

// ParseResource parses templates and return a provided Kubernetes resource.
func ParseResourceFromFS[T any](templatePath string, data interface{}, outType reflect.Type, templateFS embed.FS) (*T, error) {
	processed, err := executeTemplate(templatePath, data, templateFS)
	if err != nil {
		return nil, err
	}

	// Convert the processed bytes into the provided Kubernetes resource.
	result, err := unmarshallResource(processed.Bytes(), outType)
	if err != nil {
		return nil, err
	}

	return result.(*T), nil
}

type ResourceParserFunc[T any] func(templatePath string, data interface{}, outType reflect.Type) (*T, error)
