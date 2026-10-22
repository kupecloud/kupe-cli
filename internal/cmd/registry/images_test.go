package registry

import (
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestImageRef_Parse(t *testing.T) {
	tests := []struct {
		in      string
		want    imageRef
		wantErr string // substring; "" = parses
	}{
		{"team-a/api@" + digest, imageRef{repository: "team-a/api", digest: digest}, ""},
		{"team-a/api:v1.4.2", imageRef{repository: "team-a/api", tag: "v1.4.2"}, ""},
		{"api:Latest_1", imageRef{repository: "api", tag: "Latest_1"}, ""},
		{"registry.kupe.cloud/acme/team-a/api:v1", imageRef{repository: "team-a/api", tag: "v1"}, ""},
		{"localhost:5000/acme/api@" + digest, imageRef{repository: "api", digest: digest}, ""},
		{"team-a/api:v1@" + digest, imageRef{repository: "team-a/api", tag: "v1", digest: digest}, ""},
		{"registry.kupe.cloud/other/api:v1", imageRef{}, `belongs to tenant "other"`},
		{"team-a/api", imageRef{}, "names no image"},
		{"Team-A/api:v1", imageRef{}, "invalid repository"},
		{"api@sha256:abc", imageRef{}, "invalid digest"},
		{"api:-bad", imageRef{}, "invalid tag"},
		{"a//b:v1", imageRef{}, "invalid repository"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseImageRef("acme", tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || cli.ExitCode(err) != cli.ExitMisuse {
					t.Fatalf("err = %v; want exit 2 with %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestImageGet_SendsTheReference(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	if _, _, err := run(f, "image", "get", "registry.kupe.cloud/acme/team-a/api:v1"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(fake, "GetImage:registry:team-a/api:v1") {
		t.Fatalf("calls = %v", fake.Calls)
	}
	_, _, err := run(factoryWith(t, seeded()), "image", "get", "registry.kupe.cloud/beta/team-a/api:v1")
	assertExit(t, err, cli.ExitMisuse)
}

func TestImageDelete_RequiresDigest(t *testing.T) {
	fake := seeded()
	_, _, err := run(factoryWith(t, fake), "image", "delete", "team-a/api:v1", "--yes")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(err.Error(), `delete an image by digest; to remove a tag use "kupe registry tag delete team-a/api:v1"`) {
		t.Fatalf("err = %v", err)
	}
	assertNoCall(t, fake, "DeleteImage")

	out, _, err := run(factoryWith(t, fake), "image", "delete", "team-a/api@"+digest, "--yes")
	if err != nil || out != "image/team-a/api@"+digest+" deleted\n" {
		t.Fatalf("out %q, err %v", out, err)
	}
	if !hasCall(fake, "DeleteImage:registry:team-a/api:"+digest) {
		t.Fatalf("calls = %v", fake.Calls)
	}
}

func TestImageDelete_InUse(t *testing.T) {
	fake := seeded()
	fake.DeleteImageErr = &client.APIError{StatusCode: 409, Code: client.RegistryCodeImageInUse,
		Message: "this image is part of a multi-platform image; delete the multi-platform image instead"}
	_, _, err := run(factoryWith(t, fake), "image", "delete", "team-a/api@"+digest, "--yes")
	assertExit(t, err, cli.ExitConflict)
	if !strings.Contains(hint(err), "multi-platform image") {
		t.Fatalf("hint = %q", hint(err))
	}
}

func TestTagDelete(t *testing.T) {
	fake := seeded()
	out, _, err := run(factoryWith(t, fake), "tag", "delete", "team-a/api:v1", "--yes")
	if err != nil || out != "tag/team-a/api:v1 deleted\n" || !hasCall(fake, "DeleteTag:registry:team-a/api:v1") {
		t.Fatalf("out %q, err %v, calls %v", out, err, fake.Calls)
	}
	_, _, err = run(factoryWith(t, seeded()), "tag", "delete", "team-a/api@"+digest, "--yes")
	assertExit(t, err, cli.ExitMisuse)
	_, _, err = run(factoryWith(t, seeded()), "tag", "delete", "team-a/api:nothere", "--yes")
	assertExit(t, err, cli.ExitNotFound)
	if !strings.Contains(err.Error(), `tag "nothere" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRepoDelete_ConfirmByName(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	interactive(f, "yes\n")
	_, errOut, err := run(f, "repo", "delete", "web")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(errOut, "Deleting a repository deletes every image and tag in it.") || !strings.Contains(errOut, `delete repository "web". Type the name to confirm`) {
		t.Fatalf("stderr = %q", errOut)
	}
	assertNoCall(t, fake, "DeleteRepository")
	interactive(f, "web\n")
	out, _, err := run(f, "repo", "delete", "web")
	if err != nil || out != "repository/web deleted\n" {
		t.Fatalf("out %q, err %v", out, err)
	}
}

func TestRepoList_PagesAndAll(t *testing.T) {
	fake := seeded()
	for i := range 150 {
		fake.Repositories = append(fake.Repositories, client.Repository{Name: "r" + strings.Repeat("x", i%3) + string(rune('a'+i%26))})
	}
	f := factoryWith(t, fake)
	_, errOut, err := run(f, "repo", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "Showing repositories 1-25 of 152; pass --page 2 for more, or --all.") {
		t.Fatalf("stderr = %q", errOut)
	}
	fake.ListRepositoriesCalls = nil
	out, _, err := run(factoryWith(t, fake), "repo", "list", "--all", "-o", "name")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "\n"); n != 152 {
		t.Fatalf("--all listed %d; want 152", n)
	}
	if len(fake.ListRepositoriesCalls) != 2 || fake.ListRepositoriesCalls[0].PageSize != client.MaxRegistryPageSize {
		t.Fatalf("calls = %+v", fake.ListRepositoriesCalls)
	}
	for _, args := range [][]string{{"--all", "--page", "2"}, {"--page", "0"}, {"--page-size", "101"}} {
		_, _, err := run(factoryWith(t, seeded()), append([]string{"repo", "list"}, args...)...)
		assertExit(t, err, cli.ExitMisuse)
	}
}

func TestImageList_TaggedFilter(t *testing.T) {
	for _, tt := range []struct {
		flag string
		want *bool
	}{{"", nil}, {"--tagged", ptr(true)}, {"--untagged", ptr(false)}} {
		fake := seeded()
		args := []string{"image", "list", "team-a/api"}
		if tt.flag != "" {
			args = append(args, tt.flag)
		}
		if _, _, err := run(factoryWith(t, fake), args...); err != nil {
			t.Fatal(err)
		}
		got := fake.ListImagesCalls[0].Tagged
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%q: tagged = %v; want %v", tt.flag, got, tt.want)
		}
	}
	_, _, err := run(factoryWith(t, seeded()), "image", "list", "team-a/api", "--tagged", "--untagged")
	assertExit(t, err, cli.ExitMisuse)
}

func ptr[T any](v T) *T { return &v }
