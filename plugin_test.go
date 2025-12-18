// Package main provides tests for the NuGet plugin.
package main

import (
	"context"
	"os"
	"testing"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

func TestGetInfo(t *testing.T) {
	p := &NuGetPlugin{}
	info := p.GetInfo()

	tests := []struct {
		name     string
		check    func() bool
		expected string
	}{
		{
			name:     "name is nuget",
			check:    func() bool { return info.Name == "nuget" },
			expected: "nuget",
		},
		{
			name:     "version is set",
			check:    func() bool { return info.Version != "" },
			expected: "non-empty version",
		},
		{
			name:     "description is set",
			check:    func() bool { return info.Description != "" },
			expected: "non-empty description",
		},
		{
			name:     "author is set",
			check:    func() bool { return info.Author != "" },
			expected: "non-empty author",
		},
		{
			name:     "config schema is set",
			check:    func() bool { return info.ConfigSchema != "" },
			expected: "non-empty config schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check() {
				t.Errorf("expected %s", tt.expected)
			}
		})
	}

	// Check hooks separately for detailed verification
	t.Run("has PostPublish hook", func(t *testing.T) {
		if len(info.Hooks) == 0 {
			t.Fatal("expected at least one hook")
		}

		hasPostPublish := false
		for _, hook := range info.Hooks {
			if hook == plugin.HookPostPublish {
				hasPostPublish = true
				break
			}
		}
		if !hasPostPublish {
			t.Error("expected PostPublish hook")
		}
	})

	// Verify specific values
	t.Run("version is 2.0.0", func(t *testing.T) {
		if info.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got '%s'", info.Version)
		}
	})

	t.Run("description mentions NuGet", func(t *testing.T) {
		expected := "Publish packages to NuGet (.NET)"
		if info.Description != expected {
			t.Errorf("expected description '%s', got '%s'", expected, info.Description)
		}
	})

	t.Run("author is Relicta Team", func(t *testing.T) {
		if info.Author != "Relicta Team" {
			t.Errorf("expected author 'Relicta Team', got '%s'", info.Author)
		}
	})
}

func TestValidate(t *testing.T) {
	p := &NuGetPlugin{}
	ctx := context.Background()

	tests := []struct {
		name      string
		config    map[string]any
		envVars   map[string]string
		wantValid bool
	}{
		{
			name:      "empty config is valid",
			config:    map[string]any{},
			wantValid: true,
		},
		{
			name: "config with api_key is valid",
			config: map[string]any{
				"api_key": "test-api-key",
			},
			wantValid: true,
		},
		{
			name: "config with skip_duplicate is valid",
			config: map[string]any{
				"skip_duplicate": true,
			},
			wantValid: true,
		},
		{
			name: "config with all options is valid",
			config: map[string]any{
				"api_key":        "test-api-key",
				"source":         "https://api.nuget.org/v3/index.json",
				"skip_duplicate": true,
				"package_path":   "./artifacts/*.nupkg",
			},
			wantValid: true,
		},
		{
			name:    "config with env var fallback is valid",
			config:  map[string]any{},
			envVars: map[string]string{"NUGET_API_KEY": "env-api-key"},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected valid=%v, got valid=%v, errors=%v", tt.wantValid, resp.Valid, resp.Errors)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name            string
		config          map[string]any
		envVars         map[string]string
		expectedAPIKey  string
		expectedSkipDup bool
		expectedSource  string
	}{
		{
			name:            "defaults",
			config:          map[string]any{},
			expectedAPIKey:  "",
			expectedSkipDup: false,
			expectedSource:  "",
		},
		{
			name: "custom values",
			config: map[string]any{
				"api_key":        "my-api-key",
				"skip_duplicate": true,
				"source":         "https://custom.nuget.org/v3/index.json",
			},
			expectedAPIKey:  "my-api-key",
			expectedSkipDup: true,
			expectedSource:  "https://custom.nuget.org/v3/index.json",
		},
		{
			name:    "env var fallback for API key",
			config:  map[string]any{},
			envVars: map[string]string{"NUGET_API_KEY": "env-api-key"},
			expectedAPIKey:  "env-api-key",
			expectedSkipDup: false,
			expectedSource:  "",
		},
		{
			name: "config overrides env var",
			config: map[string]any{
				"api_key": "config-api-key",
			},
			envVars:         map[string]string{"NUGET_API_KEY": "env-api-key"},
			expectedAPIKey:  "config-api-key",
			expectedSkipDup: false,
			expectedSource:  "",
		},
		{
			name: "skip_duplicate flag false",
			config: map[string]any{
				"skip_duplicate": false,
			},
			expectedSkipDup: false,
		},
		{
			name: "skip_duplicate flag true",
			config: map[string]any{
				"skip_duplicate": true,
			},
			expectedSkipDup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}
			// Clear env var if not set in test
			if _, exists := tt.envVars["NUGET_API_KEY"]; !exists {
				_ = os.Unsetenv("NUGET_API_KEY")
			}

			// Parse config using helper functions
			apiKey := getStringWithEnv(tt.config, "api_key", "NUGET_API_KEY")
			skipDup := getBool(tt.config, "skip_duplicate")
			source := getString(tt.config, "source")

			if apiKey != tt.expectedAPIKey {
				t.Errorf("api_key: expected '%s', got '%s'", tt.expectedAPIKey, apiKey)
			}
			if skipDup != tt.expectedSkipDup {
				t.Errorf("skip_duplicate: expected %v, got %v", tt.expectedSkipDup, skipDup)
			}
			if source != tt.expectedSource {
				t.Errorf("source: expected '%s', got '%s'", tt.expectedSource, source)
			}
		})
	}
}

