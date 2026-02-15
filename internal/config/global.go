package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// GlobalConfig represents global pgit settings stored in user's config directory
// These settings affect all repositories and the local container
type GlobalConfig struct {
	Container ContainerConfig  `toml:"container"`
	Import    ImportConfig     `toml:"import"`
	User      GlobalUserConfig `toml:"user"`
}

// GlobalUserConfig contains default user identity settings
type GlobalUserConfig struct {
	Name  string `toml:"name" config:"user.name" desc:"Default author name"`
	Email string `toml:"email" config:"user.email" desc:"Default author email"`
}

// ContainerConfig contains Docker/Podman container settings
type ContainerConfig struct {
	ShmSize string `toml:"shm_size" config:"container.shm_size" default:"256m" desc:"Shared memory for PostgreSQL"`
	Port    int    `toml:"port" config:"container.port" default:"5433" min:"1" max:"65535" desc:"PostgreSQL port"`
	Image   string `toml:"image" config:"container.image" desc:"Custom pg-xpatch image (empty = default)"`

	// PostgreSQL performance settings (passed as -c flags to postgres)
	MaxConnections       int    `toml:"max_connections" config:"container.max_connections" default:"100" min:"10" max:"1000" desc:"Max database connections"`
	SharedBuffers        string `toml:"shared_buffers" config:"container.shared_buffers" default:"256MB" desc:"Shared buffer size"`
	WorkMem              string `toml:"work_mem" config:"container.work_mem" default:"16MB" desc:"Work memory per operation"`
	EffectiveCacheSize   string `toml:"effective_cache_size" config:"container.effective_cache_size" default:"1GB" desc:"Planner cache size hint"`
	MaxParallelWorkers   int    `toml:"max_parallel_workers" config:"container.max_parallel_workers" default:"4" min:"0" max:"64" desc:"Max parallel workers"`
	MaxWorkerProcesses   int    `toml:"max_worker_processes" config:"container.max_worker_processes" default:"4" min:"1" max:"64" desc:"Max worker processes"`
	MaxParallelPerGather int    `toml:"max_parallel_per_gather" config:"container.max_parallel_per_gather" default:"2" min:"0" max:"16" desc:"Workers per gather"`

	// pg-xpatch extension settings
	XpatchCacheSizeMB int `toml:"xpatch_cache_size_mb" config:"container.xpatch_cache_size_mb" default:"256" min:"16" max:"4096" desc:"xpatch content cache size in MB"`
}

// ImportConfig contains default import settings
type ImportConfig struct {
	Workers int `toml:"workers" config:"import.workers" default:"3" min:"1" max:"16" desc:"Default import workers"`
}

// DefaultGlobalConfig returns a new global config with default values
// Uses conservative defaults suitable for laptops (8GB RAM, 4 cores)
func DefaultGlobalConfig() *GlobalConfig {
	workers := runtime.NumCPU()
	if workers > 3 {
		workers = 3
	}

	// Conservative defaults - suitable for 8GB RAM laptop
	// Users with more resources can increase via pgit config --global
	return &GlobalConfig{
		Container: ContainerConfig{
			ShmSize:              "256m",
			Port:                 5433,
			Image:                "", // Empty means use default
			MaxConnections:       100,
			SharedBuffers:        "256MB",
			WorkMem:              "16MB",
			EffectiveCacheSize:   "1GB",
			MaxParallelWorkers:   4,
			MaxWorkerProcesses:   4,
			MaxParallelPerGather: 2,
			XpatchCacheSizeMB:    256,
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
	defaults := DefaultGlobalConfig()

	if cfg.Container.ShmSize == "" {
		cfg.Container.ShmSize = defaults.Container.ShmSize
	}
	if cfg.Container.Port == 0 {
		cfg.Container.Port = defaults.Container.Port
	}
	if cfg.Container.MaxConnections == 0 {
		cfg.Container.MaxConnections = defaults.Container.MaxConnections
	}
	if cfg.Container.SharedBuffers == "" {
		cfg.Container.SharedBuffers = defaults.Container.SharedBuffers
	}
	if cfg.Container.WorkMem == "" {
		cfg.Container.WorkMem = defaults.Container.WorkMem
	}
	if cfg.Container.EffectiveCacheSize == "" {
		cfg.Container.EffectiveCacheSize = defaults.Container.EffectiveCacheSize
	}
	if cfg.Container.MaxParallelWorkers == 0 {
		cfg.Container.MaxParallelWorkers = defaults.Container.MaxParallelWorkers
	}
	if cfg.Container.MaxWorkerProcesses == 0 {
		cfg.Container.MaxWorkerProcesses = defaults.Container.MaxWorkerProcesses
	}
	if cfg.Container.MaxParallelPerGather == 0 {
		cfg.Container.MaxParallelPerGather = defaults.Container.MaxParallelPerGather
	}
	if cfg.Container.XpatchCacheSizeMB == 0 {
		cfg.Container.XpatchCacheSizeMB = defaults.Container.XpatchCacheSizeMB
	}
	if cfg.Import.Workers == 0 {
		cfg.Import.Workers = defaults.Import.Workers
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

// GetValue returns a global config value by key (uses reflection)
func (c *GlobalConfig) GetValue(key string) (string, bool) {
	return getFieldValue(c, key)
}

// SetValue sets a global config value by key (uses reflection with validation)
func (c *GlobalConfig) SetValue(key, value string) error {
	return setFieldValue(c, key, value)
}

// ListGlobalKeys returns all available global config keys (uses reflection)
func ListGlobalKeys() []string {
	return ListKeys()
}
