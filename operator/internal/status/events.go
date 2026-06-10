package status

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WarningEventDeduper struct {
	mu   sync.Mutex
	last map[WarningEventKey]time.Time
}

type WarningEventKey struct {
	Namespace string
	Name      string
	Reason    string
	Message   string
}

func (d *WarningEventDeduper) ShouldRecord(key WarningEventKey, now time.Time, window time.Duration) bool {
	if window <= 0 {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.last == nil {
		d.last = map[WarningEventKey]time.Time{}
	}
	if last := d.last[key]; !last.IsZero() && now.Sub(last) < window {
		return false
	}
	d.last[key] = now
	return true
}

type EventObject interface {
	client.Object
	runtime.Object
}

func EmitNormalEvent(recorder record.EventRecorder, obj EventObject, reason, message string) {
	if recorder == nil {
		return
	}
	recorder.Event(obj, corev1.EventTypeNormal, reason, message)
}

func EmitWarningEvent(recorder record.EventRecorder, deduper *WarningEventDeduper, obj EventObject, reason, message string) {
	if recorder == nil {
		return
	}
	if deduper != nil {
		key := WarningEventKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
			Reason:    reason,
			Message:   message,
		}
		if !deduper.ShouldRecord(key, time.Now(), time.Minute) {
			return
		}
	}
	recorder.Event(obj, corev1.EventTypeWarning, reason, message)
}
