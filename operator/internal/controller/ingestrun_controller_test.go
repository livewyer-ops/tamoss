package controller

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const testIngestEndpoint = "https://tams.example.test/v8.1"

func TestDesiredIngestJobUsesFixedSecureTemplate(t *testing.T) {
	run := testIngestRun()
	spec := defaultIngestRunSpec(run.Spec)
	tamoss := testIngestTamoss()
	image := "registry.example/tamsin@sha256:" + strings.Repeat("a", 64)

	job := desiredIngestJob(run, spec, tamoss, testIngestEndpoint, image, "", "", testResolvedIngestInputs())
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("expected service account token automount to be disabled")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoffLimit = %v, want 0 to prevent replaying media writes", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 21600 {
		t.Fatalf("activeDeadlineSeconds = %v, want 21600", job.Spec.ActiveDeadlineSeconds)
	}
	pod := job.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("expected a non-root Pod security context")
	}
	container := pod.Containers[0]
	if container.Image != image {
		t.Fatalf("image = %q, want configured immutable image", container.Image)
	}
	if got := job.Labels["app.kubernetes.io/instance"]; got != tamoss.Name {
		t.Fatalf("instance label = %q, want %q", got, tamoss.Name)
	}
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("expected privilege escalation to be disabled")
	}
	if container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("expected a read-only root filesystem")
	}
	if !slices.Contains(container.Args, "s3://staging/run/input.mp4") {
		t.Fatalf("args do not contain the declared input: %#v", container.Args)
	}
	if !slices.Contains(container.Args, testIngestEndpoint) {
		t.Fatalf("args do not contain the approved HTTPS endpoint: %#v", container.Args)
	}
	if !slices.Contains(container.Args, "none") || slices.Contains(container.Args, "never") {
		t.Fatalf("args do not use TAMSin's supported non-interactive progress mode: %#v", container.Args)
	}
	if slices.Contains(container.Args, "--insecure-skip-verify") {
		t.Fatalf("non-local profile disables TLS verification: %#v", container.Args)
	}
	for _, forbidden := range []string{"--image", "--command", "--env", "--service-account"} {
		if slices.Contains(container.Args, forbidden) {
			t.Fatalf("unsafe user-controlled flag %q reached the Job", forbidden)
		}
	}
	var tokenEnv *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "TAMSIN_AUTH_TOKEN" {
			tokenEnv = &container.Env[i]
		}
	}
	if tokenEnv == nil || tokenEnv.ValueFrom == nil || tokenEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatal("expected TAMSIN_AUTH_TOKEN to come from a Secret key reference")
	}
	if got := tokenEnv.ValueFrom.SecretKeyRef.Name; got != "example-api-token" {
		t.Fatalf("token Secret = %q, want example-api-token", got)
	}
	if job.Annotations[ingestSourceAnnotation] != "test-source" || job.Annotations[ingestSourcePolicyAnnotation] != strings.Repeat("a", 64) {
		t.Fatalf("Job omitted its source-policy identity: %#v", job.Annotations)
	}
	if slices.Contains(container.Args, "--source-policy") {
		t.Fatalf("unsupported source-policy argument reached TAMSin: %#v", container.Args)
	}
	if !slices.Contains(container.Args, "--verify=auto") {
		t.Fatalf("Job omitted TAMSin's verification mode: %#v", container.Args)
	}
}

func TestDesiredIngestJobUsesOnlySourceBoundCredentialReferences(t *testing.T) {
	run := testIngestRun()
	resolved := testResolvedIngestInputs()
	resolved.CredentialSecretName = "archive-reader"
	resolved.CredentialKind = tamossv1alpha1.IngestSourceKindS3
	resolved.S3Endpoint = "https://objects.example.test"
	resolved.S3Region = "eu-west-2"
	resolved.S3PathStyle = true
	job := desiredIngestJob(run, defaultIngestRunSpec(run.Spec), testIngestTamoss(), testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64), "", "", resolved)
	container := job.Spec.Template.Spec.Containers[0]
	for _, key := range []string{s3AccessKeySecretKey, s3SecretKeySecretKey, s3SessionTokenSecretKey} {
		var found *corev1.SecretKeySelector
		for _, env := range container.Env {
			if env.Name == key && env.ValueFrom != nil {
				found = env.ValueFrom.SecretKeyRef
			}
		}
		if found == nil || found.Name != "archive-reader" || found.Key != key {
			t.Fatalf("%s reference = %#v", key, found)
		}
	}
	if !slices.Contains(container.Args, "--s3-path-style") || !slices.Contains(container.Args, resolved.S3Endpoint) {
		t.Fatalf("S3 source settings missing from args: %#v", container.Args)
	}
}

