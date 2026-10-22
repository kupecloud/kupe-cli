package printer

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestHumanBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KiB", 1288490188: "1.2 GiB", 5 << 30: "5.0 GiB", 3 << 40: "3.0 TiB",
	} {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestShortDigest(t *testing.T) {
	full := "sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a"
	if got := ShortDigest(full); got != "sha256:4f8c0d0e4b1f" {
		t.Fatalf("ShortDigest = %q", got)
	}
	if got := ShortDigest("v1"); got != "v1" {
		t.Fatalf("ShortDigest(v1) = %q", got)
	}
}

func TestRegistryStorage(t *testing.T) {
	limit, unlimited := int64(5<<30), int64(-1)
	tests := []struct {
		status *client.RegistryStatus
		want   string
	}{
		{&client.RegistryStatus{StorageUsedBytes: 1288490188, StorageLimitBytes: &limit}, "1.2 GiB of 5.0 GiB"},
		{&client.RegistryStatus{StorageUsedBytes: 0, StorageLimitBytes: &unlimited}, "0 B of unlimited"},
		{&client.RegistryStatus{StorageUsedBytes: 2048}, "2.0 KiB"},
		{nil, "0 B"},
	}
	for _, tt := range tests {
		if got := registryStorage(&client.Registry{Status: tt.status}); got != tt.want {
			t.Errorf("registryStorage = %q; want %q", got, tt.want)
		}
	}
}

func TestRegistryBinding_DetailListsPushNamespaces(t *testing.T) {
	io, out, _ := cli.Test()
	b := &client.RegistryBinding{
		Name: "prod", Cluster: "prod", PushNamespaces: []string{"ci", "build"}, SecretName: "kupe-registry-push",
		Status: &client.RegistryBindingStatus{Phase: "Pending", PullUsername: "kupe+acme+pull-prod",
			Namespaces: []client.RegistryBindingNamespaceStatus{{Name: "ci", Phase: "Ready"}, {Name: "old", Phase: "Ready"}},
			Rotation:   &client.RegistryBindingRotationStatus{Pending: true}},
	}
	if err := RegistryBinding.One(io, &Format{Kind: Table}, b); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Pull Username:", "kupe+acme+pull-prod", "ci, build", "pending", "PUSH-NAMESPACE", "build", "Pending", "old", "Removing"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestImageColumns(t *testing.T) {
	io, out, _ := cli.Test()
	imgs := []client.Image{{
		Repository: "team-a/api", Digest: "sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a", Size: 12345678,
		Tags:      []client.ImageTag{{Name: "v1.4.2"}, {Name: "latest"}},
		Platforms: []client.ImagePlatform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64", Variant: "v8"}},
	}, {Repository: "team-a/api", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if err := Image.List(io, &Format{Kind: Table}, imgs); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"sha256:4f8c0d0e4b1f ", "v1.4.2, latest", "11.8 MiB", "linux/amd64, linux/arm64/v8", "untagged"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "never") {
		t.Errorf("an image with no push time reads as never pushed:\n%s", got)
	}
	if strings.Contains(got, "4f8c0d0e4b1f2b6e") {
		t.Errorf("table shows the full digest:\n%s", got)
	}
}
