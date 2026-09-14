package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	metricCounters   = make(map[string]prometheus.Counter)
	metricCountersMu sync.Mutex
	counterVecs      = make(map[string]*prometheus.CounterVec)
)

// labelKey generates a unique key for a metric name and a set of labels.
func labelKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	// Sort keys for deterministic key
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("|%s=%s", k, labels[k]))
	}
	return b.String()
}

func GetOrCreateEvalCounter(labels map[string]string) prometheus.Counter {
	return getOrCreateCounter("eval", "Number of times this task has been scheduled", labels)
}

// GetOrCreateCounter returns a prometheus.Counter with the given name, help, and labels.
// If a counter with that name and labels already exists, it returns the existing one.
// Otherwise, it creates, registers, and returns a new counter.
func getOrCreateCounter(suffix string, help string, labels map[string]string) prometheus.Counter {
	metricCountersMu.Lock()
	defer metricCountersMu.Unlock()

	name := "trustyai_" + suffix
	key := labelKey(name, labels)
	if c, ok := metricCounters[key]; ok {
		return c
	}

	var labelNames []string
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	sort.Strings(labelNames)

	// Only create/register a new CounterVec if one for this name does not exist
	var counterVec *prometheus.CounterVec
	if existingVec, ok := counterVecs[name]; ok {
		counterVec = existingVec
	} else {
		counterVec = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: name,
				Help: help,
			},
			labelNames,
		)
		metrics.Registry.MustRegister(counterVec)
		counterVecs[name] = counterVec
	}

	// Get the counter with the label values in the correct order
	labelValues := make([]string, len(labelNames))
	for i, k := range labelNames {
		labelValues[i] = labels[k]
	}
	counter := counterVec.WithLabelValues(labelValues...)
	metricCounters[key] = counter
	return counter
}

func init() {
}