func TestDesiredIngestJobMapsReleasedTAMSinOptions(t *testing.T) {
	run := testIngestRun()
	verify := false
	run.Spec.Profile = tamossv1alpha1.IngestRunProfileMPEGTSegments
	run.Spec.Options.Verify = &verify
	run.Spec.Options.DryRun = true
	run.Spec.Options.TAMSFlowProfiles = []tamossv1alpha1.IngestRunTAMSFlowProfile{
		{Format: "audio", Index: 1, ProfileID: "8d5a25eb-35cb-423b-8e80-72258195ac2c"},
		{Format: "video", Index: 0, ProfileID: "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28"},
		{Format: "audio", Index: 0, ProfileID: "73b13cf7-719a-448d-9852-7c4d5e1bb522"},
	}
	job := desiredIngestJob(run, defaultIngestRunSpec(run.Spec), testIngestTamoss(), testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64), "", "", testResolvedIngestInputs())
	args := job.Spec.Template.Spec.Containers[0].Args
	for _, expected := range []string{"mpegts-segments@1", "--verify=none", "--dry-run=exact"} {
		if !slices.Contains(args, expected) {
			t.Fatalf("args omit %q: %#v", expected, args)
		}
	}
	var assignments []string
	for index, arg := range args {
		if arg == "--tams-flow-profile" && index+1 < len(args) {
			assignments = append(assignments, args[index+1])
		}
	}
	want := []string{
		"video:0=60d9df18-6d9d-4b86-84bf-d1dcf14b3a28",
		"audio:0=73b13cf7-719a-448d-9852-7c4d5e1bb522",
		"audio:1=8d5a25eb-35cb-423b-8e80-72258195ac2c",
	}
	if !slices.Equal(assignments, want) {
		t.Fatalf("TAMS Flow Profile args = %#v, want %#v", assignments, want)
	}
}

func TestDesiredIngestJobRendersConstrainedFlowMetadata(t *testing.T) {
	run := testIngestRun()
	run.Spec.Options.MaxInputs = 1
	run.Spec.Output = &tamossv1alpha1.IngestRunOutputIntent{FlowMetadata: tamossv1alpha1.IngestRunFlowMetadata{
		Label: "8.2 ingest test",
		Tags: map[string]apiextensionsv1.JSON{
			"editorial_purpose": {Raw: []byte(`["testing"]`)},
		},
	}}
	metadata, err := tamossv1alpha1.IngestRunFlowMetadataJSON(run.Spec.Output)
	if err != nil {
		t.Fatal(err)
	}
	job := desiredIngestJob(run, defaultIngestRunSpec(run.Spec), testIngestTamoss(), testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64), "", metadata, testResolvedIngestInputs())
	args := job.Spec.Template.Spec.Containers[0].Args
	index := slices.Index(args, "--flow-metadata")
	if index == -1 || index+1 >= len(args) {
		t.Fatalf("args omit --flow-metadata: %#v", args)
	}
	want := `{"label":"8.2 ingest test","tags":{"editorial_purpose":["testing"]}}`
	if args[index+1] != ingestFlowMetadataFile {
		t.Fatalf("flow metadata path = %q, want %q", args[index+1], ingestFlowMetadataFile)
	}
	if job.Spec.Template.Annotations[ingestFlowMetadataAnnotation] != want {
		t.Fatalf("flow metadata annotation = %q, want %q", job.Spec.Template.Annotations[ingestFlowMetadataAnnotation], want)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if len(container.VolumeMounts) != 2 || container.VolumeMounts[1].Name != ingestFlowMetadataVolume ||
		container.VolumeMounts[1].MountPath != ingestFlowMetadataMountPath || !container.VolumeMounts[1].ReadOnly {
		t.Fatalf("flow metadata mount = %#v", container.VolumeMounts)
	}
	if len(job.Spec.Template.Spec.Volumes) != 2 || job.Spec.Template.Spec.Volumes[1].DownwardAPI == nil {
		t.Fatalf("flow metadata volume = %#v", job.Spec.Template.Spec.Volumes)
	}
	item := job.Spec.Template.Spec.Volumes[1].DownwardAPI.Items[0]
	wantFieldPath := "metadata.annotations['" + ingestFlowMetadataAnnotation + "']"
	if item.Path != "flow-metadata.json" || item.FieldRef == nil || item.FieldRef.FieldPath != wantFieldPath {
		t.Fatalf("flow metadata item = %#v, want field path %q", item, wantFieldPath)
	}
}

