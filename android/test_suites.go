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
}

var TestSuiteInfoProvider = blueprint.NewProvider[TestSuiteInfo]()

type filePair struct {
	src Path
	dst WritablePath
}

type testSuiteInstallsInfo struct {
	Files              []filePair
	OneVariantInstalls []filePair
}

var testSuiteInstallsInfoProvider = blueprint.NewProvider[testSuiteInstallsInfo]()

type testModulesInstallsMap map[Module]InstallPaths

func (t testModulesInstallsMap) testModules() []Module {
	return slices.Collect(maps.Keys(t))
}

func (t *testSuiteFiles) GenerateBuildActions(ctx SingletonContext) {
	files := make(map[string]testModulesInstallsMap)
	allTestSuiteInstalls := make(map[string][]Path)
	var toInstall []filePair
	var oneVariantInstalls []filePair

	ctx.VisitAllModuleProxies(func(m ModuleProxy) {
		if tsm, ok := OtherModuleProvider(ctx, m, TestSuiteInfoProvider); ok {
			for _, testSuite := range tsm.TestSuites {
				if files[testSuite] == nil {
					files[testSuite] = make(testModulesInstallsMap)
				}
				files[testSuite][m] = append(files[testSuite][m],
					OtherModuleProviderOrDefault(ctx, m, InstallFilesProvider).InstallFiles...)
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

	if !ctx.Config().KatiEnabled() {
		for _, testSuite := range SortedKeys(files) {
			testSuiteSymbolsZipFile := pathForTestSymbols(ctx, fmt.Sprintf("%s-symbols.zip", testSuite))
			testSuiteMergedMappingProtoFile := pathForTestSymbols(ctx, fmt.Sprintf("%s-symbols-mapping.textproto", testSuite))
			allTestModules := files[testSuite].testModules()
			BuildSymbolsZip(ctx, allTestModules, testSuite, testSuiteSymbolsZipFile, testSuiteMergedMappingProtoFile)

			ctx.DistForGoalWithFilenameTag(testSuite, testSuiteSymbolsZipFile, testSuiteSymbolsZipFile.Base())
			ctx.DistForGoalWithFilenameTag(testSuite, testSuiteMergedMappingProtoFile, testSuiteMergedMappingProtoFile.Base())
		}
	}

	for suite, suiteInstalls := range allTestSuiteInstalls {
		allTestSuiteInstalls[suite] = SortedUniquePaths(suiteInstalls)
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

	packageTestSuite(ctx, allTestSuiteInstalls["performance-tests"], performanceTests)
	packageTestSuite(ctx, allTestSuiteInstalls["device-platinum-tests"], devicePlatinumTests)
}

type suiteKind int

const (
	performanceTests suiteKind = iota
	devicePlatinumTests
)

func (sk suiteKind) String() string {
	switch sk {
	case performanceTests:
		return "performance-tests"
	case devicePlatinumTests:
		return "device-platinum-tests"
	default:
		panic(fmt.Sprintf("Unrecognized suite kind %d for use in packageTestSuite", sk))
	}
	return ""
}

func (sk suiteKind) buildHostSharedLibsZip() bool {
	switch sk {
	case performanceTests:
		return false
	case devicePlatinumTests:
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

func packageTestSuite(ctx SingletonContext, files Paths, sk suiteKind) {
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

		for _, f := range files {
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
