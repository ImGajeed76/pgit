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

	// PostgreSQL performance settings (passed as -c flags to postgres)
	MaxConnections       int    `toml:"max_connections"`         // Default: 200
	SharedBuffers        string `toml:"shared_buffers"`          // Default: 2GB
	WorkMem              string `toml:"work_mem"`                // Default: 64MB
	EffectiveCacheSize   string `toml:"effective_cache_size"`    // Default: 8GB
	MaxParallelWorkers   int    `toml:"max_parallel_workers"`    // Default: 8
	MaxWorkerProcesses   int    `toml:"max_worker_processes"`    // Default: 8
	MaxParallelPerGather int    `toml:"max_parallel_per_gather"` // Default: 4
}

// ImportConfig contains default import settings
type ImportConfig struct {
	Workers int `toml:"workers"` // Default number of workers (default: CPU count, max 3)
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

// GetGlobalValue returns a global config value by key
func (c *GlobalConfig) GetValue(key string) (string, bool) {
	switch key {
	case "container.shm_size", "container.shmsize":
		return c.Container.ShmSize, true
	case "container.port":
		return strconv.Itoa(c.Container.Port), true
	case "container.image":
		return c.Container.Image, true
	case "container.max_connections":
		return strconv.Itoa(c.Container.MaxConnections), true
	case "container.shared_buffers":
		return c.Container.SharedBuffers, true
	case "container.work_mem":
		return c.Container.WorkMem, true
	case "container.effective_cache_size":
		return c.Container.EffectiveCacheSize, true
	case "container.max_parallel_workers":
		return strconv.Itoa(c.Container.MaxParallelWorkers), true
	case "container.max_worker_processes":
		return strconv.Itoa(c.Container.MaxWorkerProcesses), true
	case "container.max_parallel_per_gather":
		return strconv.Itoa(c.Container.MaxParallelPerGather), true
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
	case "container.max_connections":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if v < 10 || v > 1000 {
			return os.ErrInvalid
		}
		c.Container.MaxConnections = v
	case "container.shared_buffers":
		c.Container.SharedBuffers = value
	case "container.work_mem":
		c.Container.WorkMem = value
	case "container.effective_cache_size":
		c.Container.EffectiveCacheSize = value
	case "container.max_parallel_workers":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if v < 0 || v > 64 {
			return os.ErrInvalid
		}
		c.Container.MaxParallelWorkers = v
	case "container.max_worker_processes":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if v < 1 || v > 64 {
			return os.ErrInvalid
		}
		c.Container.MaxWorkerProcesses = v
	case "container.max_parallel_per_gather":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if v < 0 || v > 16 {
			return os.ErrInvalid
		}
		c.Container.MaxParallelPerGather = v
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
		"container.max_connections",
		"container.shared_buffers",
		"container.work_mem",
		"container.effective_cache_size",
		"container.max_parallel_workers",
		"container.max_worker_processes",
		"container.max_parallel_per_gather",
		"import.workers",
	}
}
