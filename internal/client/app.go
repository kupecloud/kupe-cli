package client

import (
	"context"
	"net/http"
)

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
	Web                *AppWeb               `json:"web" yaml:"web"`
	Secrets            []AppSecret           `json:"secrets" yaml:"secrets"`
	Domains            []string              `json:"domains" yaml:"domains"`
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
type AppWeb struct {
	Image        string           `json:"image" yaml:"image"`
	Port         int32            `json:"port" yaml:"port"`
	Replicas     *int32           `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Resources    *AppResources    `json:"resources" yaml:"resources"`
	Command      []string         `json:"command" yaml:"command"`
	Args         []string         `json:"args" yaml:"args"`
	Env          []AppEnvVar      `json:"env" yaml:"env"`
	HealthChecks *AppHealthChecks `json:"healthChecks" yaml:"healthChecks"`
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
type AppSecret struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}
type CreateAppEnvironmentRequest struct {
	Environment        string      `json:"environment" yaml:"environment"`
	Cluster            string      `json:"cluster" yaml:"cluster"`
	DeletionProtection *bool       `json:"deletionProtection,omitempty" yaml:"deletionProtection,omitempty"`
	Web                *AppWeb     `json:"web" yaml:"web"`
	Secrets            []AppSecret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Domains            []string    `json:"domains,omitempty" yaml:"domains,omitempty"`
}
type PatchAppEnvironmentRequest struct {
	Web                *AppWeb      `json:"web,omitempty"`
	Secrets            *[]AppSecret `json:"secrets,omitempty"`
	Domains            *[]string    `json:"domains,omitempty"`
	DeletionProtection *bool        `json:"deletionProtection,omitempty"`
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
