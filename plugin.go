// Package main implements the NuGet plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealCommandExecutor executes actual system commands.
type RealCommandExecutor struct{}

// Run executes the command with the given arguments.
func (e *RealCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// NuGetPlugin implements the Publish packages to NuGet (.NET) plugin.
type NuGetPlugin struct {
	// cmdExecutor is used for executing shell commands. If nil, uses RealCommandExecutor.
	cmdExecutor CommandExecutor
}

// getExecutor returns the command executor, defaulting to RealCommandExecutor.
func (p *NuGetPlugin) getExecutor() CommandExecutor {
	if p.cmdExecutor != nil {
		return p.cmdExecutor
	}
	return &RealCommandExecutor{}
}

// Config represents the NuGet plugin configuration.
type Config struct {
	APIKey        string
	Source        string
	PackagePath   string
	SkipDuplicate bool
	Timeout       int
}

// DefaultSource is the default NuGet source URL.
const DefaultSource = "https://api.nuget.org/v3/index.json"

// DefaultPackagePath is the default package path pattern.
const DefaultPackagePath = "*.nupkg"

// DefaultTimeout is the default timeout in seconds.
const DefaultTimeout = 300

// GetInfo returns plugin metadata.
func (p *NuGetPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "nuget",
		Version:     "2.0.0",
		Description: "Publish packages to NuGet (.NET)",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"api_key": {"type": "string", "description": "NuGet API key (or use NUGET_API_KEY env)"},
				"source": {"type": "string", "description": "NuGet source URL", "default": "https://api.nuget.org/v3/index.json"},
				"package_path": {"type": "string", "description": "Path to package files (supports wildcards)", "default": "*.nupkg"},
				"skip_duplicate": {"type": "boolean", "description": "Skip pushing if package already exists", "default": false},
				"timeout": {"type": "integer", "description": "Push timeout in seconds", "default": 300}
			},
			"required": []
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *NuGetPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish:
		return p.pushPackage(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// pushPackage pushes NuGet packages to the configured source.
func (p *NuGetPlugin) pushPackage(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Validate configuration
	if err := p.validateConfig(cfg); err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("configuration validation failed: %v", err),
		}, nil
	}

	// Find package files
	packages, err := p.findPackages(cfg.PackagePath)
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to find packages: %v", err),
		}, nil
	}

	if len(packages) == 0 {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("no packages found matching pattern: %s", cfg.PackagePath),
		}, nil
	}

	version := strings.TrimPrefix(releaseCtx.Version, "v")

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Would push %d package(s) to NuGet", len(packages)),
			Outputs: map[string]any{
				"packages":       packages,
				"source":         cfg.Source,
				"skip_duplicate": cfg.SkipDuplicate,
				"version":        version,
			},
		}, nil
	}

	// Push each package
	pushedPackages := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if err := p.executePush(ctx, cfg, pkg); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to push package %s: %v", pkg, err),
				Outputs: map[string]any{
					"pushed_packages": pushedPackages,
					"failed_package":  pkg,
				},
			}, nil
		}
		pushedPackages = append(pushedPackages, pkg)
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully pushed %d package(s) to NuGet", len(pushedPackages)),
		Outputs: map[string]any{
			"packages": pushedPackages,
			"source":   cfg.Source,
			"version":  version,
		},
	}, nil
}