func TestIngestRunRejectsInvalidOutputIntentBeforeCreatingJob(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.Options.MaxInputs = 1
	run.Spec.Output = &tamossv1alpha1.IngestRunOutputIntent{FlowMetadata: tamossv1alpha1.IngestRunFlowMetadata{
		Label: "test",
		Tags:  map[string]apiextensionsv1.JSON{"_tamsin_source": {Raw: []byte(`"spoofed"`)}},
	}}
	tamoss := testIngestTamoss()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, tamoss).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhasePending || conditionReason(reloaded.Status.Conditions, ingestRunReadyCondition) != "InvalidOutputIntent" {
		t.Fatalf("status = %+v", reloaded.Status)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("invalid intent created a Job: %v", err)
	}
}

func TestDesiredIngestJobAcceptsLocalKindSelfSignedTLS(t *testing.T) {
	run := testIngestRun()
	tamoss := testIngestTamoss()
	tamoss.Spec.Profile = tamossv1alpha1.TamossProfileLocalKind
	job := desiredIngestJob(
		run,
		defaultIngestRunSpec(run.Spec),
		tamoss,
		testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64),
		"",
		"",
		testResolvedIngestInputs(),
	)

	if !slices.Contains(job.Spec.Template.Spec.Containers[0].Args, "--insecure-skip-verify") {
		t.Fatalf("local-kind args do not accept the disposable self-signed endpoint: %#v", job.Spec.Template.Spec.Containers[0].Args)
	}
}

func TestDesiredIngestJobBoundsLongRunLabel(t *testing.T) {
	run := testIngestRun()
	run.Name = strings.Repeat("a", 44) + "." + strings.Repeat("b", 20)
	spec := defaultIngestRunSpec(run.Spec)
	job := desiredIngestJob(
		run,
		spec,
		testIngestTamoss(),
		testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64),
		"",
		"",
		testResolvedIngestInputs(),
	)
	label := job.Labels[ingestRunLabel]
	if len(label) > 63 {
		t.Fatalf("IngestRun label length = %d, want at most 63", len(label))
	}
	if label != ingestRunSelectorValue(run.Name) || job.Spec.Template.Labels[ingestRunLabel] != label {
		t.Fatal("bounded label must be consistent across the Job and Pod template")
	}
	if problems := kvalidation.IsDNS1123Subdomain(job.Name); len(problems) > 0 {
		t.Fatalf("generated Job name %q is invalid: %v", job.Name, problems)
	}
}

