# NuGet Plugin for Relicta

Official NuGet plugin for [Relicta](https://github.com/relicta-tech/relicta) - Publish packages to NuGet (.NET).

## Installation

```bash
relicta plugin install nuget
relicta plugin enable nuget
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: nuget
    enabled: true
    config:
      # Add configuration options here
```

## License

MIT License - see [LICENSE](LICENSE) for details.
