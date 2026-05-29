package controller

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

var defaultStorageBackendHTTPClient = &http.Client{Timeout: 5 * time.Second}

type storageBackendDiagnosticResult struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

func (r *StorageBackendReconciler) externalS3Diagnostic(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) *storageBackendDiagnosticResult {
	if !spec.IsExternalObjectStore() {
		return nil
	}
	endpoint := strings.TrimSpace(spec.Endpoint.Public.URL)
	if endpoint == "" {
		endpoint = strings.TrimSpace(spec.Endpoint.Default.URL)
	}
	if endpoint == "" {
		return &storageBackendDiagnosticResult{
			Status:  metav1.ConditionUnknown,
			Reason:  operatorstatus.ReasonExternalS3DiagnosticSkipped,
			Message: "External S3 diagnostic skipped because no endpoint URL is configured",
		}
	}
	originBase := strings.TrimSpace(tamoss.Spec.PublicEndpoint.BaseDomain)
	if originBase == "" {
		return &storageBackendDiagnosticResult{
			Status:  metav1.ConditionUnknown,
			Reason:  operatorstatus.ReasonExternalS3DiagnosticSkipped,
			Message: "External S3 diagnostic skipped because publicEndpoint.baseDomain is not configured",
		}
	}
	origin := "https://app." + originBase
	request, err := http.NewRequestWithContext(ctx, http.MethodOptions, endpoint, nil)
	if err != nil {
		return &storageBackendDiagnosticResult{
			Status:  metav1.ConditionFalse,
			Reason:  operatorstatus.ReasonEndpointUnreachable,
			Message: fmt.Sprintf("External S3 diagnostic could not build request for %s: %v", endpoint, err),
		}
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response, err := r.storageBackendHTTPClient().Do(request)
	if err != nil {
		reason := operatorstatus.ReasonEndpointUnreachable
		if isTLSValidationError(err) {
			reason = operatorstatus.ReasonTLSValidationFailed
		}
		return &storageBackendDiagnosticResult{
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: fmt.Sprintf("External S3 diagnostic failed for %s from origin %s: %v", endpoint, origin, err),
		}
	}
	defer func() { _ = response.Body.Close() }()
	if corsPreflightAllowed(response.Header, origin) {
		return &storageBackendDiagnosticResult{
			Status:  metav1.ConditionTrue,
			Reason:  operatorstatus.ReasonExternalS3DiagnosticReady,
			Message: fmt.Sprintf("External S3 diagnostic accepted browser upload preflight from origin %s", origin),
		}
	}
	return &storageBackendDiagnosticResult{
		Status:  metav1.ConditionFalse,
		Reason:  operatorstatus.ReasonCORSMisconfigured,
		Message: fmt.Sprintf("External S3 diagnostic did not observe CORS headers allowing PUT from origin %s", origin),
	}
}

func (r *StorageBackendReconciler) storageBackendHTTPClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return defaultStorageBackendHTTPClient
}

func corsPreflightAllowed(header http.Header, origin string) bool {
	allowedOrigin := header.Get("Access-Control-Allow-Origin")
	if allowedOrigin != "*" && !strings.EqualFold(allowedOrigin, origin) {
		return false
	}
	allowedMethods := header.Values("Access-Control-Allow-Methods")
	for _, value := range allowedMethods {
		for _, method := range strings.Split(value, ",") {
			method = strings.TrimSpace(method)
			if method == "*" || strings.EqualFold(method, http.MethodPut) {
				return true
			}
		}
	}
	return false
}

func isTLSValidationError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalid x509.CertificateInvalidError
	return errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &certificateInvalid) ||
		strings.Contains(strings.ToLower(err.Error()), "certificate")
}
