package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/BurntSushi/toml"
)

// GlobalConfig represents global pgit settings stored in user's config directory
// These settings affect all repositories and the local container
type GlobalConfig struct {
	Container ContainerConfig `toml:"container"`
	Import    ImportConfig    `toml:"import"`
}

// ContainerConfig contains Docker/Podman container settings
type ContainerConfig struct {
	ShmSize string `toml:"shm_size"` // Shared memory size (default: 256m)
	Port    int    `toml:"port"`     // PostgreSQL port (default: 5433)
	Image   string `toml:"image"`    // Custom image (default: ghcr.io/imgajeed76/pg-xpatch:latest)
}

// ImportConfig contains default import settings
type ImportConfig struct {
	Workers int `toml:"workers"` // Default number of workers (default: CPU count, max 3)
}

// DefaultGlobalConfig returns a new global config with default values
func DefaultGlobalConfig() *GlobalConfig {
	workers := runtime.NumCPU()
	if workers > 3 {
		workers = 3
	}

	return &GlobalConfig{
		Container: ContainerConfig{
			ShmSize: "256m",
			Port:    5433,
			Image:   "", // Empty means use default
		},
		Import: ImportConfig{
			Workers: workers,
		},
	}
}

// GlobalConfigPath returns the path to the global config file
// Follows XDG Base Directory spec on Linux, platform conventions elsewhere
func GlobalConfigPath() string {
	var configDir string

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, "Library", "Application Support", "pgit")
	case "windows":
		configDir = filepath.Join(os.Getenv("APPDATA"), "pgit")
	default: // Linux and others - follow XDG
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			configDir = filepath.Join(xdg, "pgit")
		} else {
			home, _ := os.UserHomeDir()
			configDir = filepath.Join(home, ".config", "pgit")
		}
	}

	return filepath.Join(configDir, "config.toml")
}

// LoadGlobal reads the global config file, creating defaults if it doesn't exist
func LoadGlobal() (*GlobalConfig, error) {
	configPath := GlobalConfigPath()

	// Start with defaults
	cfg := DefaultGlobalConfig()

	// Try to load existing config
	if _, err := os.Stat(configPath); err == nil {
		if _, err := toml.DecodeFile(configPath, cfg); err != nil {
			return nil, err
		}
	}

	// Apply defaults for any missing values
	if cfg.Container.ShmSize == "" {
		cfg.Container.ShmSize = "256m"
	}
	if cfg.Container.Port == 0 {
		cfg.Container.Port = 5433
	}
	if cfg.Import.Workers == 0 {
		workers := runtime.NumCPU()
		if workers > 3 {
			workers = 3
		}
		cfg.Import.Workers = workers
	}

	return cfg, nil
}

// Save writes the global config file
func (c *GlobalConfig) Save() error {
	configPath := GlobalConfigPath()

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

// GetGlobalValue returns a global config value by key
func (c *GlobalConfig) GetValue(key string) (string, bool) {
	switch key {
	case "container.shm_size", "container.shmsize":
		return c.Container.ShmSize, true
	case "container.port":
		return strconv.Itoa(c.Container.Port), true
	case "container.image":
		return c.Container.Image, true
	case "import.workers":
		return strconv.Itoa(c.Import.Workers), true
	default:
		return "", false
	}
}

// SetGlobalValue sets a global config value by key
func (c *GlobalConfig) SetValue(key, value string) error {
	switch key {
	case "container.shm_size", "container.shmsize":
		c.Container.ShmSize = value
	case "container.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if port < 1 || port > 65535 {
			return os.ErrInvalid
		}
		c.Container.Port = port
	case "container.image":
		c.Container.Image = value
	case "import.workers":
		workers, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if workers < 1 || workers > 16 {
			return os.ErrInvalid
		}
		c.Import.Workers = workers
	default:
		return os.ErrNotExist
	}
	return nil
}

// ListGlobalKeys returns all available global config keys
func ListGlobalKeys() []string {
	return []string{
		"container.shm_size",
		"container.port",
		"container.image",
		"import.workers",
	}
}
