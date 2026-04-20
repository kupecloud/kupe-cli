// Package config loads, saves, and resolves the kupe CLI's YAML config file.
// The file lives at ~/.config/kupe/config.yaml by default (XDG-aware on Linux,
// %AppData%\kupe on Windows). Tokens are NEVER stored in this file —
// TokenRef points at the OS keyring or a separate plaintext credentials file
// (see internal/auth).
package config

// Well-known config file identity. Bumping these constitutes a breaking
// change; never change without a migration path.
const (
	APIVersion = "kupe.cloud/v1"
	Kind       = "Config"
)

// TokenRef values. An empty ref means the context has no stored credentials
// (e.g., after logout).
const (
	TokenRefKeyring   = "keyring"
	TokenRefPlaintext = "plaintext"
)

// DefaultAPIURL is the base URL used when nothing else resolves one.
const DefaultAPIURL = "https://api.kupe.cloud"

// Config is the root document.
type Config struct {
	APIVersion     string      `yaml:"apiVersion"`
	Kind           string      `yaml:"kind"`
	CurrentContext string      `yaml:"currentContext,omitempty"`
	Contexts       []Context   `yaml:"contexts,omitempty"`
	Preferences    Preferences `yaml:"preferences,omitempty"`
}

// Context is one named environment — e.g., "prod" or "staging" — binding an
// API URL and tenant together with a pointer at where this context's token
// lives (keyring / plaintext / none).
type Context struct {
	Name     string `yaml:"name"`
	APIURL   string `yaml:"apiUrl"`
	Tenant   string `yaml:"tenant"`
	TokenRef string `yaml:"tokenRef,omitempty"`
	User     string `yaml:"user,omitempty"`
}

// Preferences is a bag of global defaults applied to commands unless
// overridden by flags. All fields are optional.
type Preferences struct {
	Output      string `yaml:"output,omitempty"`
	Color       string `yaml:"color,omitempty"`
	Wait        *bool  `yaml:"wait,omitempty"`
	WaitTimeout string `yaml:"waitTimeout,omitempty"`
}

// New returns a fresh Config with the canonical apiVersion/kind set but no
// contexts. Used on first login when the config file doesn't yet exist.
func New() *Config {
	return &Config{
		APIVersion: APIVersion,
		Kind:       Kind,
	}
}

// Context returns a pointer to the named context, or nil if absent. Mutations
// through the returned pointer are visible in c.Contexts.
func (c *Config) Context(name string) *Context {
	if c == nil {
		return nil
	}
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

// CurrentCtx returns a pointer to the context named by CurrentContext, or nil.
func (c *Config) CurrentCtx() *Context {
	if c == nil || c.CurrentContext == "" {
		return nil
	}
	return c.Context(c.CurrentContext)
}

// SetContext adds or replaces a context by name.
func (c *Config) SetContext(ctx Context) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == ctx.Name {
			c.Contexts[i] = ctx
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
}

// RemoveContext deletes the named context. Clears CurrentContext if it
// pointed at the removed context. Returns true if a context was removed.
func (c *Config) RemoveContext(name string) bool {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			if c.CurrentContext == name {
				c.CurrentContext = ""
			}
			return true
		}
	}
	return false
}
