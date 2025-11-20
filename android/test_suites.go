// Copyright 2020 Google Inc. All rights reserved.
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

package android

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/google/blueprint"
)

//go:generate go run ../../blueprint/gobtools/codegen/gob_gen.go

func init() {
	RegisterParallelSingletonType("testsuites", testSuiteFilesFactory)
}

func testSuiteFilesFactory() Singleton {
	return &testSuiteFiles{}
}

type testSuiteFiles struct{}

type TestSuiteModule interface {
	Module
	TestSuites() []string
}

// @auto-generate: gob
type TestSuiteInfo struct {
	// A suffix to append to the name of the test.
	// Useful because historically different variants of soong modules became differently-named
	// make modules, like "my_test.vendor" for the vendor variant.
	NameSuffix string

	TestSuites []string

	NeedsArchFolder bool

	MainFile Path

	MainFileStem string

	MainFileExt string

	ConfigFile Path

	ConfigFileSuffix string

	ExtraConfigs Paths

	PerTestcaseDirectory bool

	Data []DataPath

	NonArchData []DataPath

	CompatibilitySupportFiles []Path

	// Eqivalent of LOCAL_DISABLE_TEST_CONFIG in make
	DisableTestConfig bool
}

var TestSuiteInfoProvider = blueprint.NewProvider[TestSuiteInfo]()

// TestSuiteSharedLibsInfo is a provider of AndroidMk names of shared lib modules, for packaging
// shared libs into test suites. It's not intended as a general-purpose shared lib tracking
// mechanism. It's added to both test modules (to track their shared libs) and also shared lib
// modules (to track their transitive shared libs).
// @auto-generate: gob
type TestSuiteSharedLibsInfo struct {
	MakeNames []string
}

var TestSuiteSharedLibsInfoProvider = blueprint.NewProvider[TestSuiteSharedLibsInfo]()

// MakeNameInfoProvider records the AndroidMk name for the module. This will match the names
// referenced in TestSuiteSharedLibsInfo
// @auto-generate: gob
type MakeNameInfo struct {
	Name string
}

var MakeNameInfoProvider = blueprint.NewProvider[MakeNameInfo]()

// @auto-generate: gob
type filePair struct {
	src Path
	dst WritablePath
}

// @auto-generate: gob
type testSuiteInstallsInfo struct {
	Files              []filePair
	OneVariantInstalls []filePair
}

var testSuiteInstallsInfoProvider = blueprint.NewProvider[testSuiteInstallsInfo]()

type testModulesInstallsMap map[ModuleOrProxy]InstallPaths

func (t testModulesInstallsMap) testModules() []ModuleOrProxy {
	return slices.Collect(maps.Keys(t))
}

