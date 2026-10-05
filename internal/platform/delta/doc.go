// Package delta updates an installed tree by the chunks it lacks rather than by
// a whole installer. A release is described by a signed Index: every file's
// path, size and SHA-256, and the content-defined chunks it is made of. Chunks
// are stored once, compressed and named by the SHA-256 of their plain bytes, so
// a chunk shared by any two releases is uploaded and downloaded once.
//
// An update reads the running install with the same chunker, which is what
// makes the chunks it already has findable: content-defined boundaries move
// with an insertion rather than shifting every chunk after it. What is missing
// is fetched, each chunk checked against its name, and the new tree is written
// to a staging directory where every file is checked against the Index before
// anything is handed over to replace the install.
//
// The package decides nothing about signatures, transport or where the staged
// tree goes. Verifying the Index's signature is the caller's; so is swapping
// the staged tree in, which on Windows has to happen after the running app
// exits. What the package guarantees is narrower and total: a staged tree it
// reports complete is byte-for-byte the tree the Index describes.
package delta
