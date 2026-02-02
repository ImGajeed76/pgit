package config

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ConfigScope indicates where a config option is available
type ConfigScope string

const (
	ScopeGlobal ConfigScope = "global" // Only in global config
	ScopeLocal  ConfigScope = "local"  // Only in local (repo) config
	ScopeBoth   ConfigScope = "both"   // Available in both
)

// ConfigField represents metadata about a config field extracted from struct tags
type ConfigField struct {
	Key      string      // e.g., "container.port"
	Default  string      // default value as string
	Desc     string      // description for help text
	Min      int         // minimum value for int fields (0 = no limit)
	Max      int         // maximum value for int fields (0 = no limit)
	Type     string      // "string" or "int"
	Category string      // e.g., "container", "import", "user"
	Scope    ConfigScope // where this config is available
	ReadOnly bool        // if true, cannot be set via CLI
}

// configFieldCache caches parsed config fields to avoid repeated reflection
var globalFieldCache []ConfigField
var localFieldCache []ConfigField

// getGlobalConfigFields extracts all config fields from GlobalConfig using reflection
func getGlobalConfigFields() []ConfigField {
	if globalFieldCache != nil {
		return globalFieldCache
	}

	var fields []ConfigField
	cfg := &GlobalConfig{}
	extractFields(reflect.ValueOf(cfg).Elem(), reflect.TypeOf(cfg).Elem(), "", &fields, ScopeGlobal)

	// Sort by key for consistent ordering
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Key < fields[j].Key
	})

	globalFieldCache = fields
	return fields
}

// getLocalConfigFields extracts all config fields from Config using reflection
func getLocalConfigFields() []ConfigField {
	if localFieldCache != nil {
		return localFieldCache
	}

	var fields []ConfigField
	cfg := &Config{}
	extractFields(reflect.ValueOf(cfg).Elem(), reflect.TypeOf(cfg).Elem(), "", &fields, ScopeLocal)

	// Sort by key for consistent ordering
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Key < fields[j].Key
	})

	localFieldCache = fields
	return fields
}

// extractFields recursively extracts config fields from a struct
func extractFields(v reflect.Value, t reflect.Type, prefix string, fields *[]ConfigField, defaultScope ConfigScope) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Skip map fields (like Remotes)
		if field.Type.Kind() == reflect.Map {
			continue
		}

		// Get the config key from tag
		configKey := field.Tag.Get("config")
		if configKey == "" {
			// Check if this is a nested struct (like ContainerConfig)
			if field.Type.Kind() == reflect.Struct {
				// Use toml tag as prefix for nested structs
				nestedPrefix := field.Tag.Get("toml")
				if nestedPrefix != "" {
					extractFields(value, field.Type, nestedPrefix, fields, defaultScope)
				}
			}
			continue
		}

		cf := ConfigField{
			Key:      configKey,
			Default:  field.Tag.Get("default"),
			Desc:     field.Tag.Get("desc"),
			Category: strings.Split(configKey, ".")[0],
			Scope:    defaultScope,
			ReadOnly: field.Tag.Get("readonly") == "true",
		}

		// Override scope if specified
		if scope := field.Tag.Get("scope"); scope != "" {
			cf.Scope = ConfigScope(scope)
		}

		// Parse min/max for validation
		if minStr := field.Tag.Get("min"); minStr != "" {
			cf.Min, _ = strconv.Atoi(minStr)
		}
		if maxStr := field.Tag.Get("max"); maxStr != "" {
			cf.Max, _ = strconv.Atoi(maxStr)
		}

		// Determine type
		switch field.Type.Kind() {
		case reflect.Int:
			cf.Type = "int"
		case reflect.String:
			cf.Type = "string"
		}

		*fields = append(*fields, cf)
	}
}

// findGlobalField finds a config field by key in global config
func findGlobalField(key string) *ConfigField {
	key = normalizeKey(key)
	for _, f := range getGlobalConfigFields() {
		if f.Key == key {
			return &f
		}
	}
	return nil
}

// findLocalField finds a config field by key in local config
func findLocalField(key string) *ConfigField {
	key = normalizeKey(key)
	for _, f := range getLocalConfigFields() {
		if f.Key == key {
			return &f
		}
	}
	return nil
}

// normalizeKey handles key aliases
func normalizeKey(key string) string {
	// Handle known aliases
	aliases := map[string]string{
		"container.shmsize": "container.shm_size",
	}
	if normalized, ok := aliases[key]; ok {
		return normalized
	}
	return key
}

