package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/imgajeed76/pgit/v3/internal/util"
)

// Config represents the .pgit/config.toml file
type Config struct {
	Core    CoreConfig              `toml:"core"`
	User    UserConfig              `toml:"user"`
	Remotes map[string]RemoteConfig `toml:"remote"`
}

// CoreConfig contains core repository settings
type CoreConfig struct {
	LocalDB string `toml:"local_db" config:"core.local_db" desc:"Local database name" readonly:"true"`
}

// UserConfig contains user information for commits
type UserConfig struct {
	Name  string `toml:"name" config:"user.name" desc:"Author name for commits"`
	Email string `toml:"email" config:"user.email" desc:"Author email for commits"`
}

// RemoteConfig contains remote repository settings
type RemoteConfig struct {
	URL string `toml:"url"` // PostgreSQL connection URL
}

// DefaultConfig returns a new config with default values
func DefaultConfig(repoPath string) *Config {
	return &Config{
		Core: CoreConfig{
			LocalDB: "pgit_" + util.HashPath(repoPath),
		},
		User:    UserConfig{},
		Remotes: make(map[string]RemoteConfig),
	}
}

// Load reads the config file from the repository
func Load(repoRoot string) (*Config, error) {
	configPath := util.ConfigPath(repoRoot)

	cfg := &Config{
		Remotes: make(map[string]RemoteConfig),
	}

	if _, err := toml.DecodeFile(configPath, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes the config file to the repository
func (c *Config) Save(repoRoot string) error {
	configPath := util.ConfigPath(repoRoot)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(c)
}

// GetRemote returns a remote config by name
func (c *Config) GetRemote(name string) (RemoteConfig, bool) {
	remote, ok := c.Remotes[name]
	return remote, ok
}

// SetRemote adds or updates a remote
func (c *Config) SetRemote(name string, url string) {
	if c.Remotes == nil {
		c.Remotes = make(map[string]RemoteConfig)
	}
	c.Remotes[name] = RemoteConfig{URL: url}
}

// RemoveRemote removes a remote by name
func (c *Config) RemoveRemote(name string) bool {
	if _, ok := c.Remotes[name]; ok {
		delete(c.Remotes, name)
		return true
	}
	return false
}

// GetValue returns a config value by key (uses reflection)
func (c *Config) GetValue(key string) (string, bool) {
	return getFieldValue(c, key)
}

// SetValue sets a config value by key (uses reflection with validation)
func (c *Config) SetValue(key, value string) error {
	return setFieldValue(c, key, value)
}

// GetUserName returns the user name from config or environment
func (c *Config) GetUserName() string {
	if c.User.Name != "" {
		return c.User.Name
	}
	if name := os.Getenv("PGIT_AUTHOR_NAME"); name != "" {
		return name
	}
	// Try to get from git config as fallback
	if name := getGitConfig("user.name"); name != "" {
		return name
	}
	return ""
}

// GetUserEmail returns the user email from config or environment
func (c *Config) GetUserEmail() string {
	if c.User.Email != "" {
		return c.User.Email
	}
	if email := os.Getenv("PGIT_AUTHOR_EMAIL"); email != "" {
		return email
	}
	// Try to get from git config as fallback
	if email := getGitConfig("user.email"); email != "" {
		return email
	}
	return ""
}

// getGitConfig tries to read a value from git config
func getGitConfig(key string) string {
	// Simple implementation - could use go-git for more robust parsing
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	gitconfig := filepath.Join(home, ".gitconfig")
	if _, err := os.Stat(gitconfig); err != nil {
		return ""
	}

	// Parse .gitconfig (simplified - just look for the key)
	type GitConfig struct {
		User struct {
			Name  string `toml:"name"`
			Email string `toml:"email"`
		} `toml:"user"`
	}

	var gc GitConfig
	if _, err := toml.DecodeFile(gitconfig, &gc); err != nil {
		return ""
	}

	switch key {
	case "user.name":
		return gc.User.Name
	case "user.email":
		return gc.User.Email
	}
	return ""
}
