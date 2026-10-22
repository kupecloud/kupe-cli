package registry

import (
	"regexp"
	"strings"
)

// shellQuote quotes s as one POSIX shell word: it wraps s in single quotes
// and writes each single quote inside it as a closing quote, an escaped
// quote and an opening quote. Registry usernames (kupe+TENANT+NAME) need no
// quoting today; every username a printed command carries goes through this
// anyway, so a later username shape (a "$", a space) can never make a
// command unsafe to paste.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// plainWord is what a shell reads as itself, unquoted.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9._/:@%+=,-]+$`)

// shellArg is s as a shell word: as it is when nothing in it is special to
// the shell (a registry host), else quoted.
func shellArg(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// dockerLoginLine is the command that signs Docker in with a token held in
// the shell variable TOKEN. The secret itself is never part of it.
func dockerLoginLine(host, username string) string {
	return `printf '%s' "$TOKEN" | docker login ` + shellArg(host) + " --username " + shellQuote(username) + " --password-stdin"
}