// getFieldValue gets a field value from a config struct using reflection
func getFieldValue(cfg interface{}, key string) (string, bool) {
	key = normalizeKey(key)

	// Navigate to the correct nested struct
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return "", false
	}

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	// Find the nested struct by toml tag
	var nestedValue reflect.Value
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("toml") == parts[0] {
			nestedValue = v.Field(i)
			break
		}
	}

	if !nestedValue.IsValid() || nestedValue.Kind() != reflect.Struct {
		return "", false
	}

	// Find the actual field within the nested struct by config tag
	nestedType := nestedValue.Type()
	for i := 0; i < nestedType.NumField(); i++ {
		if nestedType.Field(i).Tag.Get("config") == key {
			fieldValue := nestedValue.Field(i)
			switch fieldValue.Kind() {
			case reflect.String:
				return fieldValue.String(), true
			case reflect.Int:
				return strconv.FormatInt(fieldValue.Int(), 10), true
			}
		}
	}

	return "", false
}

// setFieldValue sets a field value on a config struct using reflection
func setFieldValue(cfg interface{}, key, value string) error {
	key = normalizeKey(key)

	// Determine which field info to use based on config type
	var field *ConfigField
	switch cfg.(type) {
	case *GlobalConfig:
		field = findGlobalField(key)
	case *Config:
		field = findLocalField(key)
	default:
		return fmt.Errorf("unknown config type")
	}

	if field == nil {
		return fmt.Errorf("unknown config key: %s", key)
	}

	if field.ReadOnly {
		return fmt.Errorf("config key %s is read-only", key)
	}

	// Navigate to the correct nested struct
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid config key format: %s", key)
	}

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	// Find the nested struct by toml tag
	var nestedValue reflect.Value
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("toml") == parts[0] {
			nestedValue = v.Field(i)
			break
		}
	}

	if !nestedValue.IsValid() || nestedValue.Kind() != reflect.Struct {
		return fmt.Errorf("unknown config category: %s", parts[0])
	}

	// Find the actual field within the nested struct by config tag
	nestedType := nestedValue.Type()
	for i := 0; i < nestedType.NumField(); i++ {
		if nestedType.Field(i).Tag.Get("config") == key {
			fieldValue := nestedValue.Field(i)

			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
				return nil

			case reflect.Int:
				intVal, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid integer value: %s", value)
				}

				// Validate min/max
				if field.Min != 0 && intVal < field.Min {
					return fmt.Errorf("value %d is below minimum %d", intVal, field.Min)
				}
				if field.Max != 0 && intVal > field.Max {
					return fmt.Errorf("value %d exceeds maximum %d", intVal, field.Max)
				}

				fieldValue.SetInt(int64(intVal))
				return nil
			}
		}
	}

	return fmt.Errorf("field not found: %s", key)
}

// ListKeys returns all available global config keys
func ListKeys() []string {
	fields := getGlobalConfigFields()
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	return keys
}

// ListLocalKeys returns all available local config keys
func ListLocalKeys() []string {
	fields := getLocalConfigFields()
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		if !f.ReadOnly {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// GetFieldsByCategory returns global config fields grouped by category
func GetFieldsByCategory() map[string][]ConfigField {
	result := make(map[string][]ConfigField)
	for _, f := range getGlobalConfigFields() {
		result[f.Category] = append(result[f.Category], f)
	}
	return result
}

// GetLocalFieldsByCategory returns local config fields grouped by category
func GetLocalFieldsByCategory() map[string][]ConfigField {
	result := make(map[string][]ConfigField)
	for _, f := range getLocalConfigFields() {
		result[f.Category] = append(result[f.Category], f)
	}
	return result
}

// GenerateHelpText generates help text for global config options
func GenerateHelpText() string {
	var sb strings.Builder

	byCategory := GetFieldsByCategory()

	// Define category order and titles
	categories := []struct {
		key   string
		title string
	}{
		{"container", "Container"},
		{"import", "Import"},
		{"user", "User identity (used as default for new repositories)"},
	}

	for _, cat := range categories {
		fields, ok := byCategory[cat.key]
		if !ok || len(fields) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("  %s:\n", cat.title))
		for _, f := range fields {
			defaultStr := ""
			if f.Default != "" {
				defaultStr = fmt.Sprintf(" (default: %s)", f.Default)
			}
			// Pad key to align descriptions
			sb.WriteString(fmt.Sprintf("    %-35s %s%s\n", f.Key, f.Desc, defaultStr))
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// GenerateLocalHelpText generates help text for local config options
func GenerateLocalHelpText() string {
	var sb strings.Builder

	byCategory := GetLocalFieldsByCategory()

	// Define category order and titles
	categories := []struct {
		key   string
		title string
	}{
		{"user", "User identity"},
		{"core", "Core settings"},
	}

	for _, cat := range categories {
		fields, ok := byCategory[cat.key]
		if !ok || len(fields) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("  %s:\n", cat.title))
		for _, f := range fields {
			if f.ReadOnly {
				sb.WriteString(fmt.Sprintf("    %-35s %s (read-only)\n", f.Key, f.Desc))
			} else {
				sb.WriteString(fmt.Sprintf("    %-35s %s\n", f.Key, f.Desc))
			}
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
