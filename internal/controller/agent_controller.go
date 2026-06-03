/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentmeshv1 "github.com/neontvn/agent-mesh/api/v1"
	"github.com/neontvn/agent-mesh/internal/web"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Bus is the event bus for fanning mesh events out to UI subscribers.
	// Optional; if nil, events are not published.
	Bus *web.EventBus
}

// +kubebuilder:rbac:groups=agentmesh.agentmesh.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agentmesh.agentmesh.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agentmesh.agentmesh.io,resources=agents/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Agent object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
// leaseTTL is how long an agent may go without a Heartbeat before being
// marked unhealthy. Hardcoded for v0 to match the LeaseTtlSeconds value
// returned by the control-plane Register handler.
const leaseTTL = 30 * time.Second

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var agent agentmeshv1.Agent
	if err := r.Get(ctx, req.NamespacedName, &agent); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Agent deleted", "name", req.Name)
			if r.Bus != nil {
				r.Bus.Publish(web.EventAgentUnregistered, map[string]interface{}{
					"agent_id": req.Name,
				})
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling Agent",
		"name", agent.Name,
		"capabilities", agent.Spec.Capabilities,
		"endpoint", agent.Spec.Endpoint,
	)

	// Lease eviction: if no Heartbeat has landed within leaseTTL, mark
	// the agent unhealthy. Fresh agents (no heartbeat yet) get a grace
	// window measured from their creation timestamp.
	effectiveTime := agent.CreationTimestamp.Time
	if agent.Status.LastHeartbeat != nil {
		effectiveTime = agent.Status.LastHeartbeat.Time
	}

	if time.Since(effectiveTime) > leaseTTL && agent.Status.Health != "unhealthy" {
		logger.Info("Lease expired, marking unhealthy",
			"name", agent.Name,
			"last_seen", effectiveTime,
		)
		oldHealth := agent.Status.Health
		agent.Status.Health = "unhealthy"
		if err := r.Status().Update(ctx, &agent); err != nil {
			return ctrl.Result{}, err
		}
		if r.Bus != nil {
			r.Bus.Publish(web.EventAgentHealthChanged, map[string]interface{}{
				"agent_id":   agent.Name,
				"old_health": oldHealth,
				"health":     "unhealthy",
			})
		}
	}

	// Requeue periodically so lease expiry is detected even without
	// other changes triggering reconciliation.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentmeshv1.Agent{}).
		Named("agent").
		Complete(r)
}