func TestIngestRunReconcileCreatesJobAndReportsRunning(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	tamoss := testIngestTamoss()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &batchv1.Job{}).
		WithObjects(run, tamoss).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("b", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create reconcile failed: %v", err)
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job); err != nil {
		t.Fatalf("expected TAMSin Job: %v", err)
	}
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].Name != run.Name {
		t.Fatalf("unexpected Job owner references: %#v", job.OwnerReferences)
	}
	job.Status.Active = 1
	now := metav1.Now()
	job.Status.StartTime = &now
	if err := k8sClient.Status().Update(ctx, job); err != nil {
		t.Fatalf("update Job status: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("running reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseRunning {
		t.Fatalf("phase = %q, want Running", updated.Status.Phase)
	}
	if updated.Status.JobRef.Name != job.Name {
		t.Fatalf("status job = %q, want %q", updated.Status.JobRef.Name, job.Name)
	}
	if updated.Status.ResolvedSource.Name != "test-source" || updated.Status.ResolvedSource.PolicyDigest != strings.Repeat("a", 64) {
		t.Fatalf("resolved source status = %#v", updated.Status.ResolvedSource)
	}
}

func TestIngestRunWithoutConfiguredImageStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static configuration failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhasePending {
		t.Fatalf("phase = %q, want Pending", updated.Status.Phase)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "TamsinRuntimeUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunWithoutSourcePolicyResolverStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:      k8sClient,
		Scheme:      scheme,
		TamsinImage: "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static input-resolver failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "InputResolverUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunRevalidatesPendingInputAfterPolicyChange(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.Input = tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP,
		URI:  "https://media.example.test/programme.mp4",
	}
	tamoss := testIngestTamoss()
	tamoss.Spec.Ingest.SourcePolicy.Mode = tamossv1alpha1.IngestSourcePolicyDisabled
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &batchv1.Job{}).
		WithObjects(run, tamoss).
		Build()
	reconciler := &IngestRunReconciler{
		Client: k8sClient, Scheme: scheme,
		TamsinImage: "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
		InputResolver: SourcePolicyResolver{
			Client: k8sClient,
			HostResolver: staticIngestHostResolver{
				"media.example.test": {{IP: net.ParseIP("93.184.216.34")}},
			},
		},
		EndpointResolver: staticIngestEndpointResolver{},
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != ingestRunRequeuePending {
		t.Fatalf("disabled policy requeue = %s", result.RequeueAfter)
	}
	updatedRun := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updatedRun); err != nil {
		t.Fatal(err)
	}
	condition := conditionByType(updatedRun.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "InputPolicyRejected" {
		t.Fatalf("disabled policy condition = %#v", condition)
	}
	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tamoss), updatedTamoss); err != nil {
		t.Fatal(err)
	}
	updatedTamoss.Spec.Ingest.SourcePolicy.Mode = tamossv1alpha1.IngestSourcePolicyPublicHTTPS
	if err := k8sClient.Update(ctx, updatedTamoss); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job); err != nil {
		t.Fatalf("policy update did not admit Job: %v", err)
	}
	if job.Annotations[ingestSourceAnnotation] != "public-https" {
		t.Fatalf("Job source annotation = %#v", job.Annotations)
	}
}

func TestIngestRunWithoutApprovedEndpointResolverStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:        k8sClient,
		Scheme:        scheme,
		TamsinImage:   "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
		InputResolver: staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static endpoint-resolver failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestEndpointResolverUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func TestIngestRunDoesNotReplayMissingRecordedJob(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Status.JobRef = tamossv1alpha1.IngestRunJobStatus{Name: ingestJobName(run.Name), UID: types.UID("deleted-job-uid")}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("missing recorded Job should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestJobMissing" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no replayed Jobs, got %d", len(jobs.Items))
	}
}

func TestResolvedIngestInputsRejectCredentialsAndSignedURLs(t *testing.T) {
	for _, location := range []string{
		"https://user:password@example.test/media.mov",
		"https://example.test/media.mov?signature=secret",
		"file:///tmp/media.mov",
	} {
		err := validateResolvedIngestInputs(ResolvedIngestInputs{Selectors: []string{location}}, 1)
		if err == nil {
			t.Fatalf("expected %q to be rejected", location)
		}
	}
}

