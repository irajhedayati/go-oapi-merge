# go-oapi-merge

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/irajhedayati/go-oapi-merge)](https://goreportcard.com/report/github.com/irajhedayati/go-oapi-merge)
[![Go Reference](https://pkg.go.dev/badge/github.com/irajhedayati/go-oapi-merge.svg)](https://pkg.go.dev/github.com/irajhedayati/go-oapi-merge)

> **Maintained fork notice**: This is a friendly fork of [NoL1m1ts/go-oapi-merge](https://github.com/NoL1m1ts/go-oapi-merge), continued here after the original repository became inactive. All credit for the original design and implementation goes to [NoL1m1ts](https://github.com/NoL1m1ts). This fork exists to keep the project maintained, review contributions, and add improvements. Contributions previously proposed upstream are welcome here.

`go-oapi-merge` is a CLI tool for merging OpenAPI YAML files. It resolves `$ref` references across multiple files and combines them into a single, unified OpenAPI specification. Perfect for managing large or modular OpenAPI projects.

---

## Features

- **Resolves `$ref` References**: Automatically resolves and merges external references in OpenAPI files.
- **Simple CLI Interface**: Easy-to-use command-line tool for quick integration into your workflow.
- **Customizable Input/Output**: Specify input and output file paths for flexible usage.
- **Cross-Platform**: Built in Go, it works seamlessly on Windows, macOS, and Linux.

---

## Installation

Install `go-oapi-merge` using `go install`:

```bash
go install github.com/irajhedayati/go-oapi-merge@latest
```

---

## Usage

### CLI Command

```bash
go-oapi-merge -input <input_file> -output <output_file>
```

#### Options

| Flag      | Description                              | Required | Default Value   |
|-----------|------------------------------------------|----------|-----------------|
| `-input`  | Path to the main OpenAPI file.           | No       | `api.yaml`      |
| `-output` | Path to save the merged OpenAPI file.    | No       | `merged_api.yaml` |

#### Examples

```bash
# Basic usage with default values
go-oapi-merge

# Specify custom input and output files
go-oapi-merge -input specs/main.yaml -output dist/merged.yaml

# Using relative paths
go-oapi-merge -input ./api/openapi.yaml -output ./dist/final.yaml
```

#### Error Handling

The tool will display error messages in the following cases:
- Input file doesn't exist or is not accessible
- Input file is not a valid YAML
- Output directory doesn't exist or is not writable
- Referenced files (`$ref`) are missing or invalid
- Invalid OpenAPI specification format

Example error output:
```bash
Error: open api.yaml: no such file or directory
```

---

## How It Works

1. **Reads the Main File**: The tool starts by reading the main OpenAPI file specified with the `-input` flag.
2. **Resolves References**: It identifies `$ref` references, reads the linked files, and merges their content into the main file.
3. **Saves the Result**: The final merged OpenAPI specification is saved to the file specified with the `-output` flag.

---

## Example Project Structure

Here’s an example of a modular OpenAPI project:

```
├── api.yaml
├── paths
│   ├── user.yaml
│   └── pet.yaml
└── components
    └── schemas.yaml
```

The `api.yaml` file contains references to other files:

```yaml
paths:
  /user:
    $ref: "./paths/user.yaml#/paths/~1user"
  /pet:
    $ref: "./paths/pet.yaml#/paths/~1pet"
```

After running `go-oapi-merge`, the tool will merge all referenced files into a single `merged_api.yaml`.

---

## Example Project Structure with Example Files

The repository includes example files in the `examples` directory that demonstrate the recommended structure and usage:

```
examples/
├── api.yaml                 # Main OpenAPI file
├── paths/
│   ├── users.yaml          # User endpoints
│   └── pets.yaml           # Pet endpoints
└── components/
    └── schemas.yaml        # Shared schemas
```

### Main File (api.yaml)

```plain
openapi: 3.0.0
info:
  title: Example API
  version: 1.0.0
paths:
  /users:
    $ref: './paths/users.yaml#/paths/users'
  /pets:
    $ref: './paths/pets.yaml#/paths/pets'
components:
  schemas:
    $ref: './common/schemas.yaml#/common/schemas'
```

You can use these examples as a starting point:
```bash
# Copy examples to your project
cp -r examples/* your-project/

# Run merge on example files
go-oapi-merge -input examples/api.yaml -output merged_api.yaml
```

---

## Development

### Building from Source

1. Clone the repository:

   ```bash
   git clone https://github.com/irajhedayati/go-oapi-merge.git
   cd go-oapi-merge
   ```

2. Build the project:

   ```bash
   go build -o go-oapi-merge
   ```

3. Run the tool locally:

   ```bash
   ./go-oapi-merge -input api.yaml -output merged_api.yaml
   ```

### Contributing

We welcome contributions! Here’s how you can help:

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Submit a pull request with a detailed description of your changes.

Please ensure your code follows the project’s coding standards and includes appropriate tests.

### Linting and code quality

The project uses [golangci-lint](https://golangci-lint.run) with the configuration in `.golangci.yml`. A `Makefile` wraps the common workflows:

```bash
make install-tools   # installs the pinned golangci-lint into $GOPATH/bin
make lint            # run the linter
make lint-fix        # run the linter and auto-fix what it can
make test            # go test -race -count=1 ./...
make check           # vet + lint + test (what CI runs)
```

The `lint` target auto-installs the pinned linter version the first time you run it, so `make check` works on a fresh clone. Both local runs and CI use the same pinned version — bump it in the `Makefile` and `.github/workflows/go.yml` together.

---

### License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

The original copyright is held by [NoL1m1ts](https://github.com/NoL1m1ts) (2023). Modifications and continued maintenance are copyright Iraj Hedayati (2026-present). Both notices are preserved in the `LICENSE` file as required by the MIT License.

---

### Authors and Credits

- **Original author**: [NoL1m1ts](https://github.com/NoL1m1ts) — designed and built the original [go-oapi-merge](https://github.com/NoL1m1ts/go-oapi-merge).
- **Current maintainer**: [Iraj Hedayati](https://github.com/irajhedayati) — maintains this fork after the upstream project became inactive.

---

### Support

If you encounter any issues or have questions, please [open an issue](https://github.com/irajhedayati/go-oapi-merge/issues) on GitHub.

---

**Happy merging!** 🚀
