package registry

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// The registry's name grammar, as kupe-api checks it: a repository is lower-case components separated by '/', a
// digest is sha256 and 64 hex digits, a tag is up to 128 word characters.
var (
	repositoryPattern = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)
	digestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	tagPattern        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// maxRepositoryLength caps a repository name with "<tenant>/" in front.
const maxRepositoryLength = 255

// maxPage is the last page kupe-api serves.
const maxPage = 10000

// imageRef is an image named on the command line: a repository and a tag,
// a digest, or both.
type imageRef struct {
	repository, tag, digest string
}

// String is REPOSITORY@DIGEST, or REPOSITORY:TAG when there is no digest.
func (r imageRef) String() string {
	if r.digest != "" {
		return r.repository + "@" + r.digest
	}
	return r.repository + ":" + r.tag
}

// reference is what the API takes for the image: the digest when given,
// else the tag.
func (r imageRef) reference() string {
	if r.digest != "" {
		return r.digest
	}
	return r.tag
}

// stripRegistry takes "<host>/<tenant>/" off s, as docker pull would take
// it: a first segment with a '.' or ':' (or localhost) is a host when at
// least a tenant and a repository follow. The tenant must be this
// context's.
func stripRegistry(tenant, s string) (string, error) {
	parts := strings.Split(s, "/")
	if len(parts) < 3 || (!strings.ContainsAny(parts[0], ".:") && parts[0] != "localhost") {
		return s, nil
	}
	if parts[1] != tenant {
		return "", cli.MisuseError(fmt.Sprintf("%s belongs to tenant %q, but this context is tenant %q", s, parts[1], tenant)).
			WithHint("switch context with:  kupe config use-context NAME")
	}
	return strings.Join(parts[2:], "/"), nil
}

// parseRepository reads a REPOSITORY argument, with or without the
// "<host>/<tenant>/" prefix.
func parseRepository(tenant, s string) (string, error) {
	repo, err := stripRegistry(tenant, s)
	if err != nil {
		return "", err
	}
	if !repositoryPattern.MatchString(repo) || len(tenant)+1+len(repo) > maxRepositoryLength {
		return "", cli.MisuseError(fmt.Sprintf("invalid repository %q", repo)).
			WithHint("repository names are lower-case letters and digits, separated by '.', '_', '__' or '-', with '/' between parts, e.g. team-a/api")
	}
	return repo, nil
}

// parseImageRef reads REPOSITORY:TAG, REPOSITORY@sha256:DIGEST or both, with
// or without the "<host>/<tenant>/" prefix.
func parseImageRef(tenant, s string) (imageRef, error) {
	rest, err := stripRegistry(tenant, s)
	if err != nil {
		return imageRef{}, err
	}
	var ref imageRef
	name, digest, hasDigest := strings.Cut(rest, "@")
	if hasDigest {
		if !digestPattern.MatchString(digest) {
			return imageRef{}, cli.MisuseError(fmt.Sprintf("invalid digest %q", digest)).
				WithHint("a digest is sha256: and 64 lower-case hex digits")
		}
		ref.digest = digest
	}
	if i := strings.LastIndex(name, ":"); i >= 0 {
		ref.tag, name = name[i+1:], name[:i]
		if !tagPattern.MatchString(ref.tag) {
			return imageRef{}, cli.MisuseError(fmt.Sprintf("invalid tag %q", ref.tag)).
				WithHint("tags are up to 128 letters, digits, '_', '.' and '-', not starting with '.' or '-'")
		}
	}
	if ref.tag == "" && ref.digest == "" {
		return imageRef{}, cli.MisuseError(fmt.Sprintf("%q names no image", s)).
			WithHint("name an image as REPOSITORY:TAG or REPOSITORY@sha256:DIGEST; list them with:  kupe registry image list REPOSITORY")
	}
	if ref.repository, err = parseRepository(tenant, name); err != nil {
		return imageRef{}, err
	}
	return ref, nil
}

// pageOpts are the paging flags of a list.
type pageOpts struct {
	page, pageSize int
	all            bool
}

func (p *pageOpts) bind(cmd *cobra.Command) {
	cmd.Flags().IntVar(&p.page, "page", 1, "Page to show")
	cmd.Flags().IntVar(&p.pageSize, "page-size", 25, "Items per page (at most 100)")
	cmd.Flags().BoolVar(&p.all, "all", false, "Show every page")
}

func (p *pageOpts) validate(cmd *cobra.Command) error {
	switch {
	case p.all && cmd.Flags().Changed("page"):
		return cli.MisuseError("pass --page or --all, not both")
	case p.page < 1 || p.page > maxPage:
		return cli.MisuseError(fmt.Sprintf("--page must be from 1 to %d", maxPage))
	case p.pageSize < 1 || p.pageSize > client.MaxRegistryPageSize:
		return cli.MisuseError(fmt.Sprintf("--page-size must be from 1 to %d", client.MaxRegistryPageSize))
	}
	return nil
}

