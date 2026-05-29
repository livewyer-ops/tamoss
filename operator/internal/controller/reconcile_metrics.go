package controller

import (
	"time"

	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	ctrl "sigs.k8s.io/controller-runtime"
)

func recordControllerReconcile(controller string, result ctrl.Result, err error, duration time.Duration) {
	if err != nil {
		operatormetrics.RecordReconcileError(controller)
		operatormetrics.RecordReconcile(controller, "error", duration)
		return
	}
	if result.Requeue || result.RequeueAfter > 0 {
		operatormetrics.RecordReconcile(controller, "requeue", duration)
		return
	}
	operatormetrics.RecordReconcile(controller, "success", duration)
}
