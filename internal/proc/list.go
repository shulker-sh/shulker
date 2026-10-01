package proc

// Process is one running process: the executable it was started from, as the system names it, and
// its whole command line where the system gives one.
type Process struct {
	Exe     string
	Command string
}
