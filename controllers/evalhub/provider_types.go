package evalhub

// Types copied from github.com/eval-hub/eval-hub/pkg/api to avoid a direct module dependency.
// Source: https://github.com/eval-hub/eval-hub/blob/27b5bde450cb383ed92c78b13218a208c2b3a073/pkg/api/

// ProviderResource represents a provider configuration.
type ProviderResource struct {
	ID          string              `mapstructure:"id" yaml:"id" json:"id"`
	Name        string              `mapstructure:"name" yaml:"name" json:"name"`
	Description string              `mapstructure:"description" yaml:"description" json:"description"`
	Type        string              `mapstructure:"type" yaml:"type" json:"type"`
	Benchmarks  []BenchmarkResource `mapstructure:"benchmarks" yaml:"benchmarks" json:"benchmarks"`
	Runtime     *Runtime            `mapstructure:"runtime" yaml:"runtime" json:"-"`
}

// BenchmarkResource represents a benchmark configuration.
type BenchmarkResource struct {
	ID          string   `mapstructure:"id" yaml:"id" json:"id"`
	ProviderId  *string  `mapstructure:"provider_id" yaml:"provider_id" json:"provider_id,omitempty"`
	Name        string   `mapstructure:"name" yaml:"name" json:"name"`
	Description string   `mapstructure:"description" yaml:"description" json:"description"`
	Category    string   `mapstructure:"category" yaml:"category" json:"category"`
	Metrics     []string `mapstructure:"metrics" yaml:"metrics" json:"metrics"`
	NumFewShot  int      `mapstructure:"num_few_shot" yaml:"num_few_shot" json:"num_few_shot"`
	DatasetSize int      `mapstructure:"dataset_size" yaml:"dataset_size" json:"dataset_size"`
	Tags        []string `mapstructure:"tags" yaml:"tags" json:"tags"`
}

// Runtime represents the runtime configuration for a provider.
type Runtime struct {
	K8s   *K8sRuntime   `mapstructure:"k8s" yaml:"k8s" json:"k8s,omitempty"`
	Local *LocalRuntime `mapstructure:"local" yaml:"local" json:"local,omitempty"`
}

// K8sRuntime represents Kubernetes-specific runtime configuration.
type K8sRuntime struct {
	Image         string   `mapstructure:"image" yaml:"image"`
	Entrypoint    []string `mapstructure:"entrypoint" yaml:"entrypoint"`
	CPURequest    string   `mapstructure:"cpu_request" yaml:"cpu_request"`
	MemoryRequest string   `mapstructure:"memory_request" yaml:"memory_request"`
	CPULimit      string   `mapstructure:"cpu_limit" yaml:"cpu_limit"`
	MemoryLimit   string   `mapstructure:"memory_limit" yaml:"memory_limit"`
	Env           []EnvVar `mapstructure:"env" yaml:"env"`
}

// LocalRuntime represents local runtime configuration.
type LocalRuntime struct{}

// EnvVar represents an environment variable key-value pair.
type EnvVar struct {
	Name  string `mapstructure:"name" yaml:"name" json:"name"`
	Value string `mapstructure:"value" yaml:"value" json:"value"`
}
