test *args:
	go run gotest.tools/gotestsum@latest --format github-actions ./... -short {{args}}

test-race: (test "--" "-race")

lint *args:
	golangci-lint run --show-stats {{args}}
