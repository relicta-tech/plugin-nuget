// Package main provides tests for the NuGet plugin.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// MockCommandExecutor is a mock implementation of CommandExecutor for testing.
type MockCommandExecutor struct {
	RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
	Calls   []MockCall
}

// MockCall records a call to the executor.
type MockCall struct {
	Name string
	Args []string
}

// Run implements CommandExecutor.
func (m *MockCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, MockCall{Name: name, Args: args})
	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args...)
	}
	return nil, nil
}

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
			name:      "empty config is valid (api_key can come from env)",
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
				"api_key":        "test-api-key",
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
				"package_path":   "artifacts/*.nupkg",
				"timeout":        600,
			},
			wantValid: true,
		},
		{
			name:      "config with env var fallback is valid",
			config:    map[string]any{},
			envVars:   map[string]string{"NUGET_API_KEY": "env-api-key"},
			wantValid: true,
		},
		{
			name: "invalid source URL (HTTP)",
			config: map[string]any{
				"api_key": "test-api-key",
				"source":  "http://nuget.example.com/v3/index.json",
			},
			wantValid: false,
		},
		{
			name: "invalid package path (path traversal)",
			config: map[string]any{
				"api_key":      "test-api-key",
				"package_path": "../../../etc/passwd",
			},
			wantValid: false,
		},
		{
			name: "invalid timeout (zero)",
			config: map[string]any{
				"api_key": "test-api-key",
				"timeout": 0,
			},
			wantValid: false,
		},
		{
			name: "invalid timeout (negative)",
			config: map[string]any{
				"api_key": "test-api-key",
				"timeout": -100,
			},
			wantValid: false,
		},
		{
			name: "localhost source is valid with HTTP",
			config: map[string]any{
				"api_key": "test-api-key",
				"source":  "http://localhost:5000/v3/index.json",
			},
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
			// Clear env var if not set in test
			if _, exists := tt.envVars["NUGET_API_KEY"]; !exists {
				_ = os.Unsetenv("NUGET_API_KEY")
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
		expectedPath    string
		expectedTimeout int
	}{
		{
			name:            "defaults",
			config:          map[string]any{},
			expectedAPIKey:  "",
			expectedSkipDup: false,
			expectedSource:  DefaultSource,
			expectedPath:    DefaultPackagePath,
			expectedTimeout: DefaultTimeout,
		},
		{
			name: "custom values",
			config: map[string]any{
				"api_key":        "my-api-key",
				"skip_duplicate": true,
				"source":         "https://custom.nuget.org/v3/index.json",
				"package_path":   "build/*.nupkg",
				"timeout":        600,
			},
			expectedAPIKey:  "my-api-key",
			expectedSkipDup: true,
			expectedSource:  "https://custom.nuget.org/v3/index.json",
			expectedPath:    "build/*.nupkg",
			expectedTimeout: 600,
		},
		{
			name:            "env var fallback for API key",
			config:          map[string]any{},
			envVars:         map[string]string{"NUGET_API_KEY": "env-api-key"},
			expectedAPIKey:  "env-api-key",
			expectedSkipDup: false,
			expectedSource:  DefaultSource,
			expectedPath:    DefaultPackagePath,
			expectedTimeout: DefaultTimeout,
		},
		{
			name: "config overrides env var",
			config: map[string]any{
				"api_key": "config-api-key",
			},
			envVars:         map[string]string{"NUGET_API_KEY": "env-api-key"},
			expectedAPIKey:  "config-api-key",
			expectedSkipDup: false,
			expectedSource:  DefaultSource,
			expectedPath:    DefaultPackagePath,
			expectedTimeout: DefaultTimeout,
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

			p := &NuGetPlugin{}
			cfg := p.parseConfig(tt.config)

			if cfg.APIKey != tt.expectedAPIKey {
				t.Errorf("api_key: expected '%s', got '%s'", tt.expectedAPIKey, cfg.APIKey)
			}
			if cfg.SkipDuplicate != tt.expectedSkipDup {
				t.Errorf("skip_duplicate: expected %v, got %v", tt.expectedSkipDup, cfg.SkipDuplicate)
			}
			if cfg.Source != tt.expectedSource {
				t.Errorf("source: expected '%s', got '%s'", tt.expectedSource, cfg.Source)
			}
			if cfg.PackagePath != tt.expectedPath {
				t.Errorf("package_path: expected '%s', got '%s'", tt.expectedPath, cfg.PackagePath)
			}
			if cfg.Timeout != tt.expectedTimeout {
				t.Errorf("timeout: expected %d, got %d", tt.expectedTimeout, cfg.Timeout)
			}
		})
	}
}

