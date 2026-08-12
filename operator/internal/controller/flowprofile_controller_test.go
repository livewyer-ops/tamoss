package controller

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
)

func TestResolveFlowProfileBuildsCanonicalTAMSDocument(t *testing.T) {
	profile := flowProfileFixture()
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		t.Fatalf("resolve FlowProfile: %v", err)
	}
	if resolved.Spec.ID == "" || resolved.Format != "urn:x-nmos:format:video" || resolved.Codec != "video/h264" {
		t.Fatalf("unexpected resolved Profile: %#v", resolved)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(resolved.Document), &document); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if document["id"] != resolved.Spec.ID || document["label"] != "HD AVC" {
		t.Fatalf("unexpected Profile document: %#v", document)
	}
	tags := document["tags"].(map[string]any)
	if tags["tier"] != "mezzanine" {
		t.Fatalf("scalar tag was not preserved: %#v", tags)
	}
}

func TestFlowProfileJobHashTracksRuntimeAndDatabaseInputs(t *testing.T) {
	profile := flowProfileFixture()
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	tamoss := flowProfileTamossFixture()
	original := flowProfileRegistrationJob(profile, tamoss, resolved)
	if flowProfileJobDrifted(original, flowProfileRegistrationJob(profile, tamoss, resolved)) {
		t.Fatal("identical registration input was reported as drift")
	}

	changed := tamoss.DeepCopy()
	changed.Spec.API.Image.Tag = "8.2.0-oss1-replacement"
	if !flowProfileJobDrifted(original, flowProfileRegistrationJob(profile, changed, resolved)) {
		t.Fatal("API runtime image change did not invalidate the registration Job")
	}
	changed = tamoss.DeepCopy()
	changed.Spec.Backends.DB.External = &tamossv1alpha1.DBExternalSpec{Host: "replacement-db.example.test"}
	if !flowProfileJobDrifted(original, flowProfileRegistrationJob(profile, changed, resolved)) {
		t.Fatal("database connection change did not invalidate the registration Job")
	}
}

func TestFlowProfileReconcileLaunchesRegistrationAndPublishesReadyState(t *testing.T) {
	ctx := context.Background()
	scheme := flowProfileTestScheme(t)
	profile := flowProfileFixture()
	tamoss := flowProfileTamossFixture()
	schemaState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("schema-state"), Namespace: tamoss.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion}}
	c := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.FlowProfile{}, &batchv1.Job{}).
		WithIndex(&tamossv1alpha1.FlowProfile{}, flowProfileTamossIndex, func(obj client.Object) []string {
			return []string{obj.(*tamossv1alpha1.FlowProfile).Spec.TamossRef.Name}
		}).WithObjects(profile, tamoss, schemaState).Build()
	r := &FlowProfileReconciler{Client: c, Scheme: scheme, WatchNamespaces: WatchNamespaceSet{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(profile)}

	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("add finalizer: %v", err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("launch registration: %v", err)
	}
	job := &batchv1.Job{}
	jobKey := types.NamespacedName{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, "register")}
	if err := c.Get(ctx, jobKey, job); err != nil {
		t.Fatalf("get registration Job: %v", err)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "livewyer/tamoss-api:8.2.0-oss1" || !strings.Contains(strings.Join(container.Args, " "), "tamoss-profile ensure") {
		t.Fatalf("unexpected registration container: %#v", container)
	}
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken ||
		job.Spec.Template.Spec.SecurityContext == nil || job.Spec.Template.Spec.SecurityContext.RunAsNonRoot == nil || !*job.Spec.Template.Spec.SecurityContext.RunAsNonRoot ||
		container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("registration Job is missing its restricted security context: %#v", job.Spec.Template.Spec)
	}
	document := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, "document")}, document); err != nil {
		t.Fatalf("get Profile document: %v", err)
	}
	if !strings.Contains(document.Data["profile.json"], `"flow_metadata"`) {
		t.Fatalf("registration document missing TAMS metadata: %s", document.Data["profile.json"])
	}

	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatalf("update completed Job: %v", err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("observe registration completion: %v", err)
	}
	liveProfile := &tamossv1alpha1.FlowProfile{}
	if err := c.Get(ctx, request.NamespacedName, liveProfile); err != nil {
		t.Fatal(err)
	}
	if !flowProfileReady(liveProfile) || liveProfile.Status.ProfileID == "" {
		t.Fatalf("FlowProfile did not publish Ready identity: %#v", liveProfile.Status)
	}
	state := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, "state")}, state); err != nil {
		t.Fatalf("get registration state: %v", err)
	}
	if state.Data[flowProfileStateID] != liveProfile.Status.ProfileID {
		t.Fatalf("state/profile identity mismatch: %#v", state.Data)
	}
}