func (t *testSuiteFiles) GenerateBuildActions(ctx SingletonContext) {
	hostOutTestCases := pathForInstall(ctx, ctx.Config().BuildOSTarget.Os, ctx.Config().BuildOSTarget.Arch.ArchType, "testcases")
	files := make(map[string]testModulesInstallsMap)
	sharedLibRoots := make(map[string][]string)
	sharedLibGraph := make(map[string][]string)
	allTestSuiteInstalls := make(map[string][]Path)
	var toInstall []filePair
	var oneVariantInstalls []filePair

	ctx.VisitAllModuleProxies(func(m ModuleProxy) {
		commonInfo := OtherModuleProviderOrDefault(ctx, m, CommonModuleInfoProvider)
		testSuiteSharedLibsInfo := OtherModuleProviderOrDefault(ctx, m, TestSuiteSharedLibsInfoProvider)
		makeName := OtherModuleProviderOrDefault(ctx, m, MakeNameInfoProvider).Name
		if makeName != "" && commonInfo.Target.Os.Class == Host {
			sharedLibGraph[makeName] = append(sharedLibGraph[makeName], testSuiteSharedLibsInfo.MakeNames...)
		}

		if tsm, ok := OtherModuleProvider(ctx, m, TestSuiteInfoProvider); ok {
			installFilesProvider := OtherModuleProviderOrDefault(ctx, m, InstallFilesProvider)

			for _, testSuite := range tsm.TestSuites {
				if files[testSuite] == nil {
					files[testSuite] = make(testModulesInstallsMap)
				}
				files[testSuite][m] = append(files[testSuite][m],
					installFilesProvider.InstallFiles...)

				if makeName != "" {
					sharedLibRoots[testSuite] = append(sharedLibRoots[testSuite], makeName)
				}
			}

			if testSuiteInstalls, ok := OtherModuleProvider(ctx, m, testSuiteInstallsInfoProvider); ok {
				for _, testSuite := range tsm.TestSuites {
					for _, f := range testSuiteInstalls.Files {
						allTestSuiteInstalls[testSuite] = append(allTestSuiteInstalls[testSuite], f.dst)
					}
					for _, f := range testSuiteInstalls.OneVariantInstalls {
						allTestSuiteInstalls[testSuite] = append(allTestSuiteInstalls[testSuite], f.dst)
					}
				}
				installs := OtherModuleProviderOrDefault(ctx, m, InstallFilesProvider).InstallFiles
				oneVariantInstalls = append(oneVariantInstalls, testSuiteInstalls.OneVariantInstalls...)
				for _, f := range testSuiteInstalls.Files {
					alreadyInstalled := false
					for _, install := range installs {
						if install.String() == f.dst.String() {
							alreadyInstalled = true
							break
						}
					}
					if !alreadyInstalled {
						toInstall = append(toInstall, f)
					}
				}
			}
		}
	})

	for suite, suiteInstalls := range allTestSuiteInstalls {
		allTestSuiteInstalls[suite] = SortedUniquePaths(suiteInstalls)
	}

	hostSharedLibs := gatherHostSharedLibs(ctx, sharedLibRoots, sharedLibGraph)

	if !ctx.Config().KatiEnabled() {
		for _, testSuite := range SortedKeys(files) {
			testSuiteSymbolsZipFile := pathForTestSymbols(ctx, fmt.Sprintf("%s-symbols.zip", testSuite))
			testSuiteMergedMappingProtoFile := pathForTestSymbols(ctx, fmt.Sprintf("%s-symbols-mapping.textproto", testSuite))
			allTestModules := files[testSuite].testModules()
			BuildSymbolsZip(ctx, allTestModules, testSuiteSymbolsZipFile, testSuiteMergedMappingProtoFile)

			ctx.DistForGoalWithFilenameTag(testSuite, testSuiteSymbolsZipFile, testSuiteSymbolsZipFile.Base())
			ctx.DistForGoalWithFilenameTag(testSuite, testSuiteMergedMappingProtoFile, testSuiteMergedMappingProtoFile.Base())
		}
	}

	// https://source.corp.google.com/h/googleplex-android/platform/superproject/main/+/main:build/make/core/main.mk;l=674;drc=46bd04e115d34fd62b3167128854dfed95290eb0
	testInstalledSharedLibs := make(map[string]Paths)
	testInstalledSharedLibsDeduper := make(map[string]bool)
	for _, install := range toInstall {
		testInstalledSharedLibsDeduper[install.dst.String()] = true
	}
	for _, suite := range []string{"general-tests", "device-tests", "vts", "tvts", "art-host-tests", "host-unit-tests", "camera-hal-tests"} {
		var myTestCases WritablePath = hostOutTestCases
		switch suite {
		case "vts", "tvts":
			suiteInfo := ctx.Config().productVariables.CompatibilityTestcases[suite]
			outDir := suiteInfo.OutDir
			if outDir == "" {
				continue
			}
			rel, err := filepath.Rel(ctx.Config().OutDir(), outDir)
			if err != nil || strings.HasPrefix(rel, "..") {
				panic(fmt.Sprintf("Could not make COMPATIBILITY_TESTCASES_OUT_%s (%s) relative to the out dir (%s)", suite, suiteInfo.OutDir, ctx.Config().OutDir()))
			}
			myTestCases = PathForArbitraryOutput(ctx, rel)
		}

		for _, f := range hostSharedLibs[suite] {
			dir := filepath.Base(filepath.Dir(f.String()))
			out := joinWriteablePath(ctx, myTestCases, dir, filepath.Base(f.String()))
			if _, ok := testInstalledSharedLibsDeduper[out.String()]; !ok {
				ctx.Build(pctx, BuildParams{
					Rule:   Cp,
					Input:  f,
					Output: out,
				})
			}
			testInstalledSharedLibsDeduper[out.String()] = true
			testInstalledSharedLibs[suite] = append(testInstalledSharedLibs[suite], out)
		}
	}

	filePairSorter := func(arr []filePair) func(i, j int) bool {
		return func(i, j int) bool {
			c := strings.Compare(arr[i].dst.String(), arr[j].dst.String())
			if c < 0 {
				return true
			} else if c > 0 {
				return false
			}
			return arr[i].src.String() < arr[j].src.String()
		}
	}

	sort.Slice(toInstall, filePairSorter(toInstall))
	// Dedup, as multiple tests may install the same test data to the same folder
	toInstall = slices.Compact(toInstall)

	// Dedup the oneVariant files by only the dst locations, and ignore installs from other variants
	sort.Slice(oneVariantInstalls, filePairSorter(oneVariantInstalls))
	oneVariantInstalls = slices.CompactFunc(oneVariantInstalls, func(a, b filePair) bool {
		return a.dst.String() == b.dst.String()
	})

	for _, install := range toInstall {
		ctx.Build(pctx, BuildParams{
			Rule:   Cp,
			Input:  install.src,
			Output: install.dst,
		})
	}
	for _, install := range oneVariantInstalls {
		ctx.Build(pctx, BuildParams{
			Rule:   Cp,
			Input:  install.src,
			Output: install.dst,
		})
	}

	robolectricZip, robolectrictListZip := buildTestSuite(ctx, "robolectric-tests", files["robolectric-tests"])
	ctx.Phony("robolectric-tests", robolectricZip, robolectrictListZip)
	ctx.DistForGoal("robolectric-tests", robolectricZip, robolectrictListZip)

	ravenwoodZip, ravenwoodListZip := buildTestSuite(ctx, "ravenwood-tests", files["ravenwood-tests"])
	ctx.Phony("ravenwood-tests", ravenwoodZip, ravenwoodListZip)
	ctx.DistForGoal("ravenwood-tests", ravenwoodZip, ravenwoodListZip)

	packageTestSuite(ctx, allTestSuiteInstalls["performance-tests"], nil, performanceTests)
	packageTestSuite(ctx, allTestSuiteInstalls["device-platinum-tests"], nil, devicePlatinumTests)
	packageTestSuite(ctx, allTestSuiteInstalls["device-tests"], testInstalledSharedLibs["device-tests"], deviceTests)
}