func TestExecuteDryRun(t *testing.T) {
	p := &NuGetPlugin{}
	ctx := context.Background()

	tests := []struct {
		name           string
		config         map[string]any
		releaseCtx     plugin.ReleaseContext
		expectedMsg    string
		expectedMsgAlt string
	}{
		{
			name:   "basic dry run",
			config: map[string]any{},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			expectedMsg: "Would execute nuget plugin",
		},
		{
			name: "dry run with config",
			config: map[string]any{
				"api_key":        "test-key",
				"skip_duplicate": true,
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v2.0.0",
			},
			expectedMsg: "Would execute nuget plugin",
		},
		{
			name: "dry run with source",
			config: map[string]any{
				"source": "https://api.nuget.org/v3/index.json",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.2.3",
			},
			expectedMsg: "Would execute nuget plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, resp.Message)
			}
		})
	}
}

func TestExecuteUnhandledHook(t *testing.T) {
	p := &NuGetPlugin{}
	ctx := context.Background()

	tests := []struct {
		name string
		hook plugin.Hook
	}{
		{
			name: "PreInit hook",
			hook: plugin.HookPreInit,
		},
		{
			name: "PostInit hook",
			hook: plugin.HookPostInit,
		},
		{
			name: "PrePlan hook",
			hook: plugin.HookPrePlan,
		},
		{
			name: "PostPlan hook",
			hook: plugin.HookPostPlan,
		},
		{
			name: "PreVersion hook",
			hook: plugin.HookPreVersion,
		},
		{
			name: "PostVersion hook",
			hook: plugin.HookPostVersion,
		},
		{
			name: "PreNotes hook",
			hook: plugin.HookPreNotes,
		},
		{
			name: "PostNotes hook",
			hook: plugin.HookPostNotes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:   tt.hook,
				Config: map[string]any{},
				DryRun: true,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !resp.Success {
				t.Error("expected success for unhandled hook")
			}

			expectedMsg := "Hook " + string(tt.hook) + " not handled"
			if resp.Message != expectedMsg {
				t.Errorf("expected message '%s', got '%s'", expectedMsg, resp.Message)
			}
		})
	}
}

func TestExecutePostPublish(t *testing.T) {
	p := &NuGetPlugin{}
	ctx := context.Background()

	tests := []struct {
		name        string
		config      map[string]any
		releaseCtx  plugin.ReleaseContext
		dryRun      bool
		wantSuccess bool
		wantMessage string
	}{
		{
			name:   "successful execution without dry run",
			config: map[string]any{},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:      false,
			wantSuccess: true,
			wantMessage: "NuGet plugin executed successfully",
		},
		{
			name:   "dry run execution",
			config: map[string]any{},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:      true,
			wantSuccess: true,
			wantMessage: "Would execute nuget plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseCtx,
				DryRun:  tt.dryRun,
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected success=%v, got success=%v", tt.wantSuccess, resp.Success)
			}

			if resp.Message != tt.wantMessage {
				t.Errorf("expected message '%s', got '%s'", tt.wantMessage, resp.Message)
			}
		})
	}
}

// Helper functions for parsing config (these would typically be in the main plugin file)
func getString(config map[string]any, key string) string {
	if v, ok := config[key].(string); ok {
		return v
	}
	return ""
}

func getStringWithEnv(config map[string]any, key, envVar string) string {
	if v, ok := config[key].(string); ok && v != "" {
		return v
	}
	return os.Getenv(envVar)
}

func getBool(config map[string]any, key string) bool {
	if v, ok := config[key].(bool); ok {
		return v
	}
	return false
}
