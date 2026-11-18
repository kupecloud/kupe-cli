package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// AppRetry is the accepted retry, before the operator creates its release.
type AppRetry struct {
	App                string `json:"app" yaml:"app"`
	Environment        string `json:"environment" yaml:"environment"`
	DeploymentRevision int64  `json:"deploymentRevision" yaml:"deploymentRevision"`
	Generation         int64  `json:"generation" yaml:"generation"`
}
type AppRelease struct {
	Name               string            `json:"name" yaml:"name"`
	Number             int64             `json:"number" yaml:"number"`
	DeploymentRevision int64             `json:"deploymentRevision" yaml:"deploymentRevision"`
	Trigger            string            `json:"trigger" yaml:"trigger"`
	Runtime            AppReleaseRuntime `json:"runtime" yaml:"runtime"`
	Chart              AppReleaseChart   `json:"chart" yaml:"chart"`
	Status             AppReleaseStatus  `json:"status" yaml:"status"`
}

// Runtime is flat on the wire: the environment runtime fields are inlined, so
// image is runtime.image with no nested runtime object.
type AppReleaseRuntime struct {
	AppRuntime `yaml:",inline"`
	Secrets    []AppReleaseSecret `json:"secrets" yaml:"secrets"`
	Databases  []AppDatabase      `json:"databases" yaml:"databases"`
}
type AppReleaseSecret struct {
	Name string   `json:"name" yaml:"name"`
	Path string   `json:"path" yaml:"path"`
	Keys []string `json:"keys" yaml:"keys"`
}
type AppReleaseChart struct {
	Revision string `json:"revision" yaml:"revision"`
}
type AppReleaseStatus struct {
	Phase       string              `json:"phase" yaml:"phase"`
	Reason      string              `json:"reason" yaml:"reason"`
	Message     string              `json:"message" yaml:"message"`
	StartedAt   string              `json:"startedAt" yaml:"startedAt"`
	DeadlineAt  string              `json:"deadlineAt" yaml:"deadlineAt"`
	CompletedAt string              `json:"completedAt" yaml:"completedAt"`
	ImageDigest string              `json:"imageDigest" yaml:"imageDigest"`
	Recovery    *AppReleaseRecovery `json:"recovery,omitempty" yaml:"recovery,omitempty"`
}
type AppReleaseRecovery struct {
	Phase       string               `json:"phase" yaml:"phase"`
	Release     *AppReleaseReference `json:"release" yaml:"release"`
	Reason      string               `json:"reason" yaml:"reason"`
	StartedAt   string               `json:"startedAt" yaml:"startedAt"`
	DeadlineAt  string               `json:"deadlineAt" yaml:"deadlineAt"`
	CompletedAt string               `json:"completedAt" yaml:"completedAt"`
}
type ImageResolution struct {
	Reference    string          `json:"reference" yaml:"reference"`
	Platforms    []ImagePlatform `json:"platforms" yaml:"platforms"`
	User         string          `json:"user" yaml:"user"`
	ExposedPorts []int32         `json:"exposedPorts" yaml:"exposedPorts"`
}

