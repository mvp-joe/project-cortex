# Language Support

ProjectCortex uses tree-sitter to parse and extract information from multiple programming languages. This document details what gets extracted from each supported language.

## Supported Languages

- [Go](#go)
- [TypeScript / TSX](#typescript)
- [JavaScript / JSX](#javascript)
- [Python](#python)
- [Rust](#rust)
- [C / C++](#c--c)
- [PHP](#php)
- [Ruby](#ruby)
- [Java](#java)

## Extraction Tiers

For all languages, ProjectCortex extracts code at three levels:

### 1. Symbols (High-Level Overview)
- Package/module names
- Import/include statements
- Type/class/struct names with line numbers
- Function/method names with line numbers
- Top-level constants

### 2. Definitions (Full Signatures)
- Complete type definitions (structs, classes, interfaces)
- Function signatures (without implementations)
- Method signatures
- Enum definitions

### 3. Data (Constants & Values)
- Constant declarations
- Global variables with initializers
- Enum values
- Configuration values

---

## Go

### File Extensions
`.go`

### Symbols Extracted

```go
// Example file: internal/server/handler.go
package server

import (
    "net/http"
    "encoding/json"
)

type Handler struct {
    router *http.ServeMux
}

func NewHandler() *Handler { ... }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { ... }
```

**Symbols Output:**
```json
{
  "file": "internal/server/handler.go",
  "package": "server",
  "imports": ["net/http", "encoding/json"],
  "types": [
    {"name": "Handler", "kind": "struct", "line": 8}
  ],
  "functions": [
    {"name": "NewHandler", "line": 12},
    {"name": "ServeHTTP", "receiver": "Handler", "line": 13}
  ]
}
```

### Definitions Extracted

- Struct definitions
- Interface definitions
- Function signatures
- Method signatures
- Type aliases

### Data Extracted

- `const` declarations (single and grouped)
- `var` declarations with initializers
- iota-based enums

### Configuration

```yaml
languages:
  go:
    extract_comments: true
    extract_tests: false
    include_vendor: false
    generated_patterns:
      - "**/*.pb.go"
      - "**/*_gen.go"
```

---

## TypeScript

### File Extensions
`.ts`, `.tsx` (TSX includes JSX syntax support)

### Symbols Extracted

```typescript
// Example: src/components/Button.tsx
import React from 'react';
import { Theme } from './theme';

export interface ButtonProps {
  label: string;
  onClick: () => void;
}

export class Button extends React.Component<ButtonProps> { ... }
export function createButton(props: ButtonProps): JSX.Element { ... }
```

**Note**: `.tsx` files with JSX/React syntax are fully supported by tree-sitter-typescript.

**Symbols Output:**
```json
{
  "file": "src/components/Button.tsx",
  "imports": [
    {"name": "React", "from": "react"},
    {"name": "Theme", "from": "./theme"}
  ],
  "types": [
    {"name": "ButtonProps", "kind": "interface", "line": 4}
  ],
  "classes": [
    {"name": "Button", "line": 9}
  ],
  "functions": [
    {"name": "createButton", "line": 10}
  ]
}
```

### Definitions Extracted

- Interface definitions
- Type aliases
- Class definitions
- Function signatures with type annotations
- Enum definitions
- Generic type parameters

### Data Extracted

- `const` declarations with values
- `enum` values
- Default export values

### Configuration

```yaml
languages:
  typescript:
    include_types: true
    include_jsx: true
    extract_jsdoc: true
    include_declarations: true  # .d.ts files
```

---

## JavaScript

### File Extensions
`.js`, `.jsx` (JSX includes JSX syntax support), `.mjs`

### Symbols Extracted

Similar to TypeScript but without type information.

**Note**: `.jsx` files with JSX/React syntax are fully supported by tree-sitter-javascript.

```javascript
// Example: src/utils/formatter.js
import { parseDate } from './date';

export class Formatter { ... }
export function format(value) { ... }
export const DEFAULT_LOCALE = 'en-US';
```

**Symbols Output:**
```json
{
  "file": "src/utils/formatter.js",
  "imports": [
    {"name": "parseDate", "from": "./date"}
  ],
  "classes": [
    {"name": "Formatter", "line": 3}
  ],
  "functions": [
    {"name": "format", "line": 4}
  ],
  "constants": [
    {"name": "DEFAULT_LOCALE", "line": 5}
  ]
}
```

### Definitions Extracted

- Class definitions
- Function declarations
- Arrow function assignments
- Object/array destructuring

### Data Extracted

- `const` with primitive values
- Module exports
- Default exports

### Configuration

```yaml
languages:
  javascript:
    include_jsx: true
    extract_jsdoc: true
    include_commonjs: true  # require() imports
```

---

## Python

### File Extensions
`.py`

### Symbols Extracted

```python
# Example: src/models/user.py
from dataclasses import dataclass
from typing import Optional

@dataclass
class User:
    id: int
    name: str
    email: Optional[str] = None

def create_user(name: str) -> User:
    ...

DEFAULT_TIMEOUT = 30
```

**Symbols Output:**
```json
{
  "file": "src/models/user.py",
  "imports": [
    {"name": "dataclass", "from": "dataclasses"},
    {"name": "Optional", "from": "typing"}
  ],
  "classes": [
    {"name": "User", "decorators": ["dataclass"], "line": 5}
  ],
  "functions": [
    {"name": "create_user", "line": 10}
  ],
  "constants": [
    {"name": "DEFAULT_TIMEOUT", "line": 13}
  ]
}
```

### Definitions Extracted

- Class definitions with decorators
- Function signatures with type hints
- Method signatures
- Dataclass definitions
- Protocol/ABC definitions

### Data Extracted

- Module-level constants (UPPERCASE)
- Dataclass defaults
- Enum members
- Type aliases

### Configuration

```yaml
languages:
  python:
    extract_docstrings: true
    extract_type_hints: true
    include_init: true  # __init__.py files
    include_notebooks: false  # .ipynb files
```

---

## Rust

### File Extensions
`.rs`

### Symbols Extracted

```rust
// Example: src/server/handler.rs
use std::net::TcpListener;
use serde::{Serialize, Deserialize};

pub struct Handler {
    listener: TcpListener,
}

pub fn new_handler(addr: &str) -> Result<Handler, Error> { ... }

impl Handler {
    pub fn serve(&self) { ... }
}
```

**Symbols Output:**
```json
{
  "file": "src/server/handler.rs",
  "imports": [
    {"name": "TcpListener", "from": "std::net"},
    {"names": ["Serialize", "Deserialize"], "from": "serde"}
  ],
  "structs": [
    {"name": "Handler", "visibility": "pub", "line": 4}
  ],
  "functions": [
    {"name": "new_handler", "visibility": "pub", "line": 8}
  ],
  "impls": [
    {"type": "Handler", "methods": ["serve"], "line": 10}
  ]
}
```

### Definitions Extracted

- Struct definitions
- Enum definitions
- Trait definitions
- Function signatures
- Impl blocks
- Type aliases

### Data Extracted

- `const` declarations
- `static` declarations
- Enum variants with values

### Configuration

```yaml
languages:
  rust:
    extract_comments: true
    include_tests: false
    extract_macros: true
```

---

## C / C++

### File Extensions
`.c`, `.cpp`, `.cc`, `.h`, `.hpp`

### Symbols Extracted

```c
// Example: src/handler.h
#include <stdio.h>
#include "types.h"

typedef struct {
    int port;
    char* host;
} Config;

int init_server(Config* config);
void handle_request(Request* req);

#define DEFAULT_PORT 8080
```

**Symbols Output:**
```json
{
  "file": "src/handler.h",
  "includes": ["<stdio.h>", "\"types.h\""],
  "types": [
    {"name": "Config", "kind": "struct", "line": 4}
  ],
  "functions": [
    {"name": "init_server", "line": 9},
    {"name": "handle_request", "line": 10}
  ],
  "macros": [
    {"name": "DEFAULT_PORT", "line": 12}
  ]
}
```

### Definitions Extracted

- Struct/union definitions
- Function declarations
- Class definitions (C++)
- Template definitions (C++)
- Enum definitions

### Data Extracted

- `#define` constants
- `const` variables
- Enum values
- Global variables with initializers

### Configuration

```yaml
languages:
  c:
    extract_macros: true
    include_headers: true

  cpp:
    extract_templates: true
    extract_namespaces: true
```

---

## PHP

### File Extensions
`.php`

### Symbols Extracted

```php
<?php
// Example: src/Handler.php
namespace App\Server;

use Psr\Http\Message\RequestInterface;

class Handler {
    private $router;

    public function __construct(Router $router) { ... }
    public function handle(RequestInterface $request) { ... }
}

const DEFAULT_TIMEOUT = 30;
```

**Symbols Output:**
```json
{
  "file": "src/Handler.php",
  "namespace": "App\\Server",
  "imports": [
    {"name": "RequestInterface", "from": "Psr\\Http\\Message"}
  ],
  "classes": [
    {"name": "Handler", "line": 6}
  ],
  "methods": [
    {"class": "Handler", "name": "__construct", "visibility": "public", "line": 9},
    {"class": "Handler", "name": "handle", "visibility": "public", "line": 10}
  ],
  "constants": [
    {"name": "DEFAULT_TIMEOUT", "line": 13}
  ]
}
```

### Definitions Extracted

- Class definitions
- Interface definitions
- Trait definitions
- Function signatures
- Method signatures

### Data Extracted

- `const` declarations
- Class constants
- Global constants

### Configuration

```yaml
languages:
  php:
    extract_docblocks: true
    include_traits: true
```

---

## Ruby

### File Extensions
`.rb`

### Symbols Extracted

```ruby
# Example: lib/handler.rb
require 'net/http'
require_relative 'config'

module Server
  class Handler
    attr_reader :router

    def initialize(router)
      @router = router
    end

    def handle(request)
      # ...
    end
  end
end

DEFAULT_PORT = 8080
```

**Symbols Output:**
```json
{
  "file": "lib/handler.rb",
  "requires": ["net/http", "./config"],
  "modules": [
    {"name": "Server", "line": 4}
  ],
  "classes": [
    {"name": "Handler", "namespace": "Server", "line": 5}
  ],
  "methods": [
    {"class": "Handler", "name": "initialize", "line": 8},
    {"class": "Handler", "name": "handle", "line": 12}
  ],
  "constants": [
    {"name": "DEFAULT_PORT", "line": 18}
  ]
}
```

### Definitions Extracted

- Class definitions
- Module definitions
- Method signatures
- Attr declarations

### Data Extracted

- Constants (UPPERCASE)
- Class variables
- Module constants

### Configuration

```yaml
languages:
  ruby:
    extract_comments: true
    include_modules: true
```

---

## Java

### File Extensions
`.java`

### Symbols Extracted

```java
// Example: src/main/java/com/example/Handler.java
package com.example;

import java.net.HttpURLConnection;
import java.util.List;

public class Handler {
    private final Router router;

    public Handler(Router router) { ... }
    public void handle(Request request) { ... }
}

public interface RequestHandler {
    void handle(Request req);
}
```

**Symbols Output:**
```json
{
  "file": "src/main/java/com/example/Handler.java",
  "package": "com.example",
  "imports": [
    "java.net.HttpURLConnection",
    "java.util.List"
  ],
  "classes": [
    {"name": "Handler", "modifiers": ["public"], "line": 6}
  ],
  "interfaces": [
    {"name": "RequestHandler", "modifiers": ["public"], "line": 13}
  ],
  "methods": [
    {"class": "Handler", "name": "Handler", "kind": "constructor", "line": 9},
    {"class": "Handler", "name": "handle", "modifiers": ["public"], "line": 10},
    {"interface": "RequestHandler", "name": "handle", "line": 14}
  ]
}
```

### Definitions Extracted

- Class definitions
- Interface definitions
- Enum definitions
- Method signatures
- Generic type parameters

### Data Extracted

- `static final` constants
- Enum values
- Static initializers

### Configuration

```yaml
languages:
  java:
    extract_javadoc: true
    include_annotations: true
```

---

## Adding Custom Patterns

You can customize extraction patterns per language:

```yaml
languages:
  go:
    custom_queries:
      - name: "error_types"
        query: |
          (type_declaration
            (type_spec
              name: (type_identifier) @name
              type: (struct_type)
            )
          ) @error_type
          (#match? @name ".*Error$")
```

This extracts all structs ending in "Error".

## Language-Specific Ignore Patterns

```yaml
indexing:
  ignore_patterns:
    # Go
    - "**/*_test.go"
    - "**/*.pb.go"

    # JavaScript/TypeScript
    - "**/*.min.js"
    - "**/*.d.ts"
    - "node_modules/**"

    # Python
    - "**/__pycache__/**"
    - "**/*.pyc"

    # Rust
    - "target/**"

    # Java
    - "target/**"
    - "**/*.class"
```

## Testing Language Support

Test extraction for a specific file:

```bash
# Extract symbols
cortex extract symbols path/to/file.go

# Extract definitions
cortex extract definitions path/to/file.go

# Extract data
cortex extract data path/to/file.go

# View all tiers
cortex extract all path/to/file.go
```

## Contributing Language Support

See [Contributing Guide](contributing.md#adding-language-support) for how to add support for new languages.

## Related Documentation

- [Architecture](architecture.md)
- [Configuration](configuration.md)
- [Contributing](contributing.md)
