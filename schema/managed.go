package schema

// Managed is what the reader of a managed file needs to know about its kind: the code a file this
// shulker can't read fails under, whether the file is checked against its schema when read, and
// whether it may omit its $schema line.
type Managed struct {
	Code           string
	Validates      bool
	MarkerOptional bool
	// EmptyIsZero reads a file holding nothing as the zero value, the way a missing one is read.
	EmptyIsZero bool
}

// ManagedFiles is the table of every file shulker writes and reads back. State and Local never
// fail a command: their readers warn with the error's cause and go on. Manifest is the one file
// people write by hand, so it may omit its marker and reads as the current version.
var ManagedFiles = map[Kind]Managed{
	Manifest: {Code: "manifest-invalid", Validates: true, MarkerOptional: true},
	Lock:     {Code: "lock-invalid", Validates: true},
	Instance: {Code: "instance-invalid", Validates: true},
	Registry: {Code: "registry-invalid", Validates: true, EmptyIsZero: true},
	Accounts: {Code: "accounts-invalid", Validates: true, EmptyIsZero: true},
	State:    {Code: "state-invalid"},
	Local:    {Code: "local-invalid"},
	Config:   {Code: "config-invalid"},
}
