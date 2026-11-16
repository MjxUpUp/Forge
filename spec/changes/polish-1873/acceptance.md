# acceptance

```bash
go build ./...
```

```bash
go test ./internal/clitask/ -run 'TestArtifactStartHint_ChangePinned|TestRepoHasTestFiles' -count=1
```

```bash
go test ./internal/cli/ -run 'TestVersionCommand' -count=1
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```
