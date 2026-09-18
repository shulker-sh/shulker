//go:build !windows

package launcher

// The shim is a Windows executable, so nothing outside Windows ever reaches these: ShimMode is false
// there, and the sh script needs neither a raw command line nor a hidden child.

func commandLineTail() string { return "" }

func shimSpawn(dir, program, arguments string) int { return 0 }
