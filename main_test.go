package main

import (
	"testing"

	"go.viam.com/rdk/components/camera"
	genericcomponent "go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	"go.viam.com/rdk/services/worldstatestore"
	"go.viam.com/test"

	"portrait-drawing/backgroundremoval"
	"portrait-drawing/facecrop"
	"portrait-drawing/imagetoposes"
	"portrait-drawing/posesto3dscene"
	"portrait-drawing/posestoarm"
	"portrait-drawing/web"
)

// TestModelsRegistered checks every model main passes to ModularMain is
// registered under that API; a mismatch makes the module exit at startup.
func TestModelsRegistered(t *testing.T) {
	for _, am := range []resource.APIModel{
		{API: camera.API, Model: imagetoposes.Model},
		{API: camera.API, Model: backgroundremoval.Model},
		{API: camera.API, Model: facecrop.Model},
		{API: generic.API, Model: posestoarm.Model},
		{API: worldstatestore.API, Model: posesto3dscene.Model},
		{API: genericcomponent.API, Model: web.Model},
	} {
		_, ok := resource.LookupRegistration(am.API, am.Model)
		test.That(t, ok, test.ShouldBeTrue)
	}
}
