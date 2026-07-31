package loader

// Module represents a loaded Windows module.
type Module interface {
	// Proc returns a procedure by symbol name. Returns nil if the symbol is
	// not found.
	Proc(name string) Proc

	// Ordinal returns a procedure by ordinal.
	Ordinal(ordinal uint64) Proc

	// Free closes the module and frees the memory. After this, GetProcAddress
	// will stop working and procedures will no longer function.
	Free() error
}

// Loader represents a named module loader implementation.
type Loader interface {
	Load(libname string) (Module, error)
}

// MemLoader represents a memory module loader implementation.
type MemLoader interface {
	LoadMem(module []byte) (Module, error)
}
