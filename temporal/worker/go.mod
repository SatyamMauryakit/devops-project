module github.com/devops-sentinel/temporal-worker

go 1.22

require (
	go.temporal.io/sdk v1.27.1
	github.com/stretchr/testify v1.9.0
)

// go mod tidy will populate the indirect dependencies and generate go.sum.
// Run: cd temporal/worker && go mod tidy
