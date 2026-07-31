# go-winloader

**Note:** This library is still experimental. There are no guarantees of API stability, or runtime stability. Proceed with caution.

go-winloader is a library that implements the Windows module loader algorithm in pure Go. The actual Windows module loader, accessible via `LoadLibrary`, only supports loading modules from disk, which is sometimes undesirable. With go-winloader, you can load modules directly from memory without needing to write intermediate temporary files to disk.

This is a bit more versatile than linking directly to object files, since you do not need object files for this approach. As a downside, it is a purely Windows-only approach.

Note: Unlike native APIs, in place of processor register sized values go-winloader uses `uint64` instead of `uintptr` except when calling native host functions. This allows it to remain neutral to processor architecture at runtime, which in the future may allow for more esoteric use cases. (See TODO for more information on potential future features.)

## Example

```go
// Load a module off disk (for simplicity; you can pass in any byte slice.)
b, _ := ioutil.ReadFile("my.dll")
mod, err := winloader.LoadFromMemory(b)
if err != nil {
    log.Fatalln("error loading module:", err)
}

// Get a procedure.
addProc := mod.Proc("Add")
if proc == nil {
    log.Fatalln("module my.dll is missing required procedure Add")
}

// Call the procedure!
result, _, _ := addProc.Call(1, 2)
log.Printf("1 + 2 = %d", result)
```

## TODO

* WinSXS support

    * Some binaries have manifests requesting a specific version of a library.
      There is an undocumented library at sxs.dll. Since it's undocumented,
      it might be difficult to work out how to use it.
    
    * It'd be nice to have a reimplementation of the whole SXS algorithm, just
      for completeness sake. (It might even be useful to Wine.)

* Additional compatibility hacks

    * Because we are not Windows loader, Windows loader's internal structures
      do not update when we load binaries into the address space.

        * Because of this, our HINSTANCE value may not always work properly.

        * There's a hack that sends the process HINSTANCE instead.

        * There's a stub for a hack that would inject the library into the PEB
        loader data linked lists.

            * This might fail catastrophically or have worse consequences, but it
            would be interesting to explore.

        * Another useful hack would be one that can override calls to important
        Windows functions and implement their functionality for cases when our
        own false HINSTANCE is used. This could be done on a process-wide level
        (for best compatibility) or directly on the import table (usually enough,
        but tricky modules will bypass this.)

* Better support for loading executable images.

    * Right now, if you attempt to load an executable, it executes the
      entrypoint eagerly when it tries to send the DLL attach message.

* Threading support.

    * Perhaps have a helper function that can run a function in a new thread,
      automatically calling `DLL_THREAD_ATTACH`/`DLL_THREAD_DETACH` as needed
      on memory loaded modules.
    
    * While it may not be necessary for all libraries, it is likely necessary
      for libraries that have statically linked the MSVC runtime, and for
      libraries that use thread-local storage. Otherwise, calling functions on
      threads other than the initial one is likely to crash.

    * Even better: if we can find a place to hook new threads, this would be a
      nice hack to support.

* Versatility

    * Operating systems other than Windows:

        * Need to figure out how to make MSABI calls. Maybe CGo with msabi
          function pointers, or maybe we need to write out the asm by hand.

        * Emulator would need the ability to generate stub addresses that call
          back into Go code, so we can use them to handle imports and whatnot.
        
        * Would need a custom loader that lets you emulate calls to other
          libraries.

    * CPU emulation

        * Should be possible implement virtual machines with emulated CPUs.

        * Similar to the outside Windows case, we need a custom loader to
          emulate library calls. Although we *can* use the host system's
          libraries, we need to translate API calls so that they work
          correctly, like translating addresses and marshaling/unmarshaling
          data as necessary, and of course calling conventions are entirely
          different.

        * We should provide shims at least for running 32-bit binaries in
          64-bit processes using emulation. How much of the API to emulate
          would be hard to guage, but at least for documented APIs it should
          be relatively straightforward work.

    * Legacy formats

        * Currently go-winloader only supports loading PE32 or PE64 binaries.

        * It might be useful to someone to implement loading very old legacy
        binaries, like NE or LX.

    * Expose lower level APIs

        * In order to allow this library to be useful while it is still in
          heavy flux, a very small surface area is exposed today. As it
          matures, more of these internal libraries should be exposed as
          public API.
