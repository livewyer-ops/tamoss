package controller

import (
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/releases"
	"github.com/livewyer-ops/tamoss/operator/internal/schema"
)

func testRelease() releases.Release {
	return releases.Release{Version: "dev", Images: defaults.DevelopmentImages, Schema: schema.Target{
		Version: schema.SchemaVersion, PreviousVersion: schema.PreviousSupportedSchemaVersion,
		DatabaseRevision: schema.CurrentDatabaseRevision, TAMSAPI: schema.SupportedTAMSAPIVersion,
	}}
}
func testReleases() releases.Catalogue { return releases.Catalogue{"dev": testRelease()} }
func testReleasesWithTAMSin(image string) releases.Catalogue {
	release := testRelease()
	release.Images.TAMSin = image
	return releases.Catalogue{"dev": release}
}

func testSchemaController() *SchemaController { return &SchemaController{Target: testRelease().Schema} }
