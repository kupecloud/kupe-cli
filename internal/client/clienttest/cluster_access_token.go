package clienttest

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// FakeClusterAccessTokenSecret is the token a collect of id returns.
func FakeClusterAccessTokenSecret(id string) string {
	return "fake-cluster-token-" + strings.TrimPrefix(id, "cat-")
}

// Fake cluster connection details a collect returns.
const (
	FakeClusterEndpoint = "https://acme-dev.k8s.example.test"
	FakeClusterCA       = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCmZha2UKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
)

func clusterAccessTokenMissing(id string) error {
	return &client.APIError{StatusCode: 404, Message: fmt.Sprintf("access token %q not found", id)}
}

// codedErr is an API refusal with a code, as kupe-api sends it.
func codedErr(status int, code, msg string) error {
	return &client.APIError{StatusCode: status, Code: code, Message: msg}
}

// ListClusterAccessTokens implements client.Interface. Records
// "ListClusterAccessTokens:<cluster>".
func (f *Fake) ListClusterAccessTokens(_ context.Context, cluster string) ([]client.ClusterAccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListClusterAccessTokens:" + cluster)
	if f.ListClusterAccessTokensErr != nil {
		return nil, f.ListClusterAccessTokensErr
	}
	out := make([]client.ClusterAccessToken, 0, len(f.ClusterAccessTokens))
	for _, t := range f.ClusterAccessTokens {
		if t.Cluster == cluster {
			out = append(out, *t)
		}
	}
	slices.SortFunc(out, func(a, b client.ClusterAccessToken) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// GetClusterAccessToken implements client.Interface. Honours
// GetClusterAccessTokenSeq (a nil entry is a 404), answers 404 for a token
// of another cluster and, as a real request does, fails with the
// context's error once ctx is done. Records
// "GetClusterAccessToken:<cluster>:<id>".
func (f *Fake) GetClusterAccessToken(ctx context.Context, cluster, id string) (*client.ClusterAccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetClusterAccessToken:" + cluster + ":" + id)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.GetClusterAccessTokenErr != nil {
		return nil, f.GetClusterAccessTokenErr
	}
	t, ok := popSeq(f.GetClusterAccessTokenSeq, id)
	if !ok {
		t = f.ClusterAccessTokens[id]
	}
	if t == nil || t.Cluster != cluster {
		return nil, clusterAccessTokenMissing(id)
	}
	clone := *t
	return &clone, nil
}

// CreateClusterAccessToken implements client.Interface. Records
// "CreateClusterAccessToken:<cluster>:<displayName>" and the body.
func (f *Fake) CreateClusterAccessToken(_ context.Context, cluster string, req client.CreateClusterAccessTokenRequest) (*client.ClusterAccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateClusterAccessToken:" + cluster + ":" + req.DisplayName)
	f.ClusterAccessTokenCreates = append(f.ClusterAccessTokenCreates, req)
	if f.CreateClusterAccessTokenErr != nil {
		return nil, f.CreateClusterAccessTokenErr
	}
	id := f.NextClusterAccessTokenID
	if id == "" {
		id = "cat-0123456789abcdef"
	}
	phase := client.PhaseReady
	if f.CreatedClusterAccessTokenPhase != "" {
		phase = f.CreatedClusterAccessTokenPhase
	}
	stored := &client.ClusterAccessToken{
		ID: id, Cluster: cluster, DisplayName: req.DisplayName, Access: req.Access,
		CreatedBy: "tester@acme.com", CreatedAt: "2026-11-22T10:00:00Z", ExpiresAt: "2027-02-20T10:00:00Z",
		Status: &client.ClusterAccessTokenStatus{Phase: phase},
	}
	f.ClusterAccessTokens[id] = stored
	created := *stored
	created.Status = &client.ClusterAccessTokenStatus{Phase: client.PhasePending}
	return &created, nil
}

// DeleteClusterAccessToken implements client.Interface. As a real request
// does, it fails with the context's error once ctx is done. Records
// "DeleteClusterAccessToken:<cluster>:<id>".
func (f *Fake) DeleteClusterAccessToken(ctx context.Context, cluster, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteClusterAccessToken:" + cluster + ":" + id)
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.DeleteClusterAccessTokenErr != nil {
		return f.DeleteClusterAccessTokenErr
	}
	t, ok := f.ClusterAccessTokens[id]
	if !ok || t.Cluster != cluster {
		return clusterAccessTokenMissing(id)
	}
	delete(f.ClusterAccessTokens, id)
	return nil
}

// CollectClusterAccessToken implements client.Interface: the stored
// token's refusals as kupe-api answers them (404, 409 ACCESS_TOKEN_FAILED,
// 409 ACCESS_TOKEN_NOT_READY, 410), else the token, once. Records
// "CollectClusterAccessToken:<cluster>:<id>".
func (f *Fake) CollectClusterAccessToken(ctx context.Context, cluster, id string) (*client.CollectedClusterAccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CollectClusterAccessToken:" + cluster + ":" + id)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(f.CollectClusterAccessTokenErrs) > 0 {
		err := f.CollectClusterAccessTokenErrs[0]
		f.CollectClusterAccessTokenErrs = f.CollectClusterAccessTokenErrs[1:]
		return nil, err
	}
	t, ok := f.ClusterAccessTokens[id]
	if !ok || t.Cluster != cluster {
		return nil, clusterAccessTokenMissing(id)
	}
	switch {
	case t.Phase() == client.PhaseFailed:
		return nil, codedErr(409, "ACCESS_TOKEN_FAILED", "the access token could not be issued; revoke it and create another")
	case t.Phase() != client.PhaseReady:
		return nil, codedErr(409, "ACCESS_TOKEN_NOT_READY", "the access token is not ready yet; try again in a few seconds")
	case t.Collected:
		return nil, codedErr(410, "ACCESS_TOKEN_ALREADY_COLLECTED", "the access token was already collected; it is shown only once. If it was lost, revoke it and create another")
	}
	t.Collected, t.CollectedAt = true, "2026-11-22T10:00:05Z"
	token := FakeClusterAccessTokenSecret(id)
	name := "kupe-" + f.TenantName + "-" + cluster + "-" + id
	kubeconfig := fmt.Sprintf(`apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %[3]s
    server: %[2]s
  name: %[1]s
contexts:
- context:
    cluster: %[1]s
    user: %[1]s
  name: %[1]s
current-context: %[1]s
kind: Config
users:
- name: %[1]s
  user:
    token: %[4]s
`, name, FakeClusterEndpoint, FakeClusterCA, token)
	return &client.CollectedClusterAccessToken{
		ID: id, Cluster: cluster, Token: token, Kubeconfig: kubeconfig,
		Endpoint: FakeClusterEndpoint, CertificateAuthority: FakeClusterCA, ExpiresAt: t.ExpiresAt,
	}, nil
}