// Get a mapping from testSuite -> list of host shared libraries, given:
// - sharedLibRoots: Mapping from testSuite -> androidMk name of all test modules in the suite
// - sharedLibGraph: Mapping from androidMk name of module -> androidMk names of its shared libs
//
// This mimics how make did it historically, which is filled with inaccuracies. Make didn't
// track variants and treated all variants as if they were merged into one big module. This means
// you can have a test that's only included in the "vts" test suite on the device variant, and
// only has a shared library on the host variant, and that shared library will still be included
// into the vts test suite.
func gatherHostSharedLibs(ctx SingletonContext, sharedLibRoots, sharedLibGraph map[string][]string) map[string]Paths {
	hostOutTestCases := pathForInstall(ctx, ctx.Config().BuildOSTarget.Os, ctx.Config().BuildOSTarget.Arch.ArchType, "testcases")
	hostOut := filepath.Dir(hostOutTestCases.String())

	for k, v := range sharedLibGraph {
		sharedLibGraph[k] = SortedUniqueStrings(v)
	}

	suiteToSharedLibModules := make(map[string]map[string]bool)
	for suite, modules := range sharedLibRoots {
		suiteToSharedLibModules[suite] = make(map[string]bool)
		var queue []string
		for _, root := range SortedUniqueStrings(modules) {
			queue = append(queue, sharedLibGraph[root]...)
		}
		for len(queue) > 0 {
			mod := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if suiteToSharedLibModules[suite][mod] {
				continue
			}
			suiteToSharedLibModules[suite][mod] = true
			queue = append(queue, sharedLibGraph[mod]...)
		}
	}

	hostSharedLibs := make(map[string]Paths)

	ctx.VisitAllModuleProxies(func(m ModuleProxy) {
		if makeName, ok := OtherModuleProvider(ctx, m, MakeNameInfoProvider); ok {
			commonInfo := OtherModuleProviderOrDefault(ctx, m, CommonModuleInfoProvider)
			if commonInfo.SkipInstall {
				return
			}
			installFilesProvider := OtherModuleProviderOrDefault(ctx, m, InstallFilesProvider)
			for suite, sharedLibModulesInSuite := range suiteToSharedLibModules {
				if sharedLibModulesInSuite[makeName.Name] {
					for _, f := range installFilesProvider.InstallFiles {
						if strings.HasSuffix(f.String(), ".so") && strings.HasPrefix(f.String(), hostOut) {
							hostSharedLibs[suite] = append(hostSharedLibs[suite], f)
						}
					}
				}
			}
		}
	})
	for suite, files := range hostSharedLibs {
		hostSharedLibs[suite] = SortedUniquePaths(files)
	}

	return hostSharedLibs
}

type suiteKind int

const (
	performanceTests suiteKind = iota
	deviceTests
	devicePlatinumTests
)

func (sk suiteKind) String() string {
	switch sk {
	case performanceTests:
		return "performance-tests"
	case deviceTests:
		return "device-tests"
	case devicePlatinumTests:
		return "device-platinum-tests"
	default:
		panic(fmt.Sprintf("Unrecognized suite kind %d for use in packageTestSuite", sk))
	}
}