func TestExecuteDryRun(t *testing.T) {
	// Create a temporary directory with a test package
	tmpDir := t.TempDir()
	testPkg := filepath.Join(tmpDir, "test.1.0.0.nupkg")
	if err := os.WriteFile(testPkg, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	p := &NuGetPlugin{}
	ctx := context.Background()

	tests := []struct {
		name        string
		config      map[string]any
		releaseCtx  plugin.ReleaseContext
		wantSuccess bool
		wantMsg     string
	}{
		{
			name: "basic dry run",
			config: map[string]any{
				"api_key":      "test-key",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			wantSuccess: true,
			wantMsg:     "Would push 1 package(s) to NuGet",
		},
		{
			name: "dry run with skip_duplicate",
			config: map[string]any{
				"api_key":        "test-key",
				"skip_duplicate": true,
				"package_path":   filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v2.0.0",
			},
			wantSuccess: true,
			wantMsg:     "Would push 1 package(s) to NuGet",
		},
		{
			name: "dry run with localhost source",
			config: map[string]any{
				"api_key":      "test-key",
				"source":       "http://localhost:5000/v3/index.json",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.2.3",
			},
			wantSuccess: true,
			wantMsg:     "Would push 1 package(s) to NuGet",
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

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected success=%v, got success=%v, error: %s", tt.wantSuccess, resp.Success, resp.Error)
			}

			if resp.Message != tt.wantMsg {
				t.Errorf("expected message '%s', got '%s'", tt.wantMsg, resp.Message)
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
	// Create a temporary directory with test packages
	tmpDir := t.TempDir()
	testPkg := filepath.Join(tmpDir, "test.1.0.0.nupkg")
	if err := os.WriteFile(testPkg, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name          string
		config        map[string]any
		releaseCtx    plugin.ReleaseContext
		dryRun        bool
		mockError     error
		mockOutput    []byte
		wantSuccess   bool
		wantMsgPrefix string
		wantCalls     int
	}{
		{
			name: "successful push",
			config: map[string]any{
				"api_key":      "test-key",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:        false,
			mockError:     nil,
			mockOutput:    []byte("Package pushed successfully"),
			wantSuccess:   true,
			wantMsgPrefix: "Successfully pushed",
			wantCalls:     1,
		},
		{
			name: "push failure",
			config: map[string]any{
				"api_key":      "test-key",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:        false,
			mockError:     errors.New("authentication failed"),
			mockOutput:    []byte("401 Unauthorized"),
			wantSuccess:   false,
			wantMsgPrefix: "",
			wantCalls:     1,
		},
		{
			name: "dry run skips push",
			config: map[string]any{
				"api_key":      "test-key",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:        true,
			wantSuccess:   true,
			wantMsgPrefix: "Would push",
			wantCalls:     0,
		},
		{
			name: "push with skip_duplicate",
			config: map[string]any{
				"api_key":        "test-key",
				"skip_duplicate": true,
				"package_path":   filepath.Join(tmpDir, "*.nupkg"),
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "v1.0.0",
			},
			dryRun:        false,
			mockError:     nil,
			mockOutput:    []byte("Package pushed successfully"),
			wantSuccess:   true,
			wantMsgPrefix: "Successfully pushed",
			wantCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExec := &MockCommandExecutor{
				RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
					return tt.mockOutput, tt.mockError
				},
			}

			p := &NuGetPlugin{cmdExecutor: mockExec}

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
				t.Errorf("expected success=%v, got success=%v, error: %s", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.wantMsgPrefix != "" && len(resp.Message) < len(tt.wantMsgPrefix) {
				t.Errorf("expected message to start with '%s', got '%s'", tt.wantMsgPrefix, resp.Message)
			} else if tt.wantMsgPrefix != "" && resp.Message[:len(tt.wantMsgPrefix)] != tt.wantMsgPrefix {
				t.Errorf("expected message to start with '%s', got '%s'", tt.wantMsgPrefix, resp.Message)
			}

			if len(mockExec.Calls) != tt.wantCalls {
				t.Errorf("expected %d calls, got %d", tt.wantCalls, len(mockExec.Calls))
			}
		})
	}
}

func TestExecutePush_CommandArguments(t *testing.T) {
	// Create a temporary directory with a test package
	tmpDir := t.TempDir()
	testPkg := filepath.Join(tmpDir, "test.1.0.0.nupkg")
	if err := os.WriteFile(testPkg, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name           string
		config         map[string]any
		expectedArgs   []string
		unexpectedArgs []string
	}{
		{
			name: "basic push arguments",
			config: map[string]any{
				"api_key":      "my-secret-key",
				"source":       "https://api.nuget.org/v3/index.json",
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
				"timeout":      300,
			},
			expectedArgs: []string{
				"nuget", "push",
				"--api-key", "my-secret-key",
				"--source", "https://api.nuget.org/v3/index.json",
				"--timeout", "300",
			},
			unexpectedArgs: []string{"--skip-duplicate"},
		},
		{
			name: "push with skip_duplicate",
			config: map[string]any{
				"api_key":        "my-secret-key",
				"skip_duplicate": true,
				"package_path":   filepath.Join(tmpDir, "*.nupkg"),
			},
			expectedArgs: []string{
				"nuget", "push",
				"--api-key", "my-secret-key",
				"--skip-duplicate",
			},
		},
		{
			name: "push with custom timeout",
			config: map[string]any{
				"api_key":      "my-secret-key",
				"timeout":      600,
				"package_path": filepath.Join(tmpDir, "*.nupkg"),
			},
			expectedArgs: []string{
				"--timeout", "600",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExec := &MockCommandExecutor{
				RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
					return []byte("success"), nil
				},
			}

			p := &NuGetPlugin{cmdExecutor: mockExec}

			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: plugin.ReleaseContext{Version: "v1.0.0"},
				DryRun:  false,
			}

			_, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(mockExec.Calls) == 0 {
				t.Fatal("expected at least one call")
			}

			call := mockExec.Calls[0]
			if call.Name != "dotnet" {
				t.Errorf("expected command 'dotnet', got '%s'", call.Name)
			}

			// Check for expected arguments
			argsStr := join(call.Args)
			for _, expected := range tt.expectedArgs {
				if !contains(call.Args, expected) {
					t.Errorf("expected argument '%s' not found in: %s", expected, argsStr)
				}
			}

			// Check that unexpected arguments are not present
			for _, unexpected := range tt.unexpectedArgs {
				if contains(call.Args, unexpected) {
					t.Errorf("unexpected argument '%s' found in: %s", unexpected, argsStr)
				}
			}
		})
	}
}

func TestValidateSourceURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid HTTPS URL",
			url:     "https://api.nuget.org/v3/index.json",
			wantErr: false,
		},
		{
			name:    "valid localhost HTTP",
			url:     "http://localhost:5000/v3/index.json",
			wantErr: false,
		},
		{
			name:    "valid 127.0.0.1 HTTP",
			url:     "http://127.0.0.1:5000/v3/index.json",
			wantErr: false,
		},
		{
			name:    "invalid HTTP non-localhost",
			url:     "http://nuget.example.com/v3/index.json",
			wantErr: true,
			errMsg:  "only HTTPS URLs are allowed",
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
			errMsg:  "source URL cannot be empty",
		},
		{
			name:    "invalid URL format",
			url:     "not-a-url",
			wantErr: true,
			errMsg:  "only HTTPS URLs are allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSourceURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSourceURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestValidatePackagePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple pattern",
			path:    "*.nupkg",
			wantErr: false,
		},
		{
			name:    "valid directory pattern",
			path:    "artifacts/*.nupkg",
			wantErr: false,
		},
		{
			name:    "valid nested pattern",
			path:    "output/packages/*.nupkg",
			wantErr: false,
		},
		{
			name:    "path traversal attempt",
			path:    "../../../etc/passwd",
			wantErr: true,
			errMsg:  "path traversal detected",
		},
		{
			name:    "path traversal in middle",
			path:    "artifacts/../../../etc/passwd",
			wantErr: true,
			errMsg:  "path traversal detected",
		},
		{
			name:    "absolute path is allowed",
			path:    "/tmp/packages/*.nupkg",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
			errMsg:  "package path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackagePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePackagePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestFindPackages(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create test package files
	testFiles := []string{
		"test.1.0.0.nupkg",
		"test.1.0.1.nupkg",
		"other.2.0.0.nupkg",
		"readme.txt",
		"config.json",
	}

	for _, f := range testFiles {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", f, err)
		}
	}

	p := &NuGetPlugin{}

	tests := []struct {
		name        string
		pattern     string
		wantCount   int
		wantErr     bool
		wantPackage string
	}{
		{
			name:      "find all nupkg files",
			pattern:   filepath.Join(tmpDir, "*.nupkg"),
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:        "find specific package",
			pattern:     filepath.Join(tmpDir, "test.1.0.0.nupkg"),
			wantCount:   1,
			wantErr:     false,
			wantPackage: "test.1.0.0.nupkg",
		},
		{
			name:      "no matches",
			pattern:   filepath.Join(tmpDir, "nonexistent*.nupkg"),
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "path traversal blocked",
			pattern: "../../../etc/*.nupkg",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages, err := p.findPackages(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("findPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(packages) != tt.wantCount {
					t.Errorf("expected %d packages, got %d", tt.wantCount, len(packages))
				}
				if tt.wantPackage != "" && len(packages) > 0 {
					found := false
					for _, pkg := range packages {
						if filepath.Base(pkg) == tt.wantPackage {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected to find package '%s'", tt.wantPackage)
					}
				}
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	p := &NuGetPlugin{}

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				APIKey:        "test-key",
				Source:        "https://api.nuget.org/v3/index.json",
				PackagePath:   "*.nupkg",
				SkipDuplicate: false,
				Timeout:       300,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: &Config{
				APIKey:      "",
				Source:      "https://api.nuget.org/v3/index.json",
				PackagePath: "*.nupkg",
				Timeout:     300,
			},
			wantErr: true,
			errMsg:  "API key is required",
		},
		{
			name: "invalid source URL",
			config: &Config{
				APIKey:      "test-key",
				Source:      "http://not-localhost.com/v3/index.json",
				PackagePath: "*.nupkg",
				Timeout:     300,
			},
			wantErr: true,
			errMsg:  "invalid source URL",
		},
		{
			name: "invalid package path",
			config: &Config{
				APIKey:      "test-key",
				Source:      "https://api.nuget.org/v3/index.json",
				PackagePath: "../../../etc/passwd",
				Timeout:     300,
			},
			wantErr: true,
			errMsg:  "invalid package path",
		},
		{
			name: "zero timeout",
			config: &Config{
				APIKey:      "test-key",
				Source:      "https://api.nuget.org/v3/index.json",
				PackagePath: "*.nupkg",
				Timeout:     0,
			},
			wantErr: true,
			errMsg:  "timeout must be a positive integer",
		},
		{
			name: "negative timeout",
			config: &Config{
				APIKey:      "test-key",
				Source:      "https://api.nuget.org/v3/index.json",
				PackagePath: "*.nupkg",
				Timeout:     -100,
			},
			wantErr: true,
			errMsg:  "timeout must be a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestNoPackagesFound(t *testing.T) {
	tmpDir := t.TempDir()

	p := &NuGetPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_key":      "test-key",
			"package_path": filepath.Join(tmpDir, "*.nupkg"),
		},
		Context: plugin.ReleaseContext{Version: "v1.0.0"},
		DryRun:  false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Success {
		t.Error("expected failure when no packages found")
	}

	if !containsString(resp.Error, "no packages found") {
		t.Errorf("expected error about no packages found, got: %s", resp.Error)
	}
}

func TestMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	_ = os.Unsetenv("NUGET_API_KEY")

	tmpDir := t.TempDir()
	testPkg := filepath.Join(tmpDir, "test.1.0.0.nupkg")
	if err := os.WriteFile(testPkg, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	p := &NuGetPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"package_path": filepath.Join(tmpDir, "*.nupkg"),
		},
		Context: plugin.ReleaseContext{Version: "v1.0.0"},
		DryRun:  false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Success {
		t.Error("expected failure when API key is missing")
	}

	if !containsString(resp.Error, "API key is required") {
		t.Errorf("expected error about missing API key, got: %s", resp.Error)
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		{
			name:      "public IP",
			ip:        "8.8.8.8",
			isPrivate: false,
		},
		{
			name:      "private 10.x.x.x",
			ip:        "10.0.0.1",
			isPrivate: true,
		},
		{
			name:      "private 172.16.x.x",
			ip:        "172.16.0.1",
			isPrivate: true,
		},
		{
			name:      "private 192.168.x.x",
			ip:        "192.168.1.1",
			isPrivate: true,
		},
		{
			name:      "loopback",
			ip:        "127.0.0.1",
			isPrivate: true,
		},
		{
			name:      "AWS metadata",
			ip:        "169.254.169.254",
			isPrivate: true,
		},
		{
			name:      "link-local",
			ip:        "169.254.1.1",
			isPrivate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := parseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.isPrivate {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.isPrivate)
			}
		})
	}
}

func TestMultiplePackages(t *testing.T) {
	// Create a temporary directory with multiple test packages
	tmpDir := t.TempDir()
	packages := []string{
		"package1.1.0.0.nupkg",
		"package2.1.0.0.nupkg",
		"package3.1.0.0.nupkg",
	}

	for _, pkg := range packages {
		path := filepath.Join(tmpDir, pkg)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test package %s: %v", pkg, err)
		}
	}

	ctx := context.Background()
	callCount := 0

	mockExec := &MockCommandExecutor{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			callCount++
			return []byte("success"), nil
		},
	}

	p := &NuGetPlugin{cmdExecutor: mockExec}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_key":      "test-key",
			"package_path": filepath.Join(tmpDir, "*.nupkg"),
		},
		Context: plugin.ReleaseContext{Version: "v1.0.0"},
		DryRun:  false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	if callCount != 3 {
		t.Errorf("expected 3 push calls, got %d", callCount)
	}

	if !containsString(resp.Message, "3 package(s)") {
		t.Errorf("expected message to mention 3 packages, got: %s", resp.Message)
	}
}

