# core-cleanup — 可执行判据

```bash
go test -v ./internal/criteria -count=1 :: ok
```

```bash
go test -v ./internal/change -run TestVerifyFailsWhenSelectorMatchesNothing -count=1 :: --- PASS
```

```bash
go test -v ./internal/cli -run TestNoDeadCommandHints -count=1 :: --- PASS
```

```bash
go test ./... -count=1 :: ok
```
