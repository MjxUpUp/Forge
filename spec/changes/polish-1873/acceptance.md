# acceptance

```bash
go build ./...
```

```bash
go test ./internal/clitask/ -run 'TestArtifactStartHint|TestRepoHasGoTestFiles|TestRenderTaskReport_NoTestFilesHonest|TestBuildTaskReport_RepoHasGoTestFilesWiring' -count=1
```

```bash
go test ./internal/cli/ -run 'TestVersionCommand' -count=1
```

```bash
go test ./internal/cli/ -count=1
```

```bash
sh scripts/checks/gofmt-clean.sh
```

```bash
sh scripts/checks/deadcode-zero.sh
```

```bash
go run ./cmd/forge docs lint docs/reference/cli.md
```
