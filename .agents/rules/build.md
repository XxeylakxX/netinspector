# Build Guidelines

When compiling or building this project for release or production, always use the following command:

```bash
wails build -ldflags "-s -w" -clean
```

- `-ldflags "-s -w"`: Strips debug symbols and symbol tables to minimize binary size.
- `-clean`: Cleans previous build artifacts before compiling.
