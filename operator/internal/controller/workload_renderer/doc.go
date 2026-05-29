// Package workload_renderer converts Tamoss custom resources into Kubernetes
// workload objects owned by the TAMOSS operator.
//
// The underscore is intentional for now: this package is an internal rendering
// boundary and the current import path already reflects the controller task
// domain. Rename it only as a single mechanical change with all imports.
package workload_renderer