// collect fetches the page asked for, or with --all every page (at the
// largest page size), and returns the items and the total.
func collect[T any](ctx context.Context, p pageOpts, fetch func(ctx context.Context, page, pageSize int) (*client.Page[T], error)) ([]T, int, error) {
	if !p.all {
		pg, err := fetch(ctx, p.page, p.pageSize)
		if err != nil {
			return nil, 0, err
		}
		return pg.Items, pg.Total, nil
	}
	var items []T
	total := 0
	for page := 1; page <= maxPage; page++ {
		pg, err := fetch(ctx, page, client.MaxRegistryPageSize)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, pg.Items...)
		total = pg.Total
		if len(pg.Items) == 0 || len(items) >= total {
			break
		}
	}
	return items, total, nil
}

// pageNote tells a person on stderr that a table shows only part of a list.
func pageNote(io *cli.IOStreams, format *printer.Format, p pageOpts, shown, total int, what string) {
	if p.all || (format.Kind != printer.Table && format.Kind != printer.Wide) {
		return
	}
	first := (p.page-1)*p.pageSize + 1
	if shown == 0 || first+shown-1 >= total {
		return
	}
	fmt.Fprintf(io.ErrOut, "Showing %s %d-%d of %d; pass --page %d for more, or --all.\n", what, first, first+shown-1, total, p.page+1)
}

func newRepoCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"repos", "repository", "repositories"},
		Short:   "List and delete repositories",
		Long: `List, inspect and delete the registry's repositories. A repository is a name
under your registry, such as team-a/api in registry.kupe.cloud/TENANT/team-a/api,
holding the images pushed to it.`,
	}
	cmd.AddCommand(newRepoListCmd(f))
	cmd.AddCommand(newRepoGetCmd(f))
	cmd.AddCommand(newRepoDeleteCmd(f))
	return cmd
}

func newRepoListCmd(f *cli.Factory) *cobra.Command {
	var output, search string
	var paging pageOpts
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the registry's repositories",
		Long: `List the registry's repositories, 25 to a page. --search keeps the ones whose
name contains the text. IMAGES counts every image, each platform image of a
multi-platform image as well as the multi-platform image itself.`,
		Args: cobra.NoArgs,
		Example: `  kupe registry repo list
  kupe registry repos list --search team-a/ --all -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := paging.validate(cmd); err != nil {
				return err
			}
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			items, total, err := collect(cmd.Context(), paging, func(ctx context.Context, page, size int) (*client.Page[client.Repository], error) {
				return api.ListRepositories(ctx, client.RegistryName, client.ListRepositoriesOptions{Search: search, Page: page, PageSize: size})
			})
			if err != nil {
				return translateErr(err, "")
			}
			if err := printer.Repository.List(f.IOStreams, format, items); err != nil {
				return err
			}
			pageNote(f.IOStreams, format, paging, len(items), total, "repositories")
			return nil
		},
	}
	cmd.Flags().StringVar(&search, "search", "", "Only repositories whose name contains this")
	paging.bind(cmd)
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newRepoGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "get REPOSITORY",
		Short:   "Show a repository",
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry repo get team-a/api`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			repo, err := parseRepository(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			r, err := api.GetRepository(cmd.Context(), client.RegistryName, repo)
			if err != nil {
				return translateErr(err, "")
			}
			return printer.Repository.One(f.IOStreams, format, r)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newRepoDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete REPOSITORY",
		Short: "Delete a repository and every image in it",
		Long: `Delete a repository with every image and tag in it. They cannot be restored.

On a TTY you must type the repository's name; pass --yes to skip the prompt
(required in non-interactive environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry repo delete team-a/old-api`,
		RunE: func(cmd *cobra.Command, args []string) error {
			io := f.IOStreams
			api, err := f.Client()
			if err != nil {
				return err
			}
			repo, err := parseRepository(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintln(io.ErrOut, "\nDeleting a repository deletes every image and tag in it.")
				fmt.Fprintln(io.ErrOut)
			}
			if err := cli.ConfirmDeleteByName(io, yes, "repository", repo); err != nil {
				return err
			}
			if err := api.DeleteRepository(cmd.Context(), client.RegistryName, repo); err != nil {
				return translateErr(err, "")
			}
			fmt.Fprintf(io.Out, "repository/%s deleted\n", repo)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}

func newImageCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "image",
		Aliases: []string{"images"},
		Short:   "List, inspect and delete images",
		Long: `List the images of a repository, show one, or delete one. Name an image as
REPOSITORY:TAG or REPOSITORY@sha256:DIGEST; the full reference you push to,
registry.kupe.cloud/TENANT/REPOSITORY:TAG, is accepted too.`,
	}
	cmd.AddCommand(newImageListCmd(f))
	cmd.AddCommand(newImageGetCmd(f))
	cmd.AddCommand(newImageDeleteCmd(f))
	return cmd
}

