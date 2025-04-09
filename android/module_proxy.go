package android

import (
	"github.com/google/blueprint"
)

type ModuleProxy struct {
	blueprint.ModuleProxy
}

type ModuleOrProxy interface {
	blueprint.ModuleOrProxy
}
