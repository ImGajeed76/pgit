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

	// WAL / checkpoint tuning
	MaxWalSize        string `toml:"max_wal_size" config:"container.max_wal_size" default:"4GB" desc:"Max WAL size before checkpoint"`
	CheckpointTimeout string `toml:"checkpoint_timeout" config:"container.checkpoint_timeout" default:"30min" desc:"Time between automatic checkpoints"`
	WalBuffers        string `toml:"wal_buffers" config:"container.wal_buffers" default:"64MB" desc:"WAL buffer size"`

	// pg-xpatch extension settings
	XpatchCacheSizeMB       int `toml:"xpatch_cache_size_mb" config:"container.xpatch_cache_size_mb" default:"256" min:"1" max:"65536" desc:"xpatch content cache size in MB"`
	XpatchCacheMaxEntries   int `toml:"xpatch_cache_max_entries" config:"container.xpatch_cache_max_entries" default:"65536" min:"1000" max:"2147483647" desc:"xpatch max cache entries"`
	XpatchCacheMaxEntryKB   int `toml:"xpatch_cache_max_entry_kb" config:"container.xpatch_cache_max_entry_kb" default:"4096" min:"16" max:"2147483647" desc:"xpatch max single cache entry size in KB"`
	XpatchCachePartitions   int `toml:"xpatch_cache_partitions" config:"container.xpatch_cache_partitions" default:"32" min:"1" max:"256" desc:"xpatch cache lock partitions"`
	XpatchEncodeThreads     int `toml:"xpatch_encode_threads" config:"container.xpatch_encode_threads" default:"0" min:"0" max:"64" desc:"xpatch parallel delta encoding threads (0=sequential)"`
	XpatchInsertCacheSlots  int `toml:"xpatch_insert_cache_slots" config:"container.xpatch_insert_cache_slots" default:"64" min:"1" max:"2147483647" desc:"xpatch insert cache FIFO slots"`
	XpatchGroupCacheSizeMB  int `toml:"xpatch_group_cache_size_mb" config:"container.xpatch_group_cache_size_mb" default:"16" min:"1" max:"2147483647" desc:"xpatch group max-seq cache in MB"`
	XpatchTidCacheSizeMB    int `toml:"xpatch_tid_cache_size_mb" config:"container.xpatch_tid_cache_size_mb" default:"16" min:"1" max:"2147483647" desc:"xpatch TID seq cache in MB"`
	XpatchSeqTidCacheSizeMB int `toml:"xpatch_seq_tid_cache_size_mb" config:"container.xpatch_seq_tid_cache_size_mb" default:"16" min:"1" max:"2147483647" desc:"xpatch seq-to-TID cache in MB"`
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
			ShmSize:                 "256m",
			Port:                    5433,
			Image:                   "", // Empty means use default
			MaxConnections:          100,
			SharedBuffers:           "256MB",
			WorkMem:                 "16MB",
			EffectiveCacheSize:      "1GB",
			MaxParallelWorkers:      4,
			MaxWorkerProcesses:      4,
			MaxParallelPerGather:    2,
			MaxWalSize:              "4GB",
			CheckpointTimeout:       "30min",
			WalBuffers:              "64MB",
			XpatchCacheSizeMB:       256,
			XpatchCacheMaxEntries:   65536,
			XpatchCacheMaxEntryKB:   4096,
			XpatchCachePartitions:   32,
			XpatchEncodeThreads:     0,
			XpatchInsertCacheSlots:  64,
			XpatchGroupCacheSizeMB:  16,
			XpatchTidCacheSizeMB:    16,
			XpatchSeqTidCacheSizeMB: 16,
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
	if cfg.Container.MaxWalSize == "" {
		cfg.Container.MaxWalSize = defaults.Container.MaxWalSize
	}
	if cfg.Container.CheckpointTimeout == "" {
		cfg.Container.CheckpointTimeout = defaults.Container.CheckpointTimeout
	}
	if cfg.Container.WalBuffers == "" {
		cfg.Container.WalBuffers = defaults.Container.WalBuffers
	}
	if cfg.Container.XpatchCacheSizeMB == 0 {
		cfg.Container.XpatchCacheSizeMB = defaults.Container.XpatchCacheSizeMB
	}
	if cfg.Container.XpatchCacheMaxEntries == 0 {
		cfg.Container.XpatchCacheMaxEntries = defaults.Container.XpatchCacheMaxEntries
	}
	if cfg.Container.XpatchCacheMaxEntryKB == 0 {
		cfg.Container.XpatchCacheMaxEntryKB = defaults.Container.XpatchCacheMaxEntryKB
	}
	if cfg.Container.XpatchCachePartitions == 0 {
		cfg.Container.XpatchCachePartitions = defaults.Container.XpatchCachePartitions
	}
	// NOTE: XpatchEncodeThreads is NOT defaulted here because 0 is a valid
	// value (sequential encoding). The default in DefaultGlobalConfig() is 0.
	if cfg.Container.XpatchInsertCacheSlots == 0 {
		cfg.Container.XpatchInsertCacheSlots = defaults.Container.XpatchInsertCacheSlots
	}
	if cfg.Container.XpatchGroupCacheSizeMB == 0 {
		cfg.Container.XpatchGroupCacheSizeMB = defaults.Container.XpatchGroupCacheSizeMB
	}
	if cfg.Container.XpatchTidCacheSizeMB == 0 {
		cfg.Container.XpatchTidCacheSizeMB = defaults.Container.XpatchTidCacheSizeMB
	}
	if cfg.Container.XpatchSeqTidCacheSizeMB == 0 {
		cfg.Container.XpatchSeqTidCacheSizeMB = defaults.Container.XpatchSeqTidCacheSizeMB
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
