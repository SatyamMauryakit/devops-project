package remediation_test

import (
	"context"
	"testing"

	"github.com/devops-sentinel/agent/internal/remediation"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type alwaysEnabled struct{}

func (a *alwaysEnabled) IsEnabled(_ context.Context) bool { return true }

type alwaysDisabled struct{}

func (a *alwaysDisabled) IsEnabled(_ context.Context) bool { return false }

type openRateLimiter struct{}

func (o *openRateLimiter) Allow(_ string) error { return nil }
func (o *openRateLimiter) Record(_ string)      {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func fakeDeployment(name string, replicas int32, image string) *appsv1.Deployment {
	r := replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "sentinel",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &r,
			Template: appsv1.PodTemplateSpec{
				Spec: appsv1.PodSpec{
					Containers: []appsv1.Container{
						{Name: name, Image: image},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests: allowlist
// ---------------------------------------------------------------------------

func TestExecutor_AllowlistRejectsUnknownAction(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 2, "img:v1"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysEnabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "delete_everything")
	if result.Applied {
		t.Fatal("expected action to be blocked by allowlist")
	}
	if result.Error == "" {
		t.Error("expected an error message for disallowed action")
	}
}

func TestExecutor_AllowlistPermitsRestartDeployment(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 2, "img:v1"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysEnabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "restart_deployment")
	if !result.Applied {
		t.Errorf("expected restart_deployment to be applied, got error: %s", result.Error)
	}
}

func TestExecutor_AllowlistPermitsScaleDeployment(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 2, "img:v1"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysEnabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "scale_deployment")
	if !result.Applied {
		t.Errorf("expected scale_deployment to be applied, got error: %s", result.Error)
	}
}

func TestExecutor_NoActionDoesNothing(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 2, "img:v1"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysEnabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "no_action")
	if result.Applied {
		t.Error("no_action should not be marked as applied")
	}
	if result.Error != "" {
		t.Errorf("no_action should have no error, got: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Tests: kill switch
// ---------------------------------------------------------------------------

func TestExecutor_KillSwitchBlocksAction(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 2, "img:v1"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysDisabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "restart_deployment")
	if result.Applied {
		t.Fatal("kill switch should have blocked execution")
	}
	if result.Error == "" {
		t.Error("expected error message from kill switch")
	}
}

// ---------------------------------------------------------------------------
// Tests: state capture
// ---------------------------------------------------------------------------

func TestExecutor_CapturesStateBeforeAction(t *testing.T) {
	k8s := fake.NewSimpleClientset(fakeDeployment("backend", 3, "ghcr.io/org/backend:v1.2.3"))
	ex := &remediation.Executor{
		K8s:         k8s,
		KillSwitch:  &alwaysEnabled{},
		RateLimiter: &openRateLimiter{},
	}

	result := ex.Execute(context.Background(), "backend", "restart_deployment")
	if !result.Applied {
		t.Fatalf("expected action applied, got error: %s", result.Error)
	}
	if result.State.Replicas != 3 {
		t.Errorf("expected 3 replicas captured, got %d", result.State.Replicas)
	}
	if result.State.ImageTag != "ghcr.io/org/backend:v1.2.3" {
		t.Errorf("expected image tag captured, got %q", result.State.ImageTag)
	}
	if result.RollbackPlan == "" {
		t.Error("expected non-empty rollback plan")
	}
}

// ---------------------------------------------------------------------------
// Tests: AllowedActions map
// ---------------------------------------------------------------------------

func TestAllowedActionsMap(t *testing.T) {
	must := []string{"restart_deployment", "scale_deployment", "rollback_deployment", "no_action"}
	mustNot := []string{"delete_deployment", "exec_pod", "apply_manifest", ""}

	for _, a := range must {
		if !remediation.AllowedActions[a] {
			t.Errorf("expected %q to be in AllowedActions", a)
		}
	}
	for _, a := range mustNot {
		if remediation.AllowedActions[a] {
			t.Errorf("expected %q to NOT be in AllowedActions", a)
		}
	}
}
