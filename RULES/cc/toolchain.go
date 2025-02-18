package cc

import (
	"fmt"
	"sort"
	"strings"

	"dbt-rules/RULES/core"
)

type Toolchain interface {
	Name() string
	ObjectFile(out core.OutPath, depfile core.OutPath, flags []string, includes []core.Path, src core.Path) string
	StaticLibrary(out core.Path, objs []core.Path) string
	SharedLibrary(out core.Path, objs []core.Path) string
	Binary(out core.Path, objs []core.Path, alwaysLinkLibs []core.Path, libs []core.Path, flags []string, script core.Path) string
	BlobObject(out core.OutPath, src core.Path) string
	RawBinary(out core.OutPath, elfSrc core.Path) string
	StdDeps() []Dep
	Script() core.Path
}

type Architecture string

const (
	ArchitectureX86_64  Architecture = "x86_64"
	ArchitectureAArch64 Architecture = "aarch64"
	ArchitectureUnknown Architecture = "Unknown"
)

// ToolchainArchitecture returns the architecture for the toolchain if known.
func ToolchainArchitecture(toolchain Toolchain) Architecture {
	if tca, ok := toolchain.(interface{ Architecture() Architecture }); ok {
		return tca.Architecture()
	}
	return ArchitectureUnknown
}

// ToolchainFreestanding reports whether the toolchain uses a
// freestanding environment (rather than a hosted one).
func ToolchainFreestanding(toolchain Toolchain) bool {
	if tcf, ok := toolchain.(interface{ Freestanding() bool }); ok {
		return tcf.Freestanding()
	}
	return false
}

// ToolchainAccepts reports whether the parent toolchain accepts
// libraries built with the child toolchain.
func ToolchainAccepts(parent, child Toolchain) bool {
	if parent.Name() == child.Name() {
		return true
	}
	if tca, ok := parent.(interface{ Accepts(tc Toolchain) bool }); ok {
		return tca.Accepts(child)
	}
	return false
}

// Toolchain represents a C++ toolchain.
type GccToolchain struct {
	Ar      core.GlobalPath
	As      core.GlobalPath
	Cc      core.GlobalPath
	Cpp     core.GlobalPath
	Cxx     core.GlobalPath
	Objcopy core.GlobalPath
	Ld      core.GlobalPath

	Includes     []core.Path
	Deps         []Dep
	LinkerScript core.Path

	CompilerFlags []string
	LinkerFlags   []string

	ToolchainName string
	ArchName      string
	TargetName    string

	// A list of toolchain names that libraries can be built with instead
	// of our toolchain. (A toolchain is always compatible with
	// itself -- there's no need to include oneself.)
	// For example, a testing toolchain should be able to accept low-level libraries
	// built with a non-test toolchain.
	CompatibleWith []string
}

func (gcc GccToolchain) Accepts(tc Toolchain) bool {
	for _, cw := range gcc.CompatibleWith {
		if cw == tc.Name() {
			return true
		}
	}
	return false
}

func (gcc GccToolchain) Architecture() Architecture {
	// TODO: remove i386, which appears to be a typo in libsupcxx
	if gcc.ArchName == "i386" || gcc.ArchName == "x86_64" {
		return ArchitectureX86_64
	}
	if gcc.ArchName == "aarch64" {
		return ArchitectureAArch64
	}
	return ArchitectureUnknown
}

func (gcc GccToolchain) Freestanding() bool {
	for _, lf := range gcc.LinkerFlags {
		if lf == "-ffreestanding" {
			return true
		}
	}
	return false
}

func (gcc GccToolchain) NewWithStdLib(includes []core.Path, deps []Dep, linkerScript core.Path, toolchainName string) GccToolchain {
	gcc.Includes = includes
	gcc.Deps = deps
	gcc.LinkerScript = linkerScript
	gcc.ToolchainName = toolchainName
	return gcc
}

// ObjectFile generates a compile command.
func (gcc GccToolchain) ObjectFile(out core.OutPath, depfile core.OutPath, flags []string, includes []core.Path, src core.Path) string {
	includesStr := strings.Builder{}
	for _, include := range includes {
		includesStr.WriteString(fmt.Sprintf("-I%q ", include))
	}
	for _, include := range gcc.Includes {
		includesStr.WriteString(fmt.Sprintf("-isystem %q ", include))
	}

	return fmt.Sprintf(
		"%q -pipe -c -o %q -MD -MF %q %s %s %q",
		gcc.Cxx,
		out,
		depfile,
		strings.Join(append(gcc.CompilerFlags, flags...), " "),
		includesStr.String(),
		src)
}

