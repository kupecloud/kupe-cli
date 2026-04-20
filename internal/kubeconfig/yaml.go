package kubeconfig

import "sigs.k8s.io/yaml"

// yamlMarshal wraps sigs.k8s.io/yaml. Separated into a named helper so
// tests or future output customisation (e.g., comment headers) have a
// single knob to turn.
func yamlMarshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}
