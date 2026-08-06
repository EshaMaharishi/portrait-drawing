// Package main is a Viam module for drawing portraits: a camera that converts
// an image into poses, a service that moves an arm through them, and a world
// state store that shows them in the visualizer.
package main

import (
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	"go.viam.com/rdk/services/worldstatestore"

	"portrait-drawing/backgroundremoval"
	"portrait-drawing/imagetoposes"
	"portrait-drawing/posesto3dscene"
	"portrait-drawing/posestoarm"
	"portrait-drawing/tablesurface"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: camera.API, Model: imagetoposes.Model},
		resource.APIModel{API: camera.API, Model: backgroundremoval.Model},
		resource.APIModel{API: generic.API, Model: posestoarm.Model},
		resource.APIModel{API: worldstatestore.API, Model: posesto3dscene.Model},
		resource.APIModel{API: generic.API, Model: tablesurface.Model},
	)
}
