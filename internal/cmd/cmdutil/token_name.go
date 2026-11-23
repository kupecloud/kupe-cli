package cmdutil

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// maxTokenNameRunes is the longest access token name kupe-api accepts.
const maxTokenNameRunes = 128

// CheckTokenName checks an access token's --name as kupe-api does: 1 to
// 128 printable characters. example is the hint's sample name.
func CheckTokenName(name, example string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return cli.MisuseError("--name is required").WithHint("e.g. --name " + example)
	case utf8.RuneCountInString(name) > maxTokenNameRunes:
		return cli.MisuseError("--name must be 128 characters or less")
	case strings.IndexFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return cli.MisuseError("--name must be printable text")
	}
	return nil
}
