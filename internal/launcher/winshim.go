package launcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
)

// The Windows shim is `.shulker\javaw.exe`, a GUI-subsystem executable: a console-subsystem one opens
// a terminal window for the life of the game. Two routes produce it, and both read the same sidecar
// and honour the same contract as the sh script — hand the launch to the recorded shulker, fall back
// to the recorded Java, pass the child's exit code on, and say why on stderr where there is nothing
// to run at all rather than closing on a player with no window and no message.
//
// The preferred route compiles shimCS with the C# compiler Windows carries as an OS component, which
// costs 4 KB. Where that compiler is missing, shulker copies itself and patches one byte, which costs
// 11 MB, so reconcile rewrites it only when it differs from the running binary.

// shimMarkerPrefix names the compiled shim in its own assembly metadata. Reconcile recompiles when
// the marker it finds isn't the one this shulker would write.
const shimMarkerPrefix = "shulker-shim-"

// shimOldSuffix names the shim that was in the way of a rewrite, the way `self update` names the
// shulker it replaced.
const shimOldSuffix = ".old"

// cscDir is fixed across the whole 4.x line: the directory is named for the 4.0 build and never
// changes, which is why no version discovery is needed.
const cscDir = "v4.0.30319"

// shimCS is the shim's whole source, compiled at link time rather than shipped, so no unsigned binary
// lives in the repo. It knows nothing of shulker's model: it reads two paths and starts one process.
// It is the third copy of the shim's contract, and holds what the sh script (shimSh) and the Go
// fallback (runShim) hold: a change to the argument shape, the sidecar format or the no-Java line
// belongs in all three.
//
// It never tokenizes the game's argv. C# hands Main an already-parsed array, and .NET Framework has
// no way to re-quote one correctly, so the raw tail of the command line goes to the child untouched:
// the shim can neither corrupt an argument nor leak one, and that argv carries the session token.
const shimCS = `using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;

[assembly: AssemblyInformationalVersion("@MARKER@")]

class ShulkerShim
{
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)]
    static extern IntPtr GetCommandLineW();

    static int Main()
    {
        string exe = Assembly.GetExecutingAssembly().Location;
        string dir = Path.GetFullPath(Path.Combine(Path.GetDirectoryName(exe), ".."));
        string shulker = "", java = "";
        string sidecar = Path.ChangeExtension(exe, ".paths");
        string[] paths = Read(sidecar);
        if (paths.Length > 0) shulker = paths[0].Trim();
        if (paths.Length > 1) java = paths[1].Trim();
        string tail = Tail(Marshal.PtrToStringUni(GetCommandLineW()));
        string program = java, arguments = tail;
        if (shulker.Length > 0 && File.Exists(shulker))
        {
            program = shulker;
            arguments = "hook wrap -C \"" + dir + "\" --";
            if (tail.Length > 0) arguments += " " + tail;
        }
        if (program.Length == 0)
        {
            Console.Error.WriteLine(sidecar + @NOJAVA@);
            return @NOTHING@;
        }
        ProcessStartInfo start = new ProcessStartInfo(program, arguments);
        start.UseShellExecute = false;
        start.CreateNoWindow = true;
        start.WorkingDirectory = dir;
        try
        {
            using (Process child = Process.Start(start))
            {
                child.WaitForExit();
                return child.ExitCode;
            }
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("can't run " + program + ", so the game didn't start: " + e.Message);
            return @NOTHING@;
        }
    }

    // A sidecar that can't be read records no paths, which is the same to the shim as one that
    // records none.
    static string[] Read(string sidecar)
    {
        try { return File.ReadAllLines(sidecar); }
        catch (Exception) { return new string[0]; }
    }

    static string Tail(string line)
    {
        int i = 0;
        if (line.Length > 0 && line[0] == '"')
        {
            i = line.IndexOf('"', 1);
            i = i < 0 ? line.Length : i + 1;
        }
        else
        {
            while (i < line.Length && line[i] != ' ' && line[i] != '\t') i++;
        }
        return line.Substring(i).TrimStart(' ', '\t');
    }
}
`

// shimSource is the C# with everything but its own marker filled in. The line and the status this
// route leaves when there is nothing to run are the Go constants, so that copy of the shim's contract
// cannot drift from the one runShim holds.
func shimSource() string {
	return strings.NewReplacer(
		"@NOJAVA@", csLiteral(shimNoJavaTail),
		"@NOTHING@", strconv.Itoa(shimExitNothingToRun),
	).Replace(shimCS)
}

