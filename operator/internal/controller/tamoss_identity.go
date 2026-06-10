package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type identityReconcileResult struct {
	BlueprintSubmitted bool
	Ready              bool
	Reason             string
	Message            string
}

func externalIdentityConfiguration(tamoss *tamossv1alpha1.Tamoss) identityReconcileResult {
	result := identityReconcileResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonExternalIdentityConfigured,
		Message: "External identity configuration is active",
	}
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByExternal {
		return result
	}
	oauth2 := tamoss.Spec.Auth.OAuth2Config(tamoss.Namespace, tamoss.Name)
	if !oauth2.Enabled {
		return result
	}
	missing := []string{}
	if strings.TrimSpace(oauth2.Issuer) == "" {
		missing = append(missing, ".spec.auth.external.oauth2.issuer")
	}
	if strings.TrimSpace(oauth2.JWKSURI) == "" {
		missing = append(missing, ".spec.auth.external.oauth2.jwksUri")
	}
	if len(oauth2.Algorithms) == 0 {
		missing = append(missing, ".spec.auth.external.oauth2.algorithms")
	}
	if len(missing) == 0 {
		return result
	}
	return identityReconcileResult{
		Ready:   false,
		Reason:  operatorstatus.ReasonMissingProviderConfiguration,
		Message: fmt.Sprintf("Required external OAuth2 configuration is missing: %s", strings.Join(missing, ", ")),
	}
}

func (r *TamossReconciler) deleteAuthentikManagedBlueprint(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		return nil
	}
	token, err := authentik.ResolveAPIToken(ctx, r.Client, tamoss)
	if err != nil {
		return err
	}
	if token.Token == "" {
		r.recordWarning(tamoss, operatorstatus.ReasonAuthentikAPITokenMissing, token.Message)
		return nil
	}
	return authentik.NewManagedBlueprintClient(tamoss, token.Token, r.AuthentikHTTPClient).DeleteByName(ctx, authentik.ManagedBlueprintName(tamoss))
}

func (r *TamossReconciler) deleteAuthentikProxyOutpost(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		return nil
	}
	token, err := authentik.ResolveAPIToken(ctx, r.Client, tamoss)
	if err != nil {
		return err
	}
	if token.Token == "" {
		r.recordWarning(tamoss, operatorstatus.ReasonAuthentikAPITokenMissing, token.Message)
		return nil
	}
	return authentik.NewProxyOutpostClient(tamoss, token.Token, r.AuthentikHTTPClient).Delete(ctx, tamoss)
}

func (r *TamossReconciler) updateIdentityBlockedStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, reason, message string) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
		tamoss.Status.Phase = operatorstatus.PhaseDegraded
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, reason, "Schema reconciliation is blocked by identity configuration")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, true, operatorstatus.ReasonBackendReferencesConfigured, "Backend secret references are configured")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionIdentityBlueprintSubmitted, false, reason, message)
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionIdentityReady, false, reason, message)
		setActiveBlockedConditions(&tamoss.Status.Conditions, tamoss.Generation, reason, message, "Reconciliation is blocked by identity configuration")
		return nil
	}})
}

func (r *TamossReconciler) identityResult(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) identityReconcileResult {
	switch tamoss.Spec.Auth.Provider() {
	case tamossv1alpha1.AuthProvidedByNone:
		return identityReconcileResult{
			BlueprintSubmitted: false,
			Ready:              true,
			Reason:             operatorstatus.ReasonAuthenticationDisabled,
			Message:            "Authentication is disabled",
		}
	case tamossv1alpha1.AuthProvidedByAuthentikBlueprints:
		result := identityReconcileResult{
			BlueprintSubmitted: true,
			Ready:              false,
			Reason:             operatorstatus.ReasonIssuerUnreachable,
			Message:            "Authentik issuer has not become reachable",
		}
		if tamoss.Spec.Auth.AuthentikBlueprints == nil {
			result.Message = "auth.authentikBlueprints is required"
			return result
		}
		slug := tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name)
		issuerURL := authentik.APIBaseURL(tamoss)
		err := authentik.ProbeWithClient(ctx, authentik.HTTPClientOrDefault(r.AuthentikHTTPClient), issuerURL, slug)
		if err != nil {
			result.Message = fmt.Sprintf("Authentik issuer is unreachable for application %s: %v", slug, err)
			return result
		}
		result.Ready = true
		result.Reason = operatorstatus.ReasonIssuerReachable
		result.Message = fmt.Sprintf("Authentik issuer is reachable for application %s", slug)
		return result
	default:
		return identityReconcileResult{
			BlueprintSubmitted: false,
			Ready:              true,
			Reason:             operatorstatus.ReasonExternalIdentityConfigured,
			Message:            "External identity configuration is active",
		}
	}
}

func identityBlueprintReason(result identityReconcileResult) string {
	if result.BlueprintSubmitted {
		return operatorstatus.ReasonManagedBlueprintApplied
	}
	return operatorstatus.ReasonBlueprintNotRequired
}

func identityBlueprintMessage(result identityReconcileResult) string {
	if result.BlueprintSubmitted {
		return "Authentik managed Blueprint has been applied"
	}
	return "Blueprint submission is not required for this auth provider"
}

func (r *TamossReconciler) authentikProbeInterval() time.Duration {
	if r.AuthentikProbeInterval > 0 {
		return r.AuthentikProbeInterval
	}
	return defaultAuthentikProbeInterval
}
