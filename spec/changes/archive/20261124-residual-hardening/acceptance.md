# acceptance — residual-hardening

```bash
go test -v ./internal/criteria -run TestSelectorEmptySignals -count=1
```

```bash
go test -v ./internal/testledger -run TestSkipClassificationEdgeCases -count=1
```

```bash
go test -v ./internal/cli -run TestNoDeadCommandHints -count=1
```

```bash
go test -v ./internal/compat -run TestSkillsContentDirs -count=1
```

```bash
go test -v ./internal/act ./internal/health ./internal/forgedata ./internal/change ./internal/cli -count=1
```

```bash
go build ./...
```