// StaticLibrary generates the command to build a static library.
func (gcc GccToolchain) StaticLibrary(out core.Path, objs []core.Path) string {
	// ar updates an existing archive. This can cause faulty builds in the case
	// where a symbol is defined in one file, that file is removed, and the
	// symbol is subsequently defined in a new file. That's because the old object file
	// can persist in the archive. See https://github.com/daedaleanai/dbt/issues/91
	// There is no option to ar to always force creation of a new archive; the "c"
	// modifier simply suppresses a warning if the archive doesn't already
	// exist. So instead we delete the target (out) if it already exists.
	return fmt.Sprintf(
		"rm %q 2>/dev/null ; %q rcs %q %s",
		out,
		gcc.Ar,
		out,
		joinQuoted(objs))
}

// SharedLibrary generates the command to build a shared library.
func (gcc GccToolchain) SharedLibrary(out core.Path, objs []core.Path) string {
	return fmt.Sprintf(
		"%q -pipe -shared -o %q %s",
		gcc.Cxx,
		out,
		joinQuoted(objs))
}

// Binary generates the command to build an executable.
func (gcc GccToolchain) Binary(out core.Path, objs []core.Path, alwaysLinkLibs []core.Path, libs []core.Path, flags []string, script core.Path) string {
	flags = append(gcc.LinkerFlags, flags...)
	if script != nil {
		flags = append(flags, "-T", fmt.Sprintf("%q", script))
	} else if gcc.LinkerScript != nil {
		flags = append(flags, "-T", fmt.Sprintf("%q", gcc.LinkerScript))
	}

	return fmt.Sprintf(
		"%q -pipe -o %q %s -Wl,-whole-archive %s -Wl,-no-whole-archive %s %s",
		gcc.Cxx,
		out,
		joinQuoted(objs),
		joinQuoted(alwaysLinkLibs),
		joinQuoted(libs),
		strings.Join(flags, " "))
}

// BlobObject creates an object file from any binary blob of data
func (gcc GccToolchain) BlobObject(out core.OutPath, src core.Path) string {
	return fmt.Sprintf(
		"%q -r -b binary -o %q %q",
		gcc.Ld,
		out,
		src)
}

// RawBinary strips ELF metadata to create a raw binary image
func (gcc GccToolchain) RawBinary(out core.OutPath, elfSrc core.Path) string {
	return fmt.Sprintf(
		"%q -O binary %q %q",
		gcc.Objcopy,
		elfSrc,
		out)
}

func (gcc GccToolchain) StdDeps() []Dep {
	return gcc.Deps
}

func (gcc GccToolchain) Script() core.Path {
	return gcc.LinkerScript
}
func (gcc GccToolchain) Name() string {
	return gcc.ToolchainName
}

func joinQuoted(paths []core.Path) string {
	b := strings.Builder{}
	for _, p := range paths {
		fmt.Fprintf(&b, "%q ", p)
	}
	return b.String()
}

var toolchains = make(map[string]Toolchain)

func RegisterToolchain(toolchain Toolchain) Toolchain {
	if _, found := toolchains[toolchain.Name()]; found {
		core.Fatal("A toolchain with name %s has already been registered", toolchain.Name())
	}
	toolchains[toolchain.Name()] = toolchain
	return toolchain
}

var NativeGcc = RegisterToolchain(GccToolchain{
	Ar:      core.NewGlobalPath("ar"),
	As:      core.NewGlobalPath("as"),
	Cc:      core.NewGlobalPath("gcc"),
	Cpp:     core.NewGlobalPath("gcc -E"),
	Cxx:     core.NewGlobalPath("g++"),
	Objcopy: core.NewGlobalPath("objcopy"),
	Ld:      core.NewGlobalPath("ld"),

	CompilerFlags: []string{"-std=c++14", "-O3", "-fdiagnostics-color=always"},
	LinkerFlags:   []string{"-fdiagnostics-color=always"},

	ToolchainName: "native-gcc",
	ArchName:      "x86_64", // TODO: don't hardcode this.
})

var defaultToolchainFlag = core.StringFlag{
	Name:        "cc-toolchain",
	Description: "Default toolchain to compile generic C/C++ targets",
	DefaultFn:   func() string { return NativeGcc.Name() },
}.Register()

// DefaultToolchain returns the default toolchain: either the native gcc
// toolchain, or the toolchain specified on the command-line with the cc-toolchain flag.
func DefaultToolchain() Toolchain {
	if toolchain, ok := toolchains[defaultToolchainFlag.Value()]; ok {
		return toolchain
	}
	var all []string
	for tc, _ := range toolchains {
		all = append(all, fmt.Sprintf("%q", tc))
	}
	sort.Strings(all)
	core.Fatal("No registered toolchain %q. Registered toolchains: %s", defaultToolchainFlag.Value(), strings.Join(all, ", "))
	return nil
}

func toolchainOrDefault(toolchain Toolchain) Toolchain {
	if toolchain == nil {
		return DefaultToolchain()
	}
	return toolchain
}