func TestResolvedIngestInputsBoundsJobSelectors(t *testing.T) {
	selectors := make([]string, maxIngestSelectors+1)
	for i := range selectors {
		selectors[i] = "s3://staging/run/input.mp4"
	}
	if err := validateResolvedIngestInputs(ResolvedIngestInputs{Selectors: selectors}, 10_000); err == nil {
		t.Fatal("expected an oversized top-level selector list to be rejected")
	}
	if err := validateResolvedIngestInputs(ResolvedIngestInputs{
		Selectors:      []string{"s3://staging/run/manifest.txt"},
		ExpectedInputs: 10_001,
	}, 10_000); err == nil {
		t.Fatal("expected an oversized expanded input count to be rejected")
	}
}

func TestValidateIngestEndpointRequiresHTTPSWithoutCredentials(t *testing.T) {
	for _, endpoint := range []string{
		"http://tams-api.media.svc.cluster.local:8000/v8.1",
		"https://user:secret@tams.example.test/v8.1",
		"https://tams.example.test/v8.1?token=secret",
		"https://tams.example.test/v8.1#fragment",
		"https:///v8.1",
	} {
		if _, err := validateIngestEndpoint(endpoint); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}

	got, err := validateIngestEndpoint("  " + testIngestEndpoint + "  ")
	if err != nil {
		t.Fatalf("expected approved endpoint to be accepted: %v", err)
	}
	if got != testIngestEndpoint {
		t.Fatalf("endpoint = %q, want %q", got, testIngestEndpoint)
	}
}