func TestResolveIngestFlowProfilesSupportsReferenceAndUUID(t *testing.T) {
	ctx := context.Background()
	scheme := flowProfileTestScheme(t)
	profile := flowProfileFixture()
	profile.Generation = 1
	profile.Status = tamossv1alpha1.FlowProfileStatus{
		ObservedGeneration: 1,
		ProfileID:          "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28",
		Resolved:           tamossv1alpha1.FlowProfileResolvedStatus{Format: "urn:x-nmos:format:video"},
		Conditions:         []metav1.Condition{{Type: flowProfileConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now(), ObservedGeneration: 1}},
	}
	r := &IngestRunReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(profile).Build()}
	run := &tamossv1alpha1.IngestRun{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: profile.Namespace}}
	spec := tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"}, Options: tamossv1alpha1.IngestRunOptions{TAMSFlowProfiles: []tamossv1alpha1.IngestRunTAMSFlowProfile{
		{Format: "video", ProfileRef: &tamossv1alpha1.IngestFlowProfileReference{Name: profile.Name}},
		{Format: "audio", ProfileID: "73b13cf7-719a-448d-9852-7c4d5e1bb522"},
	}}}
	resolved, reason, _, err := r.resolveIngestFlowProfiles(ctx, run, spec)
	if err != nil || reason != "" {
		t.Fatalf("resolve assignments: reason=%q err=%v", reason, err)
	}
	if resolved[0].ProfileRef != profile.Name || resolved[0].ProfileID != profile.Status.ProfileID {
		t.Fatalf("reference was not resolved: %#v", resolved[0])
	}
	if resolved[1].ProfileRef != "" || resolved[1].ProfileID != spec.Options.TAMSFlowProfiles[1].ProfileID {
		t.Fatalf("raw UUID assignment changed: %#v", resolved[1])
	}
}

func TestResolvedIngestFlowProfilesArePersistedBeforeJobCreation(t *testing.T) {
	ctx := context.Background()
	scheme := flowProfileTestScheme(t)
	run := &tamossv1alpha1.IngestRun{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: "media"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&tamossv1alpha1.IngestRun{}).WithObjects(run).Build()
	r := &IngestRunReconciler{Client: c}
	resolved := []tamossv1alpha1.IngestRunResolvedFlowProfileStatus{{
		Format: "video", Index: 0, ProfileID: "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28", ProfileRef: "hd-avc",
	}}
	changed, err := r.persistResolvedIngestFlowProfiles(ctx, run, resolved)
	if err != nil || !changed {
		t.Fatalf("persist resolved Profiles: changed=%t err=%v", changed, err)
	}
	live := &tamossv1alpha1.IngestRun{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(run), live); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(live.Status.ResolvedTAMSFlowProfiles, resolved) {
		t.Fatalf("resolved Profile status was not persisted: %#v", live.Status.ResolvedTAMSFlowProfiles)
	}
	changed, err = r.persistResolvedIngestFlowProfiles(ctx, live, resolved)
	if err != nil || changed {
		t.Fatalf("identical status was not idempotent: changed=%t err=%v", changed, err)
	}
}

func TestResolveIngestFlowProfilesRejectsUnusableReferences(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*tamossv1alpha1.FlowProfile, *tamossv1alpha1.IngestRunSpec)
		wantReason string
	}{
		{name: "missing", mutate: func(profile *tamossv1alpha1.FlowProfile, _ *tamossv1alpha1.IngestRunSpec) { profile.Name = "different" }, wantReason: "IngestFlowProfileNotFound"},
		{name: "target mismatch", mutate: func(profile *tamossv1alpha1.FlowProfile, _ *tamossv1alpha1.IngestRunSpec) {
			profile.Spec.TamossRef.Name = "other"
		}, wantReason: "IngestFlowProfileTargetMismatch"},
		{name: "not ready", mutate: func(profile *tamossv1alpha1.FlowProfile, _ *tamossv1alpha1.IngestRunSpec) {
			profile.Status.Conditions = nil
		}, wantReason: "IngestFlowProfileNotReady"},
		{name: "format mismatch", mutate: func(_ *tamossv1alpha1.FlowProfile, spec *tamossv1alpha1.IngestRunSpec) {
			spec.Options.TAMSFlowProfiles[0].Format = "audio"
		}, wantReason: "IngestFlowProfileFormatMismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := readyFlowProfileFixture()
			run := &tamossv1alpha1.IngestRun{ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: profile.Namespace}}
			spec := tamossv1alpha1.IngestRunSpec{
				TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
				Options: tamossv1alpha1.IngestRunOptions{TAMSFlowProfiles: []tamossv1alpha1.IngestRunTAMSFlowProfile{{
					Format: "video", ProfileRef: &tamossv1alpha1.IngestFlowProfileReference{Name: "hd-avc"},
				}}},
			}
			tt.mutate(profile, &spec)
			r := &IngestRunReconciler{Client: fake.NewClientBuilder().WithScheme(flowProfileTestScheme(t)).WithObjects(profile).Build()}
			_, reason, _, err := r.resolveIngestFlowProfiles(context.Background(), run, spec)
			if err != nil || reason != tt.wantReason {
				t.Fatalf("reason=%q err=%v, want %q", reason, err, tt.wantReason)
			}
		})
	}
}