func csLiteral(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// shimMarker is derived from the source itself, not from shulker's version, so a release that leaves
// the shim alone doesn't make every linked instance recompile. The sidecar format and the line the
// shim leaves behind both live in that source, so a change to either moves the marker with it.
func shimMarker() string { return markerFor(shimSource()) }

func markerFor(source string) string {
	sum := sha256.Sum256([]byte(source))
	return shimMarkerPrefix + hex.EncodeToString(sum[:4])
}

// shimSidecarPath is the file beside the shim holding the two paths it needs: two lines, UTF-8, no
// escaping, since both are absolute and a Windows path can hold no newline.
func shimSidecarPath(exe string) string {
	return strings.TrimSuffix(exe, filepath.Ext(exe)) + ".paths"
}

func shimSidecar(s Shim) []byte {
	return []byte(s.Shulker + "\r\n" + s.Java + "\r\n")
}

// CscPath is the C# compiler Windows ships as part of the runtime, 64-bit first. Empty means this
// machine takes the self-copy route.
func CscPath() string {
	return cscPath(os.Getenv("WINDIR"), fileExists)
}

func cscPath(windir string, exists func(string) bool) string {
	if windir == "" {
		return ""
	}
	for _, framework := range []string{"Framework64", "Framework"} {
		path := filepath.Join(windir, "Microsoft.NET", framework, cscDir, "csc.exe")
		if exists(path) {
			return path
		}
	}
	return ""
}

// shimBuild is what the two routes need from the machine: the compiler where there is one, and the
// bytes of the binary the fallback copies.
type shimBuild struct {
	csc  string
	self func() ([]byte, error)
}

func machineShimBuild() shimBuild {
	return shimBuild{csc: CscPath(), self: func() ([]byte, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return os.ReadFile(exe)
	}}
}

func writeWindowsShim(s Shim, b shimBuild) error {
	exe := shimPath(s.Dir, "windows")
	if err := writeShimIfChanged(shimSidecarPath(exe), shimSidecar(s)); err != nil {
		return err
	}
	return writeShimExe(exe, b)
}

// writeShimExe leaves a compiled shim that carries the current marker alone, since csc output isn't
// byte-deterministic and there is nothing stable to diff against. A compile that can't run, exits
// non-zero, or writes nothing falls through to the copy silently: both routes behave alike under the
// shim's contract, so which one ran is not the player's business.
func writeShimExe(exe string, b shimBuild) error {
	marker := shimMarker()
	if data, err := os.ReadFile(exe); err == nil && shimHasMarker(data, marker) {
		return nil
	}
	if b.csc != "" {
		if compiled, err := compileShim(b.csc, marker); err == nil {
			return replaceShimExe(exe, compiled)
		}
	}
	return writeShimCopy(exe, b)
}

// writeShimCopy is the fallback: shulker's own binary with the PE subsystem byte changed, which is
// the only difference a -H=windowsgui build makes to the code. It costs 11 MB, so it is written only
// when what is there isn't already it.
func writeShimCopy(exe string, b shimBuild) error {
	self, err := b.self()
	if err != nil {
		return err
	}
	patched, err := patchSubsystem(self)
	if err != nil {
		return err
	}
	if fileHolds(exe, patched) {
		return nil
	}
	return replaceShimExe(exe, patched)
}

// replaceShimExe puts a new executable where the shim goes. Windows won't overwrite a running exe, and
// the shim is running whenever the game is, since the pre-launch sync happens inside it — but it will
// rename one, which is how `self update` replaces shulker itself. The copy moved aside goes on the
// next write, once nothing is running from it.
func replaceShimExe(path string, data []byte) error {
	old := path + shimOldSuffix
	os.Remove(old)
	err := fsutil.Write(path, data)
	if err == nil {
		return nil
	}
	if os.Rename(path, old) != nil {
		return err
	}
	return fsutil.Write(path, data)
}

func writeShimIfChanged(path string, want []byte) error {
	if fileHolds(path, want) {
		return nil
	}
	return fsutil.Write(path, want)
}

func fileHolds(path string, want []byte) bool {
	have, err := os.ReadFile(path)
	return err == nil && bytes.Equal(have, want)
}

// shimHasMarker looks for the marker in both encodings a .NET assembly can hold a string in: an
// attribute argument is UTF-8, and a string literal in the same file would be UTF-16.
func shimHasMarker(data []byte, marker string) bool {
	if bytes.Contains(data, []byte(marker)) {
		return true
	}
	wide := make([]byte, 0, len(marker)*2)
	for _, r := range []byte(marker) {
		wide = append(wide, r, 0)
	}
	return bytes.Contains(data, wide)
}

func compileShim(csc, marker string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "shulker-shim")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "shim.cs")
	if err := os.WriteFile(source, []byte(strings.ReplaceAll(shimSource(), "@MARKER@", marker)), 0o644); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "javaw.exe")
	// anycpu rather than csc's 32-bit-preferred default, so one IL shim covers x64 and ARM64.
	cmd := exec.Command(csc, "/nologo", "/target:winexe", "/platform:anycpu", "/optimize+", "/out:"+out, source)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(csc), err, bytes.TrimSpace(output))
	}
	compiled, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	if !shimHasMarker(compiled, marker) {
		return nil, errors.New("the compiled shim carries no marker")
	}
	return compiled, nil
}

// PE offsets: the DOS header holds the PE header's own offset at 0x3c, and the optional header, which
// starts after the 4-byte signature and the 20-byte COFF header, holds Subsystem 68 bytes in.
const (
	peOffsetAt       = 0x3c
	peSubsystemAt    = 4 + 20 + 68
	peSubsystemCUI   = 3
	peSubsystemGUI   = 2
	peHeaderMinBytes = peOffsetAt + 4
)

// patchSubsystem turns a console-subsystem executable into a GUI one. It is one byte: building the
// same Go program with and without -H=windowsgui differs in no executable code at all.
func patchSubsystem(pe []byte) ([]byte, error) {
	if len(pe) < peHeaderMinBytes || pe[0] != 'M' || pe[1] != 'Z' {
		return nil, errors.New("not a Windows executable")
	}
	header := int(binary.LittleEndian.Uint32(pe[peOffsetAt:]))
	at := header + peSubsystemAt
	if header < 0 || at >= len(pe) || string(pe[header:header+4]) != "PE\x00\x00" {
		return nil, errors.New("not a Windows executable")
	}
	switch pe[at] {
	case peSubsystemGUI:
		return pe, nil
	case peSubsystemCUI:
		patched := bytes.Clone(pe)
		patched[at] = peSubsystemGUI
		return patched, nil
	}
	return nil, fmt.Errorf("unexpected PE subsystem %d", pe[at])
}
