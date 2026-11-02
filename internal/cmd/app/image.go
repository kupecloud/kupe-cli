package app

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func validateImageInput(image string) error {
	if image == "" || len(image) > 512 || strings.ContainsAny(image, "@\"'\\?#") && !digestImage.MatchString(image) || strings.IndexFunc(image, unicode.IsSpace) >= 0 || strings.IndexFunc(image, unicode.IsControl) >= 0 {
		return cli.MisuseError("image must be a tag or digest reference of at most 512 characters")
	}
	return nil
}
func newImageCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "image", Short: "Inspect an app image"}
	var output string
	resolve := &cobra.Command{Use: "resolve IMAGE", Short: "Resolve an image tag to its digest", Long: `Print the immutable image reference, platforms, numeric user and exposed TCP ports.
Use -o json for the raw resolution. Images must come from an approved registry,
support linux/amd64 and run with a non-root numeric USER.`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateImageInput(args[0]); err != nil {
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
		item, err := api.ResolveAppImage(cmd.Context(), args[0])
		if err != nil {
			return imageError(err)
		}
		if item == nil || !digestImage.MatchString(item.Reference) {
			return cli.New(cli.ExitUnavailable, "image resolver returned an invalid digest reference")
		}
		return printer.AppImage.One(f.IOStreams, format, item)
	}}
	resolve.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	cmd.AddCommand(resolve)
	return cmd
}

func imageError(err error) error {
	code, message := cli.ExitMisuse, ""
	switch client.ErrorCode(err) {
	case "image_not_found":
		code, message = cli.ExitNotFound, "image not found or not accessible"
	case "image_unavailable":
		code, message = cli.ExitUnavailable, "image registry is temporarily unavailable"
	case "image_platform_unsupported":
		message = "image does not support linux/amd64"
	case "image_policy_denied":
		message = "image must use an approved registry and a non-root numeric USER (for example USER 101)"
	default:
		return releaseRequestError(err, "", "", false)
	}
	result := cli.Wrap(code, message, &safeReleaseCause{cause: err})
	var api *client.APIError
	if client.ErrorCode(err) == "image_unavailable" && errors.As(err, &api) && api.RetryAfter > 0 {
		return result.WithHint(fmt.Sprintf("try again in %s", api.RetryAfter))
	}
	return result
}
