package controller

import ctrl "sigs.k8s.io/controller-runtime"

type reconcileControl struct {
	Result ctrl.Result
	Stop   bool
}

func continueReconcile() reconcileControl {
	return reconcileControl{}
}

func stopReconcile(result ctrl.Result) reconcileControl {
	return reconcileControl{Result: result, Stop: true}
}

func stopReconcileNow() reconcileControl {
	return stopReconcile(ctrl.Result{})
}

func shouldStop(control reconcileControl, err error) bool {
	return err != nil || control.Stop
}