// executePush executes the dotnet nuget push command for a single package.
func (p *NuGetPlugin) executePush(ctx context.Context, cfg *Config, packagePath string) error {
	args := []string{"nuget", "push", packagePath}

	args = append(args, "--api-key", cfg.APIKey)
	args = append(args, "--source", cfg.Source)

	if cfg.SkipDuplicate {
		args = append(args, "--skip-duplicate")
	}

	args = append(args, "--timeout", fmt.Sprintf("%d", cfg.Timeout))

	executor := p.getExecutor()
	output, err := executor.Run(ctx, "dotnet", args...)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// findPackages finds package files matching the given pattern.
func (p *NuGetPlugin) findPackages(pattern string) ([]string, error) {
	// Validate the pattern for security
	if err := validatePackagePath(pattern); err != nil {
		return nil, err
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	// Filter to only include .nupkg files
	packages := make([]string, 0, len(matches))
	for _, match := range matches {
		if strings.HasSuffix(strings.ToLower(match), ".nupkg") {
			packages = append(packages, match)
		}
	}

	return packages, nil
}

// validateConfig validates the plugin configuration.
func (p *NuGetPlugin) validateConfig(cfg *Config) error {
	if cfg.APIKey == "" {
		return fmt.Errorf("API key is required (set api_key or NUGET_API_KEY environment variable)")
	}

	if err := validateSourceURL(cfg.Source); err != nil {
		return fmt.Errorf("invalid source URL: %w", err)
	}

	if err := validatePackagePath(cfg.PackagePath); err != nil {
		return fmt.Errorf("invalid package path: %w", err)
	}

	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be a positive integer")
	}

	return nil
}

// validateSourceURL validates that a URL is safe to use (SSRF protection).
func validateSourceURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("source URL cannot be empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsedURL.Hostname()

	// Allow localhost for testing purposes (HTTP is allowed only for localhost/127.0.0.1)
	isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"

	// Require HTTPS for non-localhost URLs
	if parsedURL.Scheme != "https" && !isLocalhost {
		return fmt.Errorf("only HTTPS URLs are allowed (got %s)", parsedURL.Scheme)
	}

	// For localhost, allow HTTP but skip the private IP check (it's intentionally local)
	if isLocalhost {
		return nil
	}

	// Resolve hostname to check for private IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("URLs pointing to private networks are not allowed")
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private/reserved range.
func isPrivateIP(ip net.IP) bool {
	// Private IPv4 ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16", // Link-local
		"0.0.0.0/8",
	}

	// Cloud metadata endpoints
	cloudMetadata := []string{
		"169.254.169.254/32", // AWS/GCP/Azure metadata
		"fd00:ec2::254/128",  // AWS IMDSv2 IPv6
	}

	allRanges := append(privateRanges, cloudMetadata...)

	for _, cidr := range allRanges {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if block.Contains(ip) {
			return true
		}
	}

	// Check for IPv6 private ranges
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}

	return false
}

// validatePackagePath validates a package path to prevent path traversal.
func validatePackagePath(path string) error {
	if path == "" {
		return fmt.Errorf("package path cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal detected: cannot use '..'")
	}

	// Clean the path (excluding wildcards for validation)
	cleanPath := strings.ReplaceAll(path, "*", "x") // Replace wildcards temporarily
	cleaned := filepath.Clean(cleanPath)

	// Check for path traversal in cleaned path
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return fmt.Errorf("path traversal detected: cannot escape working directory")
	}

	// Note: We allow absolute paths since they may be required in CI/CD environments
	// The path traversal check above prevents escaping to unauthorized directories

	return nil
}

// parseConfig parses the raw configuration into a Config struct.
func (p *NuGetPlugin) parseConfig(raw map[string]any) *Config {
	parser := helpers.NewConfigParser(raw)

	return &Config{
		APIKey:        parser.GetString("api_key", "NUGET_API_KEY", ""),
		Source:        parser.GetString("source", "", DefaultSource),
		PackagePath:   parser.GetString("package_path", "", DefaultPackagePath),
		SkipDuplicate: parser.GetBool("skip_duplicate", false),
		Timeout:       parser.GetInt("timeout", DefaultTimeout),
	}
}

// Validate validates the plugin configuration.
func (p *NuGetPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()
	parser := helpers.NewConfigParser(config)

	// Validate source URL if provided
	source := parser.GetString("source", "", DefaultSource)
	if source != "" {
		if err := validateSourceURL(source); err != nil {
			vb.AddError("source", err.Error())
		}
	}

	// Validate package path if provided
	packagePath := parser.GetString("package_path", "", DefaultPackagePath)
	if packagePath != "" {
		if err := validatePackagePath(packagePath); err != nil {
			vb.AddError("package_path", err.Error())
		}
	}

	// Validate timeout if provided
	timeout := parser.GetInt("timeout", DefaultTimeout)
	if timeout <= 0 {
		vb.AddError("timeout", "must be a positive integer")
	}

	// API key validation is optional at config time (can come from env var at runtime)
	// We don't add an error here since the key can be provided via NUGET_API_KEY env var

	return vb.Build(), nil
}
