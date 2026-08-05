// Package main is a Viam module exposing a camera component with a "draw" DoCommand.
package main

import (
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"

	"portrait-drawing/drawing"
)

func main() {
	module.ModularMain(resource.APIModel{API: camera.API, Model: drawing.Model})
}