func TestFlowProfileDeletionRemainsBlockedWhileAFlowUsesTheProfile(t *testing.T) {
	ctx := context.Background()
	scheme := flowProfileTestScheme(t)
	profile := flowProfileFixture()
	profile.Finalizers = []string{flowProfileFinalizer}
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	tamoss := flowProfileTamossFixture()
	schemaState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("schema-state"), Namespace: tamoss.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion}}
	registrationState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: flowProfileResourceName(profile, "state"), Namespace: profile.Namespace}, Data: map[string]string{
		flowProfileStateReady: "true", flowProfileStateID: resolved.Spec.ID, flowProfileStateHash: resolved.Hash,
	}}
	c := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.FlowProfile{}, &batchv1.Job{}).
		WithIndex(&tamossv1alpha1.FlowProfile{}, flowProfileTamossIndex, func(obj client.Object) []string {
			return []string{obj.(*tamossv1alpha1.FlowProfile).Spec.TamossRef.Name}
		}).
		WithObjects(profile, tamoss, schemaState, registrationState).Build()
	r := &FlowProfileReconciler{Client: c, Scheme: scheme, WatchNamespaces: WatchNamespaceSet{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(profile)}

	if err := c.Delete(ctx, profile); err != nil {
		t.Fatalf("mark FlowProfile for deletion: %v", err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("launch deletion: %v", err)
	}
	job := &batchv1.Job{}
	jobKey := types.NamespacedName{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, "delete")}
	if err := c.Get(ctx, jobKey, job); err != nil {
		t.Fatalf("get deletion Job: %v", err)
	}
	job.UID = types.UID("profile-deletion-job")
	if err := c.Update(ctx, job); err != nil {
		t.Fatalf("set deletion Job UID: %v", err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatalf("fail deletion Job: %v", err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "profile-deletion-pod", Namespace: profile.Namespace, Labels: map[string]string{"job-name": job.Name},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true)}},
	}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
		Name: "flow-profile-delete", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: flowProfileExitInUse}},
	}}}}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("create deletion Pod status: %v", err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("observe blocked deletion: %v", err)
	}
	liveProfile := &tamossv1alpha1.FlowProfile{}
	if err := c.Get(ctx, request.NamespacedName, liveProfile); err != nil {
		t.Fatalf("get terminating FlowProfile: %v", err)
	}
	if !meta.IsStatusConditionTrue(liveProfile.Status.Conditions, flowProfileConditionDeletionBlocked) || liveProfile.Status.Phase != tamossv1alpha1.FlowProfilePhaseDeleting {
		t.Fatalf("FlowProfile deletion was not reported as blocked: %#v", liveProfile.Status)
	}
	if !controllerutil.ContainsFinalizer(liveProfile, flowProfileFinalizer) {
		t.Fatal("FlowProfile finalizer was removed while the TAMS Profile was in use")
	}
}

func TestFlowProfileDuplicateLoserClearsRegistrationState(t *testing.T) {
	ctx := context.Background()
	scheme := flowProfileTestScheme(t)
	profile := flowProfileFixture()
	profile.Finalizers = []string{flowProfileFinalizer}
	profile.Spec.ID = "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28"
	duplicate := flowProfileFixture()
	duplicate.Name = "aa-hd-avc"
	duplicate.Spec.ID = profile.Spec.ID
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	tamoss := flowProfileTamossFixture()
	schemaState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("schema-state"), Namespace: tamoss.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion}}
	registrationState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: flowProfileResourceName(profile, "state"), Namespace: profile.Namespace}, Data: map[string]string{
		flowProfileStateReady: flowProfileStateValueReady, flowProfileStateID: resolved.Spec.ID, flowProfileStateHash: resolved.Hash,
	}}
	registrationJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: flowProfileResourceName(profile, "register"), Namespace: profile.Namespace}}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.FlowProfile{}).
		WithIndex(&tamossv1alpha1.FlowProfile{}, flowProfileTamossIndex, func(obj client.Object) []string {
			return []string{obj.(*tamossv1alpha1.FlowProfile).Spec.TamossRef.Name}
		}).
		WithObjects(profile, duplicate, tamoss, schemaState, registrationState, registrationJob).Build()
	r := &FlowProfileReconciler{Client: c, Scheme: scheme, WatchNamespaces: WatchNamespaceSet{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(profile)}

	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile duplicate loser: %v", err)
	}
	liveProfile := &tamossv1alpha1.FlowProfile{}
	if err := c.Get(ctx, request.NamespacedName, liveProfile); err != nil {
		t.Fatal(err)
	}
	ready := meta.FindStatusCondition(liveProfile.Status.Conditions, flowProfileConditionReady)
	if liveProfile.Status.Phase != tamossv1alpha1.FlowProfilePhaseDegraded || ready == nil || ready.Reason != "DuplicateProfileID" {
		t.Fatalf("duplicate loser status = %#v", liveProfile.Status)
	}
	for _, object := range []client.Object{
		&corev1.ConfigMap{ObjectMeta: registrationState.ObjectMeta},
		&batchv1.Job{ObjectMeta: registrationJob.ObjectMeta},
	} {
		if err := c.Get(ctx, client.ObjectKeyFromObject(object), object); !apierrors.IsNotFound(err) {
			t.Fatalf("stale registration object %T was retained: %v", object, err)
		}
	}
}

