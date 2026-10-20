package controller

import (
	"github.com/livewyer-ops/tamoss/operator/internal/releases"
)

func testRelease() releases.Release {
	catalogue, err := releases.Load("../../config/releases/catalogue.json")
	if err != nil {
		panic(err)
	}
	return catalogue["dev"]
}
func testReleases() releases.Catalogue { return releases.Catalogue{"dev": testRelease()} }
func testReleasesWithTAMSin(image string) releases.Catalogue {
	release := testRelease()
	release.Images.TAMSin = image
	return releases.Catalogue{"dev": release}
}

func testSchemaController() *SchemaController { return &SchemaController{Target: testRelease().Schema} }
