build tag='local':
	docker build -t ghcr.io/cofide/spiffe-enable:{{tag}} . 

test *args:
	go run gotest.tools/gotestsum@latest --format github-actions ./... -short {{args}}

test-race: (test "--" "-race")

lint *args:
	golangci-lint run --show-stats {{args}}
