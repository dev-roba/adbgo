# Contributing to adbgo

Thanks for helping improve adbgo.

## Before opening a pull request

1. Open an issue for significant features or behavior changes.
2. Keep changes focused and document new menu items.
3. Add tests for pure logic, parsing, validation, and configuration behavior.
4. Never add credentials, personal device logs, or private configuration files.
5. Avoid claims that an operation succeeded unless the command and relevant result were verified.

## Local checks

Install Go and Android Platform Tools, then run:

```bash
go mod tidy
gofmt -w $(git ls-files '*.go')
go vet ./...
go test ./...
go build ./...
```

Most unit tests should not require a connected Android device. Device-dependent tests must be opt-in and clearly documented.

## Safety expectations

- Every device-specific command must identify its target serial.
- Destructive operations require an explicit confirmation that names the device and consequences.
- Timeouts and command failures must be reported honestly.
- AI-generated suggestions must never execute without user approval.
- Do not add lock-screen, FRP, or other security-bypass claims.