func TestPartialPushFailure(t *testing.T) {
	// Create a temporary directory with multiple test packages
	tmpDir := t.TempDir()
	packages := []string{
		"package1.1.0.0.nupkg",
		"package2.1.0.0.nupkg",
		"package3.1.0.0.nupkg",
	}

	for _, pkg := range packages {
		path := filepath.Join(tmpDir, pkg)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test package %s: %v", pkg, err)
		}
	}

	ctx := context.Background()
	callCount := 0

	mockExec := &MockCommandExecutor{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			callCount++
			if callCount == 2 {
				return []byte("authentication failed"), errors.New("push failed")
			}
			return []byte("success"), nil
		},
	}

	p := &NuGetPlugin{cmdExecutor: mockExec}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_key":      "test-key",
			"package_path": filepath.Join(tmpDir, "*.nupkg"),
		},
		Context: plugin.ReleaseContext{Version: "v1.0.0"},
		DryRun:  false,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Success {
		t.Error("expected failure on partial push")
	}

	if !containsString(resp.Error, "failed to push package") {
		t.Errorf("expected error about failed push, got: %s", resp.Error)
	}

	// Check that outputs contain pushed packages
	if resp.Outputs != nil {
		pushedPkgs, ok := resp.Outputs["pushed_packages"].([]string)
		if ok && len(pushedPkgs) != 1 {
			t.Errorf("expected 1 pushed package before failure, got %d", len(pushedPkgs))
		}
	}
}

// Helper functions

func parseIP(s string) []byte {
	var result [4]byte
	var parts [4]int
	n := parseIPv4(s, &parts)
	if n != 4 {
		return nil
	}
	for i, p := range parts {
		if p < 0 || p > 255 {
			return nil
		}
		result[i] = byte(p)
	}
	return result[:]
}

func parseIPv4(s string, parts *[4]int) int {
	count := 0
	val := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			if count >= 3 {
				return count
			}
			parts[count] = val
			count++
			val = 0
		} else if s[i] >= '0' && s[i] <= '9' {
			val = val*10 + int(s[i]-'0')
		} else {
			return count
		}
	}
	if count == 3 {
		parts[count] = val
		count++
	}
	return count
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func join(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}
