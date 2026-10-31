package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

const maxEnvironmentFile = 1 << 20
const imageHint = `use "kupe app deploy" to deploy a tag, or "kupe app image resolve" to get its digest`

var digestImage = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?((\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:[0-9]{1,5})?|:[0-9]{1,5})/[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*(/[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*)*@sha256:[a-f0-9]{64}$`)

// readEnvironmentFile validates structure before any API call. Parsing errors
// never echo input, which may contain sensitive literal configuration.
func readEnvironmentFile(path string, in io.Reader, update bool) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- CLI user selects this local file; arbitrary paths are intentional.
		if err != nil {
			return nil, cli.Wrap(cli.ExitMisuse, "open environment file", err)
		}
		defer func() { _ = f.Close() }()
		in = f
	}
	data, err := io.ReadAll(io.LimitReader(in, maxEnvironmentFile+1))
	if err != nil {
		return nil, cli.Wrap(cli.ExitMisuse, "read environment file", err)
	}
	if len(data) > maxEnvironmentFile {
		return nil, cli.MisuseError("environment file exceeds 1 MiB")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		return nil, cli.MisuseError("invalid YAML or JSON environment file")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, cli.MisuseError("environment file must contain exactly one document")
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, cli.MisuseError("environment file must be an object")
	}
	if err := validateInputNode(node.Content[0], 0); err != nil {
		return nil, err
	}
	jsonData, err := sigyaml.YAMLToJSONStrict(data)
	if err != nil {
		return nil, cli.MisuseError("invalid YAML or JSON environment file")
	}
	var typed client.CreateAppEnvironmentRequest
	strict := json.NewDecoder(bytes.NewReader(jsonData))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&typed); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return nil, cli.MisuseError(err.Error())
		}
		return nil, cli.MisuseError("environment file has an invalid field type")
	}
	var fields map[string]any
	if err := json.Unmarshal(jsonData, &fields); err != nil {
		return nil, cli.MisuseError("invalid environment file")
	}
	if update {
		for _, key := range []string{"environment", "cluster"} {
			if _, ok := fields[key]; ok {
				return nil, cli.MisuseError(key + " cannot be changed on update")
			}
		}
	}
	return fields, nil
}
func validateInputNode(node *yaml.Node, depth int) error {
	if depth > 64 {
		return cli.MisuseError("environment file is nested too deeply")
	}
	if node.Kind == yaml.AliasNode {
		return cli.MisuseError("environment file cannot use YAML aliases")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return cli.MisuseError("environment file keys must be strings")
			}
			if seen[key.Value] {
				return cli.MisuseError("environment file contains a duplicate field")
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateInputNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func mergeEnvironmentMap(current, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(current, k)
			continue
		}
		if child, ok := v.(map[string]any); ok {
			prior, ok := current[k].(map[string]any)
			if !ok {
				prior = map[string]any{}
			}
			mergeEnvironmentMap(prior, child)
			current[k] = prior
		} else {
			current[k] = v
		}
	}
}
func environmentRequest(current *client.AppEnvironment) client.CreateAppEnvironmentRequest {
	if current != nil {
		p := current.DeletionProtection
		return client.CreateAppEnvironmentRequest{Environment: current.Environment, Cluster: current.Cluster, DeletionProtection: &p, Web: current.Web, Secrets: current.Secrets, Domains: current.Domains}
	}
	n := int32(1)
	return client.CreateAppEnvironmentRequest{Web: &client.AppWeb{Replicas: &n, Resources: &client.AppResources{Requests: client.AppQuantities{CPU: "250m", Memory: "256Mi"}, Limits: client.AppQuantities{CPU: "1", Memory: "512Mi"}}}}
}
func (o *envOptions) build(cmd *cobra.Command, current *client.AppEnvironment, file map[string]any, name string) (client.CreateAppEnvironmentRequest, error) {
	base, err := json.Marshal(environmentRequest(current))
	if err != nil {
		return client.CreateAppEnvironmentRequest{}, fmt.Errorf("encode current environment: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(base, &fields); err != nil {
		return client.CreateAppEnvironmentRequest{}, fmt.Errorf("decode current environment: %w", err)
	}
	mergeEnvironmentMap(fields, file)
	data, err := json.Marshal(fields)
	if err != nil {
		return client.CreateAppEnvironmentRequest{}, fmt.Errorf("encode environment input: %w", err)
	}
	var req client.CreateAppEnvironmentRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, cli.MisuseError("environment file has an invalid field type")
	}
	if current == nil && req.Environment != "" && req.Environment != name {
		return req, cli.MisuseError("environment in the file must match the command argument")
	}
	req.Environment = name
	if req.Web == nil {
		req.Web = &client.AppWeb{}
	}
	w := req.Web
	if w.Replicas == nil {
		n := int32(1)
		w.Replicas = &n
	}
	if w.Resources == nil {
		w.Resources = &client.AppResources{}
	}
	if current == nil {
		defaults := environmentRequest(nil).Web.Resources
		if w.Resources.Requests.CPU == "" {
			w.Resources.Requests.CPU = defaults.Requests.CPU
		}
		if w.Resources.Requests.Memory == "" {
			w.Resources.Requests.Memory = defaults.Requests.Memory
		}
		if w.Resources.Limits.CPU == "" {
			w.Resources.Limits.CPU = defaults.Limits.CPU
		}
		if w.Resources.Limits.Memory == "" {
			w.Resources.Limits.Memory = defaults.Limits.Memory
		}
	}
	if cmd.Flags().Changed("cluster") {
		req.Cluster = o.cluster
	}
	if cmd.Flags().Changed("image") {
		w.Image = o.image
	}
	if cmd.Flags().Changed("port") {
		w.Port = o.port
	}
	if cmd.Flags().Changed("replicas") {
		w.Replicas = &o.replicas
	}
	for _, entry := range []struct {
		name, value string
		dst         *string
	}{{"cpu", o.cpu, &w.Resources.Requests.CPU}, {"memory", o.memory, &w.Resources.Requests.Memory}, {"cpu-limit", o.cpuLimit, &w.Resources.Limits.CPU}, {"memory-limit", o.memoryLimit, &w.Resources.Limits.Memory}} {
		if cmd.Flags().Changed(entry.name) {
			*entry.dst = entry.value
		}
	}
	if cmd.Flags().Changed("no-deletion-protection") {
		p := !o.noProtection
		req.DeletionProtection = &p
	}
	if cmd.Flags().Changed("deletion-protection") {
		req.DeletionProtection = &o.protection
	}
	if err := applyEnvironmentLists(&req, o); err != nil {
		return req, err
	}
	if cmd.Flags().Changed("readiness") {
		if w.HealthChecks == nil {
			w.HealthChecks = &client.AppHealthChecks{}
		}
		switch {
		case o.readiness == "tcp":
			w.HealthChecks.Readiness = &client.AppProbe{TCP: &struct{}{}}
		case strings.HasPrefix(o.readiness, "http:/"):
			w.HealthChecks.Readiness = &client.AppProbe{HTTP: &client.AppHTTPProbe{Path: strings.TrimPrefix(o.readiness, "http:")}}
		default:
			return req, cli.MisuseError("--readiness must be tcp or http:/path")
		}
	}
	return req, validateEnvironment(req)
}
func splitAssignment(value, flag string) (string, string, error) {
	name, value, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return "", "", cli.MisuseError(flag + " must use NAME=VALUE")
	}
	return name, value, nil
}
func applyEnvironmentLists(req *client.CreateAppEnvironmentRequest, o *envOptions) error {
	for _, entry := range o.env {
		n, v, err := splitAssignment(entry, "--env")
		if err != nil {
			return err
		}
		replaceEnv(&req.Web.Env, client.AppEnvVar{Name: n, Value: &v})
	}
	for _, entry := range o.secretEnv {
		n, v, err := splitAssignment(entry, "--secret-env")
		if err != nil {
			return err
		}
		secret, key, ok := strings.Cut(v, ":")
		if !ok || secret == "" || key == "" {
			return cli.MisuseError("--secret-env must use VAR=SECRET:KEY")
		}
		replaceEnv(&req.Web.Env, client.AppEnvVar{Name: n, ValueFrom: &client.AppValueFrom{SecretKeyRef: client.AppSecretKeyRef{Secret: secret, Key: key}}})
	}
	for _, name := range o.unsetEnv {
		kept := make([]client.AppEnvVar, 0, len(req.Web.Env))
		for _, e := range req.Web.Env {
			if e.Name != name {
				kept = append(kept, e)
			}
		}
		req.Web.Env = kept
	}
	for _, entry := range o.secrets {
		n, v, err := splitAssignment(entry, "--secret")
		if err != nil {
			return err
		}
		found := false
		for i := range req.Secrets {
			if req.Secrets[i].Name == n {
				req.Secrets[i].Path = v
				found = true
				break
			}
		}
		if !found {
			req.Secrets = append(req.Secrets, client.AppSecret{Name: n, Path: v})
		}
	}
	for _, name := range o.removeSecrets {
		kept := make([]client.AppSecret, 0, len(req.Secrets))
		for _, s := range req.Secrets {
			if s.Name != name {
				kept = append(kept, s)
			}
		}
		req.Secrets = kept
	}
	for _, host := range o.domains {
		found := false
		for _, h := range req.Domains {
			if h == host {
				found = true
			}
		}
		if !found {
			req.Domains = append(req.Domains, host)
		}
	}
	for _, host := range o.removeDomains {
		kept := make([]string, 0, len(req.Domains))
		for _, h := range req.Domains {
			if h != host {
				kept = append(kept, h)
			}
		}
		req.Domains = kept
	}
	return nil
}
func replaceEnv(entries *[]client.AppEnvVar, next client.AppEnvVar) {
	for i := range *entries {
		if (*entries)[i].Name == next.Name {
			(*entries)[i] = next
			return
		}
	}
	*entries = append(*entries, next)
}
func validateEnvironment(req client.CreateAppEnvironmentRequest) error {
	if len(validation.IsDNS1123Label(req.Cluster)) > 0 {
		return cli.MisuseError("cluster must be an RFC 1123 label")
	}
	w := req.Web
	if len(w.Image) > 512 || !digestImage.MatchString(w.Image) {
		return cli.MisuseError("image must be a digest reference").WithHint(imageHint)
	}
	if w.Port < 1 || w.Port > 65535 {
		return cli.MisuseError("port must be between 1 and 65535")
	}
	if w.Replicas == nil || *w.Replicas < 1 || *w.Replicas > 10 {
		return cli.MisuseError("replicas must be between 1 and 10")
	}
	for _, entry := range []struct{ flag, label, request, limit string }{{"cpu", "CPU", w.Resources.Requests.CPU, w.Resources.Limits.CPU}, {"memory", "memory", w.Resources.Requests.Memory, w.Resources.Limits.Memory}} {
		request, err := resource.ParseQuantity(entry.request)
		if err != nil || len(entry.request) > 20 {
			return cli.MisuseError("invalid " + entry.flag + " request quantity")
		}
		limit, err := resource.ParseQuantity(entry.limit)
		if err != nil || len(entry.limit) > 20 {
			return cli.MisuseError("invalid " + entry.flag + " limit quantity")
		}
		if request.Cmp(limit) > 0 {
			return cli.MisuseError(fmt.Sprintf("--%s %s exceeds the %s limit %s; raise it with --%s-limit", entry.flag, entry.request, entry.label, entry.limit, entry.flag))
		}
	}
	for _, list := range [][]string{w.Command, w.Args} {
		if len(list) > 32 {
			return cli.MisuseError("command and args accept at most 32 entries")
		}
		for _, value := range list {
			if len(value) > 1024 {
				return cli.MisuseError("command and args entries must be at most 1024 bytes")
			}
		}
	}
	if len(w.Env) > 100 {
		return cli.MisuseError("web.env accepts at most 100 entries")
	}
	if len(req.Secrets) > 20 {
		return cli.MisuseError("secrets accepts at most 20 entries")
	}
	if len(req.Domains) > 5 {
		return cli.MisuseError("domains accepts at most five hostnames")
	}
	declared := map[string]bool{}
	for _, s := range req.Secrets {
		if len(validation.IsDNS1123Label(s.Name)) > 0 || declared[s.Name] {
			return cli.MisuseError("secret names must be unique RFC 1123 labels")
		}
		if s.Path == "" || len(s.Path) > 512 || strings.Contains(s.Path, "..") || !vaultReferencePath.MatchString(s.Path) {
			return cli.MisuseError("invalid Vault path in secrets")
		}
		declared[s.Name] = true
	}
	names := map[string]bool{}
	for _, e := range w.Env {
		if len(e.Name) > 253 || len(validation.IsEnvVarName(e.Name)) > 0 || names[e.Name] {
			return cli.MisuseError("environment variable names must be valid and unique")
		}
		names[e.Name] = true
		if (e.Value == nil) == (e.ValueFrom == nil) {
			return cli.MisuseError("each environment variable needs exactly one of value or valueFrom")
		}
		if e.Value != nil && utf8.RuneCountInString(*e.Value) > 4096 {
			return cli.MisuseError("environment variable values must be at most 4096 characters")
		}
		if e.ValueFrom != nil {
			ref := e.ValueFrom.SecretKeyRef
			if !declared[ref.Secret] {
				return cli.MisuseError("environment variable references an undeclared secret")
			}
			if len(validation.IsConfigMapKey(ref.Key)) > 0 {
				return cli.MisuseError("invalid secret key")
			}
		}
	}
	if w.HealthChecks != nil {
		for _, p := range []*client.AppProbe{w.HealthChecks.Readiness, w.HealthChecks.Liveness} {
			if p == nil {
				continue
			}
			if (p.TCP == nil) == (p.HTTP == nil) {
				return cli.MisuseError("a health check needs exactly one of tcp or http")
			}
			if p.HTTP != nil && (!strings.HasPrefix(p.HTTP.Path, "/") || utf8.RuneCountInString(p.HTTP.Path) > 2048) {
				return cli.MisuseError("HTTP health check paths must begin with / and be at most 2048 characters")
			}
		}
	}
	hosts := map[string]bool{}
	for _, h := range req.Domains {
		if !validDomain.MatchString(h) || len(h) > 253 || hosts[h] {
			return cli.MisuseError("domains must be unique exact lower-case hostnames")
		}
		hosts[h] = true
	}
	return nil
}

var vaultReferencePath = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
var validDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// changedTargets compares only user-edited fields, leaving unrelated concurrent
// edits available to the second RMW attempt. Lists edited by flags compare keys.
func changedTargets(before, after *client.AppEnvironment, o *envOptions, file map[string]any, cmd *cobra.Command) bool {
	if before.UID != after.UID {
		return true
	}
	b, _ := json.Marshal(environmentRequest(before))
	a, _ := json.Marshal(environmentRequest(after))
	var bm, am map[string]any
	_ = json.Unmarshal(b, &bm)
	_ = json.Unmarshal(a, &am)
	var paths [][]string
	var walk func(map[string]any, []string)
	walk = func(m map[string]any, prefix []string) {
		for k, v := range m {
			path := append(append([]string{}, prefix...), k)
			if child, ok := v.(map[string]any); ok {
				walk(child, path)
			} else {
				paths = append(paths, path)
			}
		}
	}
	walk(file, nil)
	for flag, path := range map[string][]string{"image": {"web", "image"}, "port": {"web", "port"}, "replicas": {"web", "replicas"}, "cpu": {"web", "resources", "requests", "cpu"}, "memory": {"web", "resources", "requests", "memory"}, "cpu-limit": {"web", "resources", "limits", "cpu"}, "memory-limit": {"web", "resources", "limits", "memory"}, "readiness": {"web", "healthChecks", "readiness"}, "no-deletion-protection": {"deletionProtection"}, "deletion-protection": {"deletionProtection"}} {
		if cmd.Flags().Changed(flag) {
			paths = append(paths, path)
		}
	}
	for _, path := range paths {
		if !reflect.DeepEqual(valueAt(bm, path), valueAt(am, path)) {
			return true
		}
	}
	for _, values := range [][]string{o.env, o.secretEnv, o.unsetEnv} {
		for _, v := range values {
			name, _, _ := strings.Cut(v, "=")
			if !reflect.DeepEqual(envNamed(before.Web.Env, name), envNamed(after.Web.Env, name)) {
				return true
			}
		}
	}
	for _, values := range [][]string{o.secrets, o.removeSecrets} {
		for _, v := range values {
			name, _, _ := strings.Cut(v, "=")
			if !reflect.DeepEqual(secretNamed(before.Secrets, name), secretNamed(after.Secrets, name)) {
				return true
			}
		}
	}
	for _, values := range [][]string{o.domains, o.removeDomains} {
		for _, host := range values {
			if hasDomain(before.Domains, host) != hasDomain(after.Domains, host) {
				return true
			}
		}
	}
	return false
}
func valueAt(m map[string]any, path []string) any {
	var v any = m
	for _, key := range path {
		object, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = object[key]
	}
	return v
}
func envNamed(env []client.AppEnvVar, name string) *client.AppEnvVar {
	for _, e := range env {
		if e.Name == name {
			return &e
		}
	}
	return nil
}
func secretNamed(secrets []client.AppSecret, name string) *client.AppSecret {
	for _, s := range secrets {
		if s.Name == name {
			return &s
		}
	}
	return nil
}
func hasDomain(domains []string, host string) bool {
	for _, h := range domains {
		if h == host {
			return true
		}
	}
	return false
}
