package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	os.Unsetenv("ASTRA_SERVER_HOST")
	os.Unsetenv("ASTRA_DB_TYPE")

	err := LoadConfig()
	// Should not error even without config file
	assert.NoError(t, err)
}

func TestLoadConfig_TomlFile(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
[server]
host = "0.0.0.0"
port = 8080
domain = ["http://test.com"]

[db]
type = "sqlite"
path = ":memory:"

[sys_db]
type = "sqlite"
path = ":memory:"

[astra]
url = "http://localhost:9000"
token = "test-token"

[log]
debug = true
`
	configPath := filepath.Join(tmpDir, "config.toml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	err = LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", Configs.Server.Host)
	assert.Equal(t, 8080, Configs.Server.Port)
	assert.Equal(t, "sqlite", Configs.DB.Type)
}

func TestLoadConfig_EnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	os.Setenv("ASTRA_SERVER_HOST", "env-host")
	os.Setenv("ASTRA_SERVER_PORT", "3000")
	os.Setenv("ASTRA_DB_TYPE", "sqlite")
	os.Setenv("ASTRA_DB_PATH", ":memory:")
	os.Setenv("ASTRA_SYS_DB_TYPE", "sqlite")
	os.Setenv("ASTRA_SYS_DB_PATH", ":memory:")
	os.Setenv("ASTRA_ASTRA_URL", "http://env.api.com")
	os.Setenv("ASTRA_ASTRA_TOKEN", "env-token")
	defer func() {
		os.Unsetenv("ASTRA_SERVER_HOST")
		os.Unsetenv("ASTRA_SERVER_PORT")
		os.Unsetenv("ASTRA_DB_TYPE")
		os.Unsetenv("ASTRA_DB_PATH")
		os.Unsetenv("ASTRA_SYS_DB_TYPE")
		os.Unsetenv("ASTRA_SYS_DB_PATH")
		os.Unsetenv("ASTRA_ASTRA_URL")
		os.Unsetenv("ASTRA_ASTRA_TOKEN")
	}()

	err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "env-host", Configs.Server.Host)
	assert.Equal(t, 3000, Configs.Server.Port)
	assert.Equal(t, "env-token", Configs.Astra.Token)
}
