// internal/killswitch/killswitch.go
// Kill switch for all automated remediation.
//
// The kill switch is backed by a Kubernetes ConfigMap key so it can be toggled
// without restarting the agent. The agent reads the ConfigMap on every
// remediation attempt — not at startup — so changes take effect immediately.
//
// ConfigMap: sentinel/agent-config
// Key: remediation.enabled
// Value: "true" (default, remediation allowed) | "false" (all remediation blocked)
//
// To disable remediation immediately:
//   kubectl patch configmap agent-config -n sentinel \
//     --type merge -p '{"data":{"remediation.enabled":"false"}}'
//
// To re-enable:
//   kubectl patch configmap agent-config -n sentinel \
//     --type merge -p '{"data":{"remediation.enabled":"true"}}'
//
// Fallback: if the ConfigMap cannot be read, remediation is DISABLED.
// This is the safe default — fail closed rather than fail open.

package killswitch

import (
	"context"
	"log/slog"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	configMapName = "agent-config"
	configMapKey  = "remediation.enabled"
	namespace     = "sentinel"
)

// KillSwitch checks whether automated remediation is currently enabled.
type KillSwitch struct {
	k8s kubernetes.Interface
}

// New creates a KillSwitch backed by the Kubernetes API.
func New(k8s kubernetes.Interface) *KillSwitch {
	return &KillSwitch{k8s: k8s}
}

// IsEnabled returns true if remediation is allowed, false if it is blocked.
// Always returns false (safe) when the ConfigMap cannot be read.
func (ks *KillSwitch) IsEnabled(ctx context.Context) bool {
	cm, err := ks.k8s.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		slog.Warn("kill switch: cannot read ConfigMap — disabling remediation as a safe default",
			"configmap", configMapName,
			"error", err.Error(),
		)
		return false
	}

	val, ok := cm.Data[configMapKey]
	if !ok {
		slog.Warn("kill switch: key not found in ConfigMap — disabling remediation",
			"key", configMapKey,
		)
		return false
	}

	enabled := strings.TrimSpace(strings.ToLower(val)) == "true"
	if !enabled {
		slog.Warn("kill switch: remediation is DISABLED by ConfigMap",
			"configmap", configMapName,
			"key", configMapKey,
			"value", val,
		)
	}
	return enabled
}