func TestIngestRunRetryRequiresMatchingParentUID(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: "previous-run", UID: "original-parent-uid"}
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("replacement-parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("d", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentReplaced" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunRetryCannotCrossTamossInstances(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Spec.TamossRef.Name = "other-instance"
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("d", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentTargetMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunRetryIncrementsAttempt(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhasePartiallySucceeded
	parent.Status.Attempt = 3
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &batchv1.Job{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("e", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Attempt != 4 {
		t.Fatalf("attempt = %d, want 4", updated.Status.Attempt)
	}
}

func TestIngestRunRetryRequiresTheParentConfiguration(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	run.Spec.Profile = tamossv1alpha1.IngestRunProfilePreserve
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("e", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentConfigurationMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func TestIngestRunResolvesApprovedStorageBackendDestination(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	storageBackend := testIngestStorageBackend()
	run.Spec.Options.StorageBackendRef = &tamossv1alpha1.IngestStorageBackendReference{Name: storageBackend.Name}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &tamossv1alpha1.StorageBackend{}, &batchv1.Job{}).
		WithObjects(run, testIngestTamoss(), storageBackend).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("f", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job); err != nil {
		t.Fatalf("get Job: %v", err)
	}
	args := job.Spec.Template.Spec.Containers[0].Args
	wantID := storageBackend.Status.BackendID
	for i := range args {
		if args[i] == "--storage-id" && i+1 < len(args) && args[i+1] == wantID {
			return
		}
	}
	t.Fatalf("Job args do not contain the approved storage ID %q: %#v", wantID, args)
}

func TestIngestRunRejectsStorageBackendFromAnotherTamoss(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	storageBackend := testIngestStorageBackend()
	storageBackend.Spec.TamossRef.Name = "other-instance"
	run.Spec.Options.StorageBackendRef = &tamossv1alpha1.IngestStorageBackendReference{Name: storageBackend.Name}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &tamossv1alpha1.StorageBackend{}).
		WithObjects(run, testIngestTamoss(), storageBackend).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("f", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestStorageBackendTargetMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestPhaseRecognisesPartialBatchExit(t *testing.T) {
	job := &batchv1.Job{
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "backoff limit reached",
		}}},
	}
	phase, reason, _, progressing := ingestPhaseFromJob(job, verifiedIngestResult(), time.Now(), partialIngestSummary())
	if phase != tamossv1alpha1.IngestRunPhasePartiallySucceeded || reason != "IngestPartiallySucceeded" || progressing {
		t.Fatalf("got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestPhaseWaitsForValidatedTerminalStream(t *testing.T) {
	job := &batchv1.Job{
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
		}}},
	}
	phase, reason, _, progressing := ingestPhaseFromJob(job, verifiedIngestResult(), time.Now(), nil)
	if phase != tamossv1alpha1.IngestRunPhaseRunning || reason != "IngestResultPending" || progressing {
		t.Fatalf("without a validated stream got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestPhasePropagatesTerminalPodListErrors(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media", UID: types.UID("job-uid")},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}},
	}
	reader := failingPodListReader{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()}

	_, _, _, err := terminalIngestPodResult(ctx, reader, job)
	if err == nil || !strings.Contains(err.Error(), "list terminal Pods") {
		t.Fatalf("expected Pod list error, got %v", err)
	}
}

func TestIngestPhaseWaitsForDigestVerifiedResult(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobComplete, Status: corev1.ConditionTrue,
		}}},
	}

	// A recorded result must still carry a valid digest before the run is
	// believed, so a collector cannot publish a half-written artefact and have
	// the run claim success on it.
	unverified := verifiedIngestResult()
	unverified.Verified = false
	phase, reason, _, progressing := ingestPhaseFromJob(job, unverified, time.Now(), succeededIngestSummary())
	if phase != tamossv1alpha1.IngestRunPhaseRunning || reason != "ResultVerificationPending" || progressing {
		t.Fatalf("with an unverified result got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}

	// TAMSin publishes a complete terminal event stream rather than a separate
	// durable result artefact, so a run without one succeeds from that stream.
	phase, reason, _, _ = ingestPhaseFromJob(job, tamossv1alpha1.IngestRunResultStatus{}, time.Now(), succeededIngestSummary())
	if phase != tamossv1alpha1.IngestRunPhaseSucceeded || reason != "IngestSucceeded" {
		t.Fatalf("without a recorded result got phase=%q reason=%q, want Succeeded", phase, reason)
	}

	phase, reason, _, progressing = ingestPhaseFromJob(job, verifiedIngestResult(), time.Now(), succeededIngestSummary())
	if phase != tamossv1alpha1.IngestRunPhaseSucceeded || reason != "IngestSucceeded" || progressing {
		t.Fatalf("with verified result got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestRunCancellationDeletesJobAndRetainsRun(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	job := desiredIngestJob(run, defaultIngestRunSpec(run.Spec), testIngestTamoss(), testIngestEndpoint, "tamsin:test", "", "", testResolvedIngestInputs())
	job.OwnerReferences = []metav1.OwnerReference{{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "IngestRun", Name: run.Name, UID: run.UID, Controller: ptr.To(true)}}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &batchv1.Job{}).
		WithObjects(run, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme, TamsinImage: "tamsin:test"}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("cancelling reconcile failed: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected Job to be gone after foreground deletion, got %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("cancelled reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatalf("phase = %q, want Cancelled", updated.Status.Phase)
	}
}

func TestIngestRunCancellationWaitsForOwnedPodsToDisappear(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	run.Status.JobRef = tamossv1alpha1.IngestRunJobStatus{Name: ingestJobName(run.Name), UID: types.UID("job-uid")}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tamsin-daily-news-abc",
			Namespace: run.Namespace,
			Labels: map[string]string{
				ingestRunLabel:       run.Name,
				ingestRunTargetLabel: run.Spec.TamossRef.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: run.Status.JobRef.Name, UID: run.Status.JobRef.UID}},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}).
		WithObjects(run, pod).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile while Pod remains: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected cancellation to poll for Pod termination")
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase == tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatal("run was marked Cancelled while an owned Pod remained")
	}
	if err := k8sClient.Delete(ctx, pod); err != nil {
		t.Fatalf("delete Pod: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile after Pod deletion: %v", err)
	}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get cancelled IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatalf("phase = %q, want Cancelled", updated.Status.Phase)
	}
}

func TestIngestRunCancellationDoesNotDeleteConflictingJob(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: ingestJobName(run.Name), Namespace: run.Namespace, UID: types.UID("foreign-job")}}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}).
		WithObjects(run, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); err != nil {
		t.Fatalf("conflicting Job was deleted: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "JobOwnershipConflict" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func ingestRunTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for name, add := range map[string]func(*runtime.Scheme) error{
		"core":   corev1.AddToScheme,
		"batch":  batchv1.AddToScheme,
		"tamoss": tamossv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add %s scheme: %v", name, err)
		}
	}
	return scheme
}