func (sk suiteKind) buildHostSharedLibsZip() bool {
	switch sk {
	case devicePlatinumTests:
		return true
	}
	return false
}

func (sk suiteKind) includeHostSharedLibsInMainZip() bool {
	switch sk {
	case deviceTests:
		return true
	}
	return false
}

func buildTestSuite(ctx SingletonContext, suiteName string, files testModulesInstallsMap) (Path, Path) {
	var installedPaths Paths
	for _, module := range files.testModules() {
		installedPaths = append(installedPaths, files[module].Paths()...)
	}

	installedPaths = SortedUniquePaths(installedPaths)

	outputFile := pathForPackaging(ctx, suiteName+".zip")
	rule := NewRuleBuilder(pctx, ctx)
	rule.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", outputFile).
		FlagWithArg("-P ", "host/testcases").
		FlagWithArg("-C ", pathForTestCases(ctx).String()).
		FlagWithRspFileInputList("-r ", outputFile.ReplaceExtension(ctx, "rsp"), installedPaths).
		Flag("-sha256") // necessary to save cas_uploader's time

	testList := buildTestList(ctx, suiteName+"_list", installedPaths)
	testListZipOutputFile := pathForPackaging(ctx, suiteName+"_list.zip")

	rule.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", testListZipOutputFile).
		FlagWithArg("-C ", pathForPackaging(ctx).String()).
		FlagWithInput("-f ", testList).
		Flag("-sha256")

	rule.Build(strings.ReplaceAll(suiteName, "-", "_")+"_zip", suiteName+".zip")

	return outputFile, testListZipOutputFile
}

func buildTestList(ctx SingletonContext, listFile string, installedPaths Paths) Path {
	buf := &strings.Builder{}
	for _, p := range installedPaths {
		if p.Ext() != ".config" {
			continue
		}
		pc, err := toTestListPath(p.String(), pathForTestCases(ctx).String(), "host/testcases")
		if err != nil {
			ctx.Errorf("Failed to convert path: %s, %v", p.String(), err)
			continue
		}
		buf.WriteString(pc)
		buf.WriteString("\n")
	}
	outputFile := pathForPackaging(ctx, listFile)
	WriteFileRuleVerbatim(ctx, outputFile, buf.String())
	return outputFile
}

func toTestListPath(path, relativeRoot, prefix string) (string, error) {
	dest, err := filepath.Rel(relativeRoot, path)
	if err != nil {
		return "", err
	}
	return filepath.Join(prefix, dest), nil
}

func pathForPackaging(ctx PathContext, pathComponents ...string) OutputPath {
	pathComponents = append([]string{"packaging"}, pathComponents...)
	return PathForOutput(ctx, pathComponents...)
}

func pathForTestCases(ctx PathContext) InstallPath {
	return pathForInstall(ctx, ctx.Config().BuildOS, X86, "testcases")
}

func pathForTestSymbols(ctx PathContext, pathComponents ...string) InstallPath {
	return pathForInstall(ctx, ctx.Config().BuildOS, ctx.Config().BuildArch, "", pathComponents...)
}

