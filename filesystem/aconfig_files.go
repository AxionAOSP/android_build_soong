// Copyright (C) 2024 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filesystem

import (
	"android/soong/android"
	"strconv"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	pctx.HostBinToolVariable("aconfig", "aconfig")
}

var (
	aconfigCreateStorage = pctx.AndroidStaticRule("aconfig_create_storage", blueprint.RuleParams{
		Command:     `$aconfig create-storage --container $container --file $fileType --out $out --cache $in --version $version`,
		CommandDeps: []string{"$aconfig"},
	}, "container", "fileType", "version")
)

type installedAconfigFlagsInfo struct {
	aconfigFiles android.Paths
}

var installedAconfigFlagsProvider = blueprint.NewProvider[installedAconfigFlagsInfo]()

type importAconfigDepDag struct {
	blueprint.BaseDependencyTag
}

var importAconfigDependencyTag = interPartitionDepTag{}

func (f *filesystem) buildAconfigFlagsFiles(
	ctx android.ModuleContext,
	builder *android.RuleBuilder,
	specs map[string]android.PackagingSpec,
	dir android.OutputPath,
	fullInstallPaths *[]FullInstallPathInfo,
) {
	var caches []android.Path
	for _, ps := range specs {
		caches = append(caches, ps.GetAconfigPaths()...)
	}

	ctx.VisitDirectDepsWithTag(importAconfigDependencyTag, func(m android.Module) {
		info, ok := android.OtherModuleProvider(ctx, m, installedAconfigFlagsProvider)
		if !ok {
			ctx.ModuleErrorf("expected dependency %s to have an installedAconfigFlagsProvider", m.Name())
			return
		}
		caches = append(caches, info.aconfigFiles...)
	})
	caches = android.SortedUniquePaths(caches)

	android.SetProvider(ctx, installedAconfigFlagsProvider, installedAconfigFlagsInfo{
		aconfigFiles: caches,
	})

	if !proptools.Bool(f.properties.Gen_aconfig_flags_pb) {
		return
	}

	container := f.PartitionType()

	aconfigFlagsPb := android.PathForModuleOut(ctx, "aconfig", "aconfig_flags.pb")
	aconfigFlagsPbBuilder := android.NewRuleBuilder(pctx, ctx)
	cmd := aconfigFlagsPbBuilder.Command().
		BuiltTool("aconfig").
		Text(" dump-cache --dedup --format protobuf --out").
		Output(aconfigFlagsPb).
		Textf("--filter container:%s+state:ENABLED", container).
		Textf("--filter container:%s+permission:READ_WRITE", container)
	for _, cache := range caches {
		cmd.FlagWithInput("--cache ", cache)
	}
	aconfigFlagsPbBuilder.Build("aconfig_flags_pb", "build aconfig_flags.pb")

	installAconfigFlagsPath := dir.Join(ctx, "etc", "aconfig_flags.pb")
	builder.Command().Text("mkdir -p ").Text(dir.Join(ctx, "etc").String())
	builder.Command().Text("cp").Input(aconfigFlagsPb).Text(installAconfigFlagsPath.String())
	*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
		FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), "etc/aconfig_flags.pb"),
		SourcePath:      aconfigFlagsPb,
	})
	f.appendToEntry(ctx, installAconfigFlagsPath)

	// To enable fingerprint, we need to have v2 storage files. The default version is 1.
	storageFilesVersion := 1
	if ctx.Config().ReleaseFingerprintAconfigPackages() {
		storageFilesVersion = 2
	}

	installAconfigStorageDir := dir.Join(ctx, "etc", "aconfig")
	builder.Command().Text("mkdir -p").Text(installAconfigStorageDir.String())

	generatePartitionAconfigStorageFile := func(fileType, fileName string) {
		outPath := android.PathForModuleOut(ctx, "aconfig", fileName)
		installPath := installAconfigStorageDir.Join(ctx, fileName)
		ctx.Build(pctx, android.BuildParams{
			Rule:   aconfigCreateStorage,
			Input:  aconfigFlagsPb,
			Output: outPath,
			Args: map[string]string{
				"container": container,
				"fileType":  fileType,
				"version":   strconv.Itoa(storageFilesVersion),
			},
		})
		builder.Command().
			Text("cp").Input(outPath).Text(installPath.String())
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			SourcePath:      outPath,
			FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), "etc/aconfig", fileName),
		})
		f.appendToEntry(ctx, installPath)
	}

	if ctx.Config().ReleaseCreateAconfigStorageFile() {
		generatePartitionAconfigStorageFile("package_map", "package.map")
		generatePartitionAconfigStorageFile("flag_map", "flag.map")
		generatePartitionAconfigStorageFile("flag_val", "flag.val")
		generatePartitionAconfigStorageFile("flag_info", "flag.info")
	}
}
