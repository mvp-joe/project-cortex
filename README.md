# Cortex

A powerful CLI tool built with Go and Cobra.

## Features

- Modern CLI architecture using Cobra
- Configuration management with Viper
- Shell completion support (bash, zsh, fish, powershell)
- Extensible command structure
- Global flags and command-specific options

## Installation

### From Source

```bash
git clone <repository-url>
cd project-cortex
make install
```

### Building

```bash
make build
```

The binary will be created in the `bin/` directory.

## Usage

### Basic Commands

```bash
# Show help
cortex --help

# Show version
cortex version

# Example greet command
cortex greet
cortex greet --name Alice
cortex greet Bob
```

### Global Flags

```bash
# Verbose output
cortex --verbose <command>

# Custom config file
cortex --config /path/to/config.yaml <command>
```

### Shell Completion

Generate shell completion scripts:

```bash
# Bash
cortex completion bash > /etc/bash_completion.d/cortex

# Zsh
cortex completion zsh > "${fpath[1]}/_cortex"

# Fish
cortex completion fish > ~/.config/fish/completions/cortex.fish
```

## Configuration

Cortex looks for configuration files in the following locations:
- `$HOME/.cortex.yaml`
- `./cortex.yaml` (current directory)

You can also specify a custom config file:

```bash
cortex --config /path/to/config.yaml
```

## Project Structure

```
.
├── cmd/
│   └── cortex/          # Main application entry point
│       └── main.go
├── internal/
│   ├── cli/             # CLI commands
│   │   ├── root.go      # Root command
│   │   ├── version.go   # Version command
│   │   ├── greet.go     # Example command
│   │   └── completion.go # Shell completion
│   └── config/          # Configuration handling
├── pkg/                 # Public packages (if any)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Development

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Cleaning

```bash
make clean
```

### Installing Locally

```bash
make install
```

## Adding New Commands

1. Create a new file in `internal/cli/` (e.g., `mycommand.go`)
2. Define your command using Cobra:

```go
package cli

import (
    "fmt"
    "github.com/spf13/cobra"
)

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Description of my command",
    Long:  `Longer description...`,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println("Hello from my command!")
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
    // Add flags here if needed
}
```

3. Build and test your command

## Dependencies

- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [Viper](https://github.com/spf13/viper) - Configuration management

## License

[Add your license here]

## Contributing

[Add contribution guidelines here]
