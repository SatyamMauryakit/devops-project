// internal/remediation/executor.go
// Executes approved remediation actions against the Kubernetes API.
//
// Safety rules enforced here:
//  1. Allowlist: only restart_deployment, scale_deployment, rollback_deployment.
//     Any other action string is rejected before any API call is made.
//  2. Kill switch: checked before every execution via the KillSwitch interface.
//  3. Rate limit: checked before every execution via the RateLimiter interface.
//  4. State capture: the current replica count and image tag are recorded
//     before any change so an automatic rollback has the correct target state.
//  5. No cluster-admin: the ServiceAccount bound to this agent has a Role
//     limited to get/patch/update on deployments in the sentinel namespace.

package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const sentinelNamespace = "sentinel"

// AllowedActions is the exhaustive list of actions the executor will perform.
// This list is checked at execution time, not just at LLM validation time,
// so even if a bug allows an unexpected action through validation it is
// blocked here.
var AllowedActions = map[string]bool{
	"restart_deployment":  true,
	"scale_deployment":    true,
	"rollback_deployment": true,
	"no_action":           true,
}

// KillSwitch is the interface for the kill switch check.
type KillSwitch interface {
	IsEnabled(ctx context.Context) bool
}

// RateLimiter is the interface for the per-service rate limit check.
type RateLimiter interface {
	Allow(service string) error
	Record(service string)
}

// CapturedState holds the pre-action state of a deployment for rollback.
type CapturedState struct {
	DeploymentName string
	Namespace      string
	Replicas       int32
	ImageTag       string
	CapturedAt     time.Time
}

// Result describes what the executor did.
type Result struct {
	Action        string
	Applied       bool
	State         CapturedState
	RollbackPlan  string
	Error         string
}

// Executor applies approved remediation actions.
type Executor struct {
	K8s         kubernetes.Interface
	KillSwitch  KillSwitch
	RateLimiter RateLimiter
}

// Execute performs the approved action for the given deployment.
// It returns a Result describing what happened. Errors that prevent execution
// are embedded in Result.Error rather than returned so the caller always
// gets a Result to audit-log.
func (e *Executor) Execute(ctx context.Context, deploymentName, action string) Result {
	result := Result{Action: action}

	// Safety check 1: allowlist.
	if !AllowedActions[action] {
		result.Error = fmt.Sprintf("action %q is not in the allowlist", action)
		slog.Error("executor: action not allowed",
			"action", action,
			"deployment", deploymentName,
		)
		return result
	}

	if action == "no_action" {
		result.Applied = false
		result.RollbackPlan = "no_action requires no rollback"
		slog.Info("executor: no_action — nothing to do", "deployment", deploymentName)
		return result
	}

	// Safety check 2: kill switch.
	if !e.KillSwitch.IsEnabled(ctx) {
		result.Error = "remediation is disabled by the kill switch"
		slog.Warn("executor: kill switch is active — remediation blocked",
			"deployment", deploymentName,
			"action", action,
		)
		return result
	}

	// Safety check 3: rate limit.
	if err := e.RateLimiter.Allow(deploymentName); err != nil {
		result.Error = err.Error()
		slog.Warn("executor: rate limit active", "error", err.Error())
		return result
	}

	// Capture current state before making any change.
	state, err := e.captureState(ctx, deploymentName)
	if err != nil {
		result.Error = fmt.Sprintf("capture state: %s", err.Error())
		slog.Error("executor: cannot capture state",
			"deployment", deploymentName,
			"error", err.Error(),
		)
		return result
	}
	result.State = state
	result.RollbackPlan = buildRollbackPlan(deploymentName, state)

	slog.Info("executor: applying action",
		"deployment", deploymentName,
		"action", action,
		"currentReplicas", state.Replicas,
		"currentImage", state.ImageTag,
	)

	// Apply the action.
	switch action {
	case "restart_deployment":
		err = e.restartDeployment(ctx, deploymentName)
	case "scale_deployment":
		// Scale to min(currentReplicas+1, 4) to ensure at least one new pod.
		// The operator can adjust further manually or via a follow-up approval.
		target := state.Replicas + 1
		if target > 4 {
			target = 4
		}
		err = e.scaleDeployment(ctx, deploymentName, target)
	case "rollback_deployment":
		err = e.rollbackDeployment(ctx, deploymentName)
	}

	if err != nil {
		result.Error = err.Error()
		slog.Error("executor: action failed",
			"deployment", deploymentName,
			"action", action,
			"error", err.Error(),
		)
		return result
	}

	// Record the run time for rate limiting.
	e.RateLimiter.Record(deploymentName)

	result.Applied = true
	slog.Info("executor: action applied successfully",
		"deployment", deploymentName,
		"action", action,
	)
	return result
}

