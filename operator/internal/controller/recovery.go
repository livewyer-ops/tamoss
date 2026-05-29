package controller

const (
	AnnotationSchemaRetry     = "tamoss.livewyer.io/schema-retry"
	AnnotationAPITokenRotate  = "tamoss.livewyer.io/api-token-rotate"
	annotationSchemaRetryDone = "tamoss.livewyer.io/schema-retry-consumed"
	annotationAPITokenDone    = "tamoss.livewyer.io/api-token-rotate-consumed"
)

type recoveryActionEvent struct {
	Type    string
	Reason  string
	Message string
}