func newImageListCmd(f *cli.Factory) *cobra.Command {
	var output string
	var tagged, untagged bool
	var paging pageOpts
	cmd := &cobra.Command{
		Use:   "list REPOSITORY",
		Short: "List the images of a repository",
		Long: `List a repository's images, 25 to a page: digest, tags, size, platforms and
when it was pushed. --untagged shows only images no tag points at, which keep
using storage until they are deleted.`,
		Args: cobra.ExactArgs(1),
		Example: `  kupe registry image list team-a/api
  kupe registry image list team-a/api --untagged --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tagged && untagged {
				return cli.MisuseError("pass --tagged or --untagged, not both")
			}
			if err := paging.validate(cmd); err != nil {
				return err
			}
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			repo, err := parseRepository(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			opts := client.ListImagesOptions{}
			if tagged || untagged {
				opts.Tagged = &tagged
			}
			items, total, err := collect(cmd.Context(), paging, func(ctx context.Context, page, size int) (*client.Page[client.Image], error) {
				o := opts
				o.Page, o.PageSize = page, size
				return api.ListImages(ctx, client.RegistryName, repo, o)
			})
			if err != nil {
				return translateErr(err, "")
			}
			if err := printer.Image.List(f.IOStreams, format, items); err != nil {
				return err
			}
			pageNote(f.IOStreams, format, paging, len(items), total, "images")
			return nil
		},
	}
	cmd.Flags().BoolVar(&tagged, "tagged", false, "Only images with at least one tag")
	cmd.Flags().BoolVar(&untagged, "untagged", false, "Only images without a tag")
	paging.bind(cmd)
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newImageGetCmd(f *cli.Factory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get REPOSITORY:TAG|REPOSITORY@sha256:DIGEST",
		Short: "Show an image, with how to pull it",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe registry image get team-a/api:v1.4.2
  kupe registry image get registry.kupe.cloud/acme/team-a/api@sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			ref, err := parseImageRef(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			img, err := api.GetImage(cmd.Context(), client.RegistryName, ref.repository, ref.reference())
			if err != nil {
				return translateErr(err, "")
			}
			return printer.Image.One(f.IOStreams, format, img)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newImageDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete REPOSITORY@sha256:DIGEST",
		Short: "Delete an image by digest",
		Long: `Delete one image, named by digest, with every tag on it. It cannot be
restored. To remove only a tag, use "kupe registry tag delete REPOSITORY:TAG".
A platform image of a multi-platform image cannot be deleted on its own:
delete the multi-platform image instead.

Prompts on a TTY; pass --yes to skip (required in non-interactive
environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry image delete team-a/api@sha256:4f8c0d0e4b1f2b6e7e0c6a3d6c0f3a9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a`,
		RunE: func(cmd *cobra.Command, args []string) error {
			io := f.IOStreams
			api, err := f.Client()
			if err != nil {
				return err
			}
			ref, err := parseImageRef(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			if ref.digest == "" {
				return cli.MisuseError("delete an image by digest; to remove a tag use \"kupe registry tag delete " + ref.String() + "\"").
					WithHint("find the digest with:  kupe registry image get " + ref.String())
			}
			if !yes {
				fmt.Fprintln(io.ErrOut, "\nDeleting an image deletes every tag on it.")
				fmt.Fprintln(io.ErrOut)
			}
			if err := cli.ConfirmDelete(io, yes, "image", ref.String()); err != nil {
				return err
			}
			if err := api.DeleteImage(cmd.Context(), client.RegistryName, ref.repository, ref.digest); err != nil {
				return translateErr(err, "")
			}
			fmt.Fprintf(io.Out, "image/%s deleted\n", ref.String())
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}

func newTagCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag",
		Aliases: []string{"tags"},
		Short:   "Remove tags",
		Long: `Remove a tag from an image. The image stays, and an image left with no tag is
listed by "kupe registry image list REPOSITORY --untagged". Tags are listed
with their images.`,
	}
	cmd.AddCommand(newTagDeleteCmd(f))
	return cmd
}

func newTagDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete REPOSITORY:TAG",
		Short: "Remove a tag; the image stays",
		Long: `Remove one tag. The image it pointed at stays, and so do its other tags.

Prompts on a TTY; pass --yes to skip (required in non-interactive
environments).`,
		Args:    cobra.ExactArgs(1),
		Example: `  kupe registry tag delete team-a/api:old`,
		RunE: func(cmd *cobra.Command, args []string) error {
			io := f.IOStreams
			api, err := f.Client()
			if err != nil {
				return err
			}
			ref, err := parseImageRef(api.Tenant(), args[0])
			if err != nil {
				return err
			}
			if ref.tag == "" || ref.digest != "" {
				return cli.MisuseError(fmt.Sprintf("%q is not a tag", args[0])).
					WithHint("name the tag as REPOSITORY:TAG; to delete an image by digest use \"kupe registry image delete\"")
			}
			if err := cli.ConfirmDelete(io, yes, "tag", ref.String()); err != nil {
				return err
			}
			if err := api.DeleteTag(cmd.Context(), client.RegistryName, ref.repository, ref.tag); err != nil {
				return translateErr(err, "")
			}
			fmt.Fprintf(io.Out, "tag/%s deleted\n", ref.String())
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
