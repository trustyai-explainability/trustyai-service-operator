package templates

import (
	"embed"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed *.tmpl.yaml
var templateFS embed.FS

func ParseResource[T client.Object](templatePath string, data interface{}, outType reflect.Type) (T, error) {
	return utils.ParseResourceFromFS[T](templatePath, data, outType, templateFS)
}
