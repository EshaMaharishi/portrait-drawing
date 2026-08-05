// Package main is a Viam module exposing a camera component with a "draw" DoCommand.
package main

import (
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/worldstatestore"

	"portrait-drawing/drawing"
	"portrait-drawing/posestore"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: camera.API, Model: drawing.Model},
		resource.APIModel{API: worldstatestore.API, Model: posestore.Model},
	)
}