func (c *Client) RetryAppEnvironment(ctx context.Context, app, env, etag string) (*AppRetry, string, error) {
	var result AppRetry
	next, err := c.requestWithOptions(ctx, http.MethodPost, c.tenantPath("apps", app, "environments", env, "retry"), etag, nil, &result, requestOptions{noReplay: true, noRedirect: true})
	if err != nil {
		return nil, "", err
	}
	return &result, next, nil
}
func (c *Client) ListAppReleases(ctx context.Context, app, env string) ([]AppRelease, error) {
	var result struct {
		Items []AppRelease `json:"items"`
	}
	_, err := c.request(ctx, http.MethodGet, c.tenantPath("apps", app, "environments", env, "releases"), nil, &result)
	return result.Items, err
}
func (c *Client) ResolveAppImage(ctx context.Context, image string) (*ImageResolution, error) {
	var result ImageResolution
	_, err := c.request(ctx, http.MethodPost, c.tenantPath("app-images", "resolve"), struct {
		Image string `json:"image"`
	}{image}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ReportDeployWithoutImage tells the API a deploy was tried without an image.
// detected is "dockerfile" or "none". The API answers 204 with no body.
// A POST is never retried, apart from the one 429 retry every request gets.
func (c *Client) ReportDeployWithoutImage(ctx context.Context, detected string) error {
	_, err := c.request(ctx, http.MethodPost, c.tenantPath("app-deploys", "without-image"), struct {
		Detected string `json:"detected"`
	}{detected}, nil)
	return err
}

// App is one stateless service and the summary of its environments.
type App struct {
	Name               string     `json:"name" yaml:"name"`
	UID                string     `json:"uid" yaml:"uid"`
	Generation         int64      `json:"generation" yaml:"generation"`
	ResourceVersion    string     `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt          string     `json:"createdAt" yaml:"createdAt"`
	DeletionProtection bool       `json:"deletionProtection" yaml:"deletionProtection"`
	Status             *AppStatus `json:"status,omitempty" yaml:"status,omitempty"`
}
type AppStatus struct {
	Phase              string                  `json:"phase" yaml:"phase"`
	Environments       []AppEnvironmentSummary `json:"environments" yaml:"environments"`
	Conditions         []Condition             `json:"conditions" yaml:"conditions"`
	ObservedGeneration int64                   `json:"observedGeneration" yaml:"observedGeneration"`
}
type AppEnvironmentSummary struct {
	Name    string `json:"name" yaml:"name"`
	Phase   string `json:"phase" yaml:"phase"`
	Address string `json:"address" yaml:"address"`
}
type CreateAppRequest struct {
	Name               string `json:"name"`
	DeletionProtection *bool  `json:"deletionProtection,omitempty"`
}
type PatchAppRequest struct {
	DeletionProtection *bool `json:"deletionProtection,omitempty"`
}

// AppEnvironment contains desired configuration and independent deployment,
// active-release and recovery status. It never contains fetched Vault values.
type AppEnvironment struct {
	Name               string                `json:"name" yaml:"name"`
	UID                string                `json:"uid" yaml:"uid"`
	Generation         int64                 `json:"generation" yaml:"generation"`
	ResourceVersion    string                `json:"resourceVersion" yaml:"resourceVersion"`
	CreatedAt          string                `json:"createdAt" yaml:"createdAt"`
	App                string                `json:"app" yaml:"app"`
	Environment        string                `json:"environment" yaml:"environment"`
	Cluster            string                `json:"cluster" yaml:"cluster"`
	DeletionProtection bool                  `json:"deletionProtection" yaml:"deletionProtection"`
	DeploymentRevision int64                 `json:"deploymentRevision" yaml:"deploymentRevision"`
	Runtime            *AppRuntime           `json:"runtime" yaml:"runtime"`
	Secrets            []AppSecret           `json:"secrets" yaml:"secrets"`
	Domains            []string              `json:"domains" yaml:"domains"`
	Databases          []AppDatabase         `json:"databases" yaml:"databases"`
	Status             *AppEnvironmentStatus `json:"status,omitempty" yaml:"status,omitempty"`
}
type AppQuantities struct {
	CPU    string `json:"cpu" yaml:"cpu"`
	Memory string `json:"memory" yaml:"memory"`
}
type AppResources struct {
	Requests AppQuantities `json:"requests" yaml:"requests"`
	Limits   AppQuantities `json:"limits" yaml:"limits"`
}
type AppRuntime struct {
	Image        string           `json:"image" yaml:"image"`
	Port         int32            `json:"port" yaml:"port"`
	Replicas     *int32           `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Resources    *AppResources    `json:"resources" yaml:"resources"`
	Command      []string         `json:"command" yaml:"command"`
	Args         []string         `json:"args" yaml:"args"`
	Env          []AppEnvVar      `json:"env" yaml:"env"`
	HealthChecks *AppHealthChecks `json:"healthChecks" yaml:"healthChecks"`
	Autoscaling  *AppAutoscaling  `json:"autoscaling,omitempty" yaml:"autoscaling,omitempty"`
}

// AppAutoscaling keeps the pod count between MinPods and MaxPods, aiming for
// CPUPercent of the CPU request. A zero CPUPercent is left out so the platform
// default applies.
type AppAutoscaling struct {
	MinPods    int32 `json:"minPods" yaml:"minPods"`
	MaxPods    int32 `json:"maxPods" yaml:"maxPods"`
	CPUPercent int32 `json:"cpuPercent,omitempty" yaml:"cpuPercent,omitempty"`
}
type AppEnvVar struct {
	Name      string        `json:"name" yaml:"name"`
	Value     *string       `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *AppValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}
type AppValueFrom struct {
	SecretKeyRef AppSecretKeyRef `json:"secretKeyRef" yaml:"secretKeyRef"`
}
type AppSecretKeyRef struct {
	Secret string `json:"secret" yaml:"secret"`
	Key    string `json:"key" yaml:"key"`
}
type AppProbe struct {
	TCP  *struct{}     `json:"tcp,omitempty" yaml:"tcp,omitempty"`
	HTTP *AppHTTPProbe `json:"http,omitempty" yaml:"http,omitempty"`
}
type AppHTTPProbe struct {
	Path string `json:"path" yaml:"path"`
}
type AppHealthChecks struct {
	Readiness *AppProbe `json:"readiness,omitempty" yaml:"readiness,omitempty"`
	Liveness  *AppProbe `json:"liveness,omitempty" yaml:"liveness,omitempty"`
}

// AppDatabase connects a database in the tenant to an environment. Env names
// the variable that carries the connection URL; the API defaults it to
// DATABASE_URL.
type AppDatabase struct {
	Name            string `json:"name" yaml:"name"`
	Database        string `json:"database" yaml:"database"`
	LogicalDatabase string `json:"logicalDatabase,omitempty" yaml:"logicalDatabase,omitempty"`
	Env             string `json:"env" yaml:"env"`
}

// AppDatabaseStatus reports one connection. The Secret fields name the
// delivered Secrets, never their values.
type AppDatabaseStatus struct {
	Name            string `json:"name" yaml:"name"`
	Env             string `json:"env" yaml:"env"`
	Phase           string `json:"phase" yaml:"phase"`
	Message         string `json:"message" yaml:"message"`
	RuntimeSecret   string `json:"runtimeSecret" yaml:"runtimeSecret"`
	MigrationSecret string `json:"migrationSecret" yaml:"migrationSecret"`
}
type AppSecret struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}
type CreateAppEnvironmentRequest struct {
	Environment        string        `json:"environment" yaml:"environment"`
	Cluster            string        `json:"cluster" yaml:"cluster"`
	DeletionProtection *bool         `json:"deletionProtection,omitempty" yaml:"deletionProtection,omitempty"`
	Runtime            *AppRuntime   `json:"runtime" yaml:"runtime"`
	Secrets            []AppSecret   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Domains            []string      `json:"domains,omitempty" yaml:"domains,omitempty"`
	Databases          []AppDatabase `json:"databases,omitempty" yaml:"databases,omitempty"`
}
type PatchAppEnvironmentRequest struct {
	// Runtime is the complete effective configuration. Its absent checks and probe
	// alternatives must be explicit nulls in the recursive merge patch.
	Runtime            *AppRuntime    `json:"runtime,omitempty"`
	Secrets            *[]AppSecret   `json:"secrets,omitempty"`
	Domains            *[]string      `json:"domains,omitempty"`
	Databases          *[]AppDatabase `json:"databases,omitempty"`
	DeletionProtection *bool          `json:"deletionProtection,omitempty"`
}

func (p PatchAppEnvironmentRequest) MarshalJSON() ([]byte, error) {
	type probePatch struct {
		TCP  *struct{}     `json:"tcp"`
		HTTP *AppHTTPProbe `json:"http"`
	}
	type checksPatch struct {
		Readiness *probePatch `json:"readiness"`
		Liveness  *probePatch `json:"liveness"`
	}
	type runtimePatch struct {
		*AppRuntime
		HealthChecks *checksPatch    `json:"healthChecks"`
		Autoscaling  *AppAutoscaling `json:"autoscaling"` // explicit null removes autoscaling
	}
	probe := func(p *AppProbe) *probePatch {
		if p == nil {
			return nil
		}
		return &probePatch{TCP: p.TCP, HTTP: p.HTTP}
	}
	var runtime *runtimePatch
	if p.Runtime != nil {
		runtime = &runtimePatch{AppRuntime: p.Runtime, Autoscaling: p.Runtime.Autoscaling}
		if checks := p.Runtime.HealthChecks; checks != nil {
			runtime.HealthChecks = &checksPatch{Readiness: probe(checks.Readiness), Liveness: probe(checks.Liveness)}
		}
	}
	// The alias retains the other patch fields without recursively calling us.
	type patch PatchAppEnvironmentRequest
	return json.Marshal(struct {
		patch
		Runtime *runtimePatch `json:"runtime,omitempty"`
	}{patch: patch(p), Runtime: runtime})
}

type AppEnvironmentStatus struct {
	Phase                 string               `json:"phase" yaml:"phase"`
	Deployment            *AppDeployment       `json:"deployment" yaml:"deployment"`
	Namespace             string               `json:"namespace" yaml:"namespace"`
	Address               string               `json:"address" yaml:"address"`
	RequestedRelease      *AppReleaseReference `json:"requestedRelease" yaml:"requestedRelease"`
	ActiveRelease         *AppReleaseReference `json:"activeRelease" yaml:"activeRelease"`
	LastSuccessfulRelease *AppReleaseReference `json:"lastSuccessfulRelease" yaml:"lastSuccessfulRelease"`
	Recovery              *AppRecovery         `json:"recovery" yaml:"recovery"`
	Domains               []AppDomainStatus    `json:"domains" yaml:"domains"`
	Secrets               []AppSecretStatus    `json:"secrets" yaml:"secrets"`
	Databases             []AppDatabaseStatus  `json:"databases" yaml:"databases"`
	Conditions            []Condition          `json:"conditions" yaml:"conditions"`
	ObservedGeneration    int64                `json:"observedGeneration" yaml:"observedGeneration"`
}
type AppReleaseReference struct {
	Name string `json:"name" yaml:"name"`
}
type AppDeployment struct {
	Release *AppReleaseReference `json:"release" yaml:"release"`
	Phase   string               `json:"phase" yaml:"phase"`
	Reason  string               `json:"reason" yaml:"reason"`
}
type AppRecovery struct {
	Phase   string               `json:"phase" yaml:"phase"`
	Release *AppReleaseReference `json:"release" yaml:"release"`
}
type AppDomainStatus struct {
	Host   string `json:"host" yaml:"host"`
	Phase  string `json:"phase" yaml:"phase"`
	Reason string `json:"reason" yaml:"reason"`
	Record string `json:"record" yaml:"record"`
}
type AppSecretStatus struct {
	Name        string   `json:"name" yaml:"name"`
	Phase       string   `json:"phase" yaml:"phase"`
	MissingKeys []string `json:"missingKeys" yaml:"missingKeys"`
}

// ConfirmedResourceAPI adds the App-specific delete body without changing
// ResourceAPI.Delete for other managed services.
type ConfirmedResourceAPI[T, C, P any] interface {
	ResourceAPI[T, C, P]
	DeleteConfirmed(ctx context.Context, name, confirm string) error
}
type AppAPI = ConfirmedResourceAPI[App, CreateAppRequest, PatchAppRequest]
type AppEnvironmentAPI = ConfirmedResourceAPI[AppEnvironment, CreateAppEnvironmentRequest, PatchAppEnvironmentRequest]
type appResource[T, C, P any] struct{ resource[T, C, P] }

func (c *Client) Apps() AppAPI {
	return appResource[App, CreateAppRequest, PatchAppRequest]{newResource[App, CreateAppRequest, PatchAppRequest](c, "apps")}
}
func (c *Client) AppEnvironments(app string) AppEnvironmentAPI {
	return appResource[AppEnvironment, CreateAppEnvironmentRequest, PatchAppEnvironmentRequest]{newResource[AppEnvironment, CreateAppEnvironmentRequest, PatchAppEnvironmentRequest](c, "apps", app, "environments")}
}
func (r appResource[T, C, P]) Delete(ctx context.Context, name string) error {
	return r.DeleteConfirmed(ctx, name, name)
}
func (r appResource[T, C, P]) DeleteConfirmed(ctx context.Context, name, confirm string) error {
	_, etag, err := r.Get(ctx, name)
	if err != nil {
		return err
	}
	_, err = r.c.requestWithETag(ctx, http.MethodDelete, r.path(name), etag, struct {
		Confirm string `json:"confirm"`
	}{confirm}, nil)
	return err
}