// ---------------------------------------------------------------------------
// State capture
// ---------------------------------------------------------------------------

func (e *Executor) captureState(ctx context.Context, deploymentName string) (CapturedState, error) {
	dep, err := e.K8s.AppsV1().Deployments(sentinelNamespace).Get(
		ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return CapturedState{}, fmt.Errorf("get deployment %q: %w", deploymentName, err)
	}

	replicas := int32(1)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}

	imageTag := ""
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		imageTag = dep.Spec.Template.Spec.Containers[0].Image
	}

	return CapturedState{
		DeploymentName: deploymentName,
		Namespace:      sentinelNamespace,
		Replicas:       replicas,
		ImageTag:       imageTag,
		CapturedAt:     time.Now().UTC(),
	}, nil
}

// ---------------------------------------------------------------------------
// Action implementations
// ---------------------------------------------------------------------------

// restartDeployment triggers a rolling restart by patching the pod template
// annotation kubectl.kubernetes.io/restartedAt. This is equivalent to
// "kubectl rollout restart deployment/<name>" and requires only PATCH on
// deployments — no cluster-admin needed.
func (e *Executor) restartDeployment(ctx context.Context, name string) error {
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339),
	)
	_, err := e.K8s.AppsV1().Deployments(sentinelNamespace).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// scaleDeployment sets spec.replicas to the given count.
func (e *Executor) scaleDeployment(ctx context.Context, name string, replicas int32) error {
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err := e.K8s.AppsV1().Deployments(sentinelNamespace).Patch(
		ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// rollbackDeployment triggers a rollback to the previous ReplicaSet using the
// rollback subresource. This is equivalent to "kubectl rollout undo".
func (e *Executor) rollbackDeployment(ctx context.Context, name string) error {
	dep, err := e.K8s.AppsV1().Deployments(sentinelNamespace).Get(
		ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment for rollback: %w", err)
	}

	// Set the rollback annotation to trigger the controller to roll back.
	// This is the same mechanism kubectl uses for "rollout undo".
	annotations := dep.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["deployment.kubernetes.io/revision"] = "0" // 0 = previous revision

	patch := `{"spec":{"rollbackTo":{"revision":0}}}`
	_, err = e.K8s.AppsV1().Deployments(sentinelNamespace).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// ---------------------------------------------------------------------------
// Rollback (called by health checker on auto-rollback)
// ---------------------------------------------------------------------------

// Rollback restores a deployment to the captured pre-action state.
func (e *Executor) Rollback(ctx context.Context, state CapturedState) error {
	slog.Warn("executor: rolling back deployment",
		"deployment", state.DeploymentName,
		"targetReplicas", state.Replicas,
		"targetImage", state.ImageTag,
	)

	patch := fmt.Sprintf(
		`{"spec":{"replicas":%d,"template":{"spec":{"containers":[{"name":%q,"image":%q}]}}}}`,
		state.Replicas,
		state.DeploymentName, // container name is assumed to match deployment name
		state.ImageTag,
	)

	_, err := e.K8s.AppsV1().Deployments(state.Namespace).Patch(
		ctx, state.DeploymentName,
		types.StrategicMergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("rollback patch: %w", err)
	}

	slog.Info("executor: rollback applied", "deployment", state.DeploymentName)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildRollbackPlan(deploymentName string, state CapturedState) string {
	return fmt.Sprintf(
		"kubectl scale deployment/%s -n %s --replicas=%d && "+
			"kubectl set image deployment/%s %s=%s -n %s",
		deploymentName, sentinelNamespace, state.Replicas,
		deploymentName, deploymentName, state.ImageTag, sentinelNamespace,
	)
}

// GetDeployment is exported so tests and the health checker can use it.
func (e *Executor) GetDeployment(ctx context.Context, name string) (*appsv1.Deployment, error) {
	return e.K8s.AppsV1().Deployments(sentinelNamespace).Get(ctx, name, metav1.GetOptions{})
}
