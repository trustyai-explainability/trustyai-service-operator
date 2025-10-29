package utils

import (
	"reflect"
)

type ResourceParserFunc[T any] func(templatePath string, data interface{}, outType reflect.Type) (*T, error)
