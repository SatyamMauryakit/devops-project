module github.com/devops-sentinel/agent

go 1.22

require (
	go.opentelemetry.io/otel                                          v1.28.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc  v1.28.0
	go.opentelemetry.io/otel/sdk                                      v1.28.0
	go.opentelemetry.io/otel/trace                                    v1.28.0
	go.temporal.io/sdk                                                v1.27.1
	google.golang.org/grpc                                            v1.65.0

	// Kubernetes client — used by the remediation executor.
	// client-go major version must match the cluster's Kubernetes version.
	// EKS 1.30 / kind 1.30 -> use client-go v0.30.x
	k8s.io/client-go  v0.30.3
	k8s.io/api         v0.30.3
	k8s.io/apimachinery v0.30.3

	// MongoDB driver — used by the audit log.
	go.mongodb.org/mongo-driver v1.16.0

	// testify — test assertions
	github.com/stretchr/testify v1.9.0
)

// Run: go mod tidy  to populate indirect dependencies and generate go.sum.