func TestFlowProfileDeletionDistinguishesOwnerFromDuplicate(t *testing.T) {
	for _, tt := range []struct {
		name        string
		currentName string
		otherName   string
		wantBlocked bool
	}{
		{name: "owner waits for duplicate", currentName: "aa-hd-avc", otherName: "zz-hd-avc", wantBlocked: true},
		{name: "duplicate releases without deleting shared profile", currentName: "zz-hd-avc", otherName: "aa-hd-avc", wantBlocked: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := flowProfileTestScheme(t)
			current := flowProfileFixture()
			current.Name = tt.currentName
			current.Finalizers = []string{flowProfileFinalizer}
			current.Spec.ID = "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28"
			other := flowProfileFixture()
			other.Name = tt.otherName
			other.CreationTimestamp = current.CreationTimestamp
			other.Spec.ID = current.Spec.ID
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&tamossv1alpha1.FlowProfile{}).
				WithIndex(&tamossv1alpha1.FlowProfile{}, flowProfileTamossIndex, func(obj client.Object) []string {
					return []string{obj.(*tamossv1alpha1.FlowProfile).Spec.TamossRef.Name}
				}).
				WithObjects(current, other).Build()
			r := &FlowProfileReconciler{Client: c, Scheme: scheme, WatchNamespaces: WatchNamespaceSet{}}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(current)}
			if err := c.Delete(ctx, current); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}

			live := &tamossv1alpha1.FlowProfile{}
			err := c.Get(ctx, request.NamespacedName, live)
			if !tt.wantBlocked {
				if !apierrors.IsNotFound(err) {
					t.Fatalf("duplicate claim was not released: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			condition := meta.FindStatusCondition(live.Status.Conditions, flowProfileConditionDeletionBlocked)
			if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "ProfileClaimedByFlowProfile" {
				t.Fatalf("owner deletion was not blocked: %#v", live.Status)
			}
		})
	}
}

func flowProfileFixture() *tamossv1alpha1.FlowProfile {
	return &tamossv1alpha1.FlowProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "hd-avc", Namespace: "media", CreationTimestamp: metav1.Now()},
		Spec: tamossv1alpha1.FlowProfileSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"}, Label: "HD AVC",
			Tags:         map[string]apiextensionsv1.JSON{"tier": {Raw: []byte(`"mezzanine"`)}},
			FlowMetadata: apiextensionsv1.JSON{Raw: []byte(`{"format":"urn:x-nmos:format:video","codec":"video/h264","container":"video/mp4","essence_parameters":{"frame_rate":{"numerator":25,"denominator":1},"frame_width":1920,"frame_height":1080}}`)},
		},
	}
}

func readyFlowProfileFixture() *tamossv1alpha1.FlowProfile {
	profile := flowProfileFixture()
	profile.Generation = 1
	profile.Status = tamossv1alpha1.FlowProfileStatus{
		ObservedGeneration: 1,
		ProfileID:          "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28",
		Resolved:           tamossv1alpha1.FlowProfileResolvedStatus{Format: "urn:x-nmos:format:video"},
		Conditions:         []metav1.Condition{{Type: flowProfileConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now(), ObservedGeneration: 1}},
	}
	return profile
}

func flowProfileTamossFixture() *tamossv1alpha1.Tamoss {
	tamoss := tamossFixture()
	tamoss.Name = "example"
	tamoss.Namespace = "media"
	tamoss.Spec.API.Image.Repository = "livewyer/tamoss-api"
	tamoss.Spec.API.Image.Tag = "8.2.0-oss1"
	tamoss.Status.Resolved.Versions.TAMSAPI = schemabundle.SupportedTAMSAPIVersion
	return tamoss
}

func flowProfileTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}