func packageTestSuite(ctx SingletonContext, files Paths, sharedLibs Paths, sk suiteKind) {
	hostOutTestCases := pathForInstall(ctx, ctx.Config().BuildOSTarget.Os, ctx.Config().BuildOSTarget.Arch.ArchType, "testcases")
	targetOutTestCases := pathForInstall(ctx, ctx.Config().AndroidFirstDeviceTarget.Os, ctx.Config().AndroidFirstDeviceTarget.Arch.ArchType, "testcases")
	hostOut := filepath.Dir(hostOutTestCases.String())
	targetOut := filepath.Dir(targetOutTestCases.String())

	testsZip := pathForPackaging(ctx, sk.String()+".zip")
	testsListTxt := pathForPackaging(ctx, sk.String()+"_list.txt")
	testsListZip := pathForPackaging(ctx, sk.String()+"_list.zip")
	testsConfigsZip := pathForPackaging(ctx, sk.String()+"_configs.zip")
	testsHostSharedLibsZip := pathForPackaging(ctx, sk.String()+"_host-shared-libs.zip")
	var listLines []string

	// use intermediate files to hold the file inputs, to prevent argument list from being too long
	testsZipCmdHostFileInput := PathForIntermediates(ctx, sk.String()+"_host_list.txt")
	testsZipCmdTargetFileInput := PathForIntermediates(ctx, sk.String()+"_target_list.txt")
	var testsZipCmdHostFileInputContent, testsZipCmdTargetFileInputContent []string

	testsZipBuilder := NewRuleBuilder(pctx, ctx)
	testsZipCmd := testsZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-sha256").
		Flag("-d").
		FlagWithOutput("-o ", testsZip).
		FlagWithArg("-P ", "host").
		FlagWithArg("-C ", hostOut)

	testsConfigsZipBuilder := NewRuleBuilder(pctx, ctx)
	testsConfigsZipCmd := testsConfigsZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-d").
		FlagWithOutput("-o ", testsConfigsZip).
		FlagWithArg("-P ", "host").
		FlagWithArg("-C ", hostOut)

	for _, f := range files {
		if strings.HasPrefix(f.String(), hostOutTestCases.String()) {
			testsZipCmdHostFileInputContent = append(testsZipCmdHostFileInputContent, f.String())
			testsZipCmd.Implicit(f)

			if strings.HasSuffix(f.String(), ".config") {
				testsConfigsZipCmd.FlagWithInput("-f ", f)
				listLines = append(listLines, strings.Replace(f.String(), hostOut, "host", 1))
			}
		}
	}

	if sk.includeHostSharedLibsInMainZip() {
		for _, f := range sharedLibs {
			if strings.HasPrefix(f.String(), hostOutTestCases.String()) {
				testsZipCmdHostFileInputContent = append(testsZipCmdHostFileInputContent, f.String())
				testsZipCmd.Implicit(f)
			}
		}
	}

	WriteFileRule(ctx, testsZipCmdHostFileInput, strings.Join(testsZipCmdHostFileInputContent, " "))

	testsZipCmd.
		FlagWithInput("-l ", testsZipCmdHostFileInput).
		FlagWithArg("-P ", "target").
		FlagWithArg("-C ", targetOut)
	testsConfigsZipCmd.
		FlagWithArg("-P ", "target").
		FlagWithArg("-C ", targetOut)

	for _, f := range files {
		if strings.HasPrefix(f.String(), targetOutTestCases.String()) {
			testsZipCmdTargetFileInputContent = append(testsZipCmdTargetFileInputContent, f.String())
			testsZipCmd.Implicit(f)

			if strings.HasSuffix(f.String(), ".config") {
				testsConfigsZipCmd.FlagWithInput("-f ", f)
				listLines = append(listLines, strings.Replace(f.String(), targetOut, "target", 1))
			}
		}
	}

	WriteFileRule(ctx, testsZipCmdTargetFileInput, strings.Join(testsZipCmdTargetFileInputContent, " "))
	testsZipCmd.FlagWithInput("-l ", testsZipCmdTargetFileInput)

	testsZipBuilder.Build(sk.String(), "building "+sk.String()+" zip")
	testsConfigsZipBuilder.Build(sk.String()+"-configs", "building "+sk.String()+" configs zip")

	if sk.buildHostSharedLibsZip() {
		testsHostSharedLibsZipBuilder := NewRuleBuilder(pctx, ctx)
		testsHostSharedLibsZipCmd := testsHostSharedLibsZipBuilder.Command().
			BuiltTool("soong_zip").
			Flag("-d").
			FlagWithOutput("-o ", testsHostSharedLibsZip).
			FlagWithArg("-P ", "host").
			FlagWithArg("-C ", hostOut)

		for _, f := range sharedLibs {
			if strings.HasPrefix(f.String(), hostOutTestCases.String()) && strings.HasSuffix(f.String(), ".so") {
				testsHostSharedLibsZipCmd.FlagWithInput("-f ", f)
			}
		}

		testsHostSharedLibsZipBuilder.Build(sk.String()+"-host-shared-libs", "building "+sk.String()+"host shared libs")
	}

	WriteFileRule(ctx, testsListTxt, strings.Join(listLines, "\n"))

	testsListZipBuilder := NewRuleBuilder(pctx, ctx)
	testsListZipBuilder.Command().
		BuiltTool("soong_zip").
		Flag("-d").
		FlagWithOutput("-o ", testsListZip).
		FlagWithArg("-e ", sk.String()+"_list").
		FlagWithInput("-f ", testsListTxt)
	testsListZipBuilder.Build(sk.String()+"_list_zip", "building "+sk.String()+" list zip")

	ctx.Phony(sk.String(), testsZip)
	ctx.DistForGoal(sk.String(), testsZip, testsListZip, testsConfigsZip)
	if sk.buildHostSharedLibsZip() {
		ctx.DistForGoal(sk.String(), testsHostSharedLibsZip)
	}
	ctx.Phony("tests", PathForPhony(ctx, sk.String()))
}
