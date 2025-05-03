// Copyright 2025 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package unbundled

import (
	"android/soong/android"
	"android/soong/cc"
	"slices"

	"github.com/google/blueprint"
)

func init() {
	android.InitRegistrationContext.RegisterModuleType("unbundled_builder", unbundledBuilderFactory)
}

func unbundledBuilderFactory() android.Module {
	m := &unbundledBuilder{}
	android.InitAndroidArchModule(m, android.DeviceSupported, android.MultilibFirst)
	return m
}

// unbundledBuilder handles building and disting certain artifacts for "unbundled builds".
// unbundled builds are builds that aren't for a specific device, such as when you just want to
// build an app or an apex.
type unbundledBuilder struct {
	android.ModuleBase
}

type unbundledDepTagType struct {
	blueprint.BaseDependencyTag
}

func (unbundledDepTagType) ExcludeFromVisibilityEnforcement() {}

var _ android.ExcludeFromVisibilityEnforcementTag = unbundledDepTagType{}

var unbundledDepTag = unbundledDepTagType{}

// We need to implement IsNativeCoverageNeeded so that in coverage builds we depend on the coverage
// variants of the unbundled apps. Only the coverage variants export symbols info.
func (p *unbundledBuilder) IsNativeCoverageNeeded(ctx cc.IsNativeCoverageNeededContext) bool {
	return ctx.DeviceConfig().NativeCoverageEnabled()
}

var _ cc.UseCoverage = (*unbundledBuilder)(nil)

func (*unbundledBuilder) DepsMutator(ctx android.BottomUpMutatorContext) {
	apps := ctx.Config().UnbundledBuildApps()
	apps = slices.Clone(apps)
	slices.Sort(apps)

	for _, app := range apps {
		ctx.AddDependency(ctx.Module(), unbundledDepTag, app)
	}
}

func (*unbundledBuilder) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	ctx.Module().HideFromMake()
	if ctx.ModuleDir() != "build/soong" {
		ctx.ModuleErrorf("There can only be 1 unbundled_builder in build/soong")
		return
	}
	if !ctx.Config().HasUnbundledBuildApps() {
		return
	}

	var appModules []android.ModuleOrProxy
	ctx.VisitDirectDepsProxyWithTag(unbundledDepTag, func(m android.ModuleProxy) {
		appModules = append(appModules, m)
	})

	targetProductPrefix := ""
	if ctx.Config().HasDeviceProduct() {
		targetProductPrefix = ctx.Config().DeviceProduct()
		if ctx.Config().BuildType() == "debug" {
			targetProductPrefix += "_debug"
		}
		targetProductPrefix += "-"
	}

	symbolsZip := android.PathForOutput(ctx, "unbundled_singleton", targetProductPrefix+"symbols.zip")
	symbolsMapping := android.PathForOutput(ctx, "unbundled_singleton", targetProductPrefix+"symbols-mapping.textproto")
	android.BuildSymbolsZip(ctx, appModules, symbolsZip, symbolsMapping)
	ctx.DistForGoalWithFilenameTag("apps_only", symbolsZip, symbolsZip.Base())
	ctx.DistForGoalWithFilenameTag("apps_only", symbolsMapping, symbolsMapping.Base())
}
