// Package main is a Viam module exposing a generic service with a "draw" DoCommand.
package main

import (
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"

	"portrait-drawing/drawing"
)

func main() {
	module.ModularMain(resource.APIModel{API: generic.API, Model: drawing.Model})
}