func testIngestRun() *tamossv1alpha1.IngestRun {
	return &tamossv1alpha1.IngestRun{
		ObjectMeta: metav1.ObjectMeta{Name: "daily-news", Namespace: "media", UID: types.UID("run-uid"), Generation: 1},
		Spec: tamossv1alpha1.IngestRunSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			Input:     tamossv1alpha1.IngestRunInput{Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://staging/run/input.mp4"},
			Profile:   tamossv1alpha1.IngestRunProfileEssenceSegments,
			SizeClass: tamossv1alpha1.IngestRunSizeClassSmall,
			Options: tamossv1alpha1.IngestRunOptions{
				Verify:    ptr.To(true),
				MaxInputs: 1000,
			},
			DesiredState: tamossv1alpha1.IngestRunDesiredStateRunning,
		},
	}
}

func testIngestTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media", UID: types.UID("tamoss-uid"), Generation: 1},
		Spec: tamossv1alpha1.TamossSpec{
			Secrets: tamossv1alpha1.SecretsSpec{APIToken: tamossv1alpha1.APITokenSecretSpec{Generate: true}},
		},
		Status: tamossv1alpha1.TamossStatus{
			ObservedGeneration: 1,
			Conditions: []metav1.Condition{{
				Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now(), ObservedGeneration: 1,
			}},
		},
	}
}

func testIngestStorageBackend() *tamossv1alpha1.StorageBackend {
	const backendID = "f1ab5b54-9703-42ed-b181-11ba1c794a7f"
	return &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "media-primary", Namespace: "media", UID: types.UID("storage-uid"), Generation: 1},
		Spec: tamossv1alpha1.StorageBackendSpec{
			ID:        backendID,
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			Usage:     tamossv1alpha1.StorageBackendUsageMedia,
		},
		Status: tamossv1alpha1.StorageBackendStatus{
			ObservedGeneration: 1,
			BackendID:          backendID,
			Resolved:           tamossv1alpha1.StorageBackendResolvedStatus{BackendID: backendID},
			Conditions: []metav1.Condition{{
				Type: operatorstatus.ConditionReady, Status: metav1.ConditionTrue, Reason: operatorstatus.ReasonStorageBackendReady,
				LastTransitionTime: metav1.Now(), ObservedGeneration: 1,
			}},
		},
	}
}

func verifiedIngestResult() tamossv1alpha1.IngestRunResultStatus {
	return tamossv1alpha1.IngestRunResultStatus{
		Key:       "ingest-results/run-uid.json",
		SHA256:    strings.Repeat("a", 64),
		Size:      512,
		MediaType: "application/json",
		Verified:  true,
	}
}

type staticIngestInputResolver struct {
	selectors []string
}

func testResolvedIngestInputs() ResolvedIngestInputs {
	return ResolvedIngestInputs{
		Selectors: []string{"s3://staging/run/input.mp4"}, ExpectedInputs: 1, SourceName: "test-source",
		PolicyDigest: strings.Repeat("a", 64),
	}
}

func (r staticIngestInputResolver) Resolve(context.Context, *tamossv1alpha1.Tamoss, tamossv1alpha1.IngestRunInput, int32) (ResolvedIngestInputs, error) {
	return ResolvedIngestInputs{
		Selectors: r.selectors, ExpectedInputs: int32(len(r.selectors)), SourceName: "test-source",
		PolicyDigest: strings.Repeat("a", 64),
	}, nil
}

type staticIngestEndpointResolver struct {
	endpoint string
}

type failingPodListReader struct {
	client.Client
}

func (r failingPodListReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.PodList); ok {
		return errors.New("temporary Pod cache failure")
	}
	return r.Client.List(ctx, list, opts...)
}

func (r staticIngestEndpointResolver) Resolve(context.Context, string, string) (string, error) {
	if r.endpoint == "" {
		return testIngestEndpoint, nil
	}
	return r.endpoint, nil
}

func conditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
