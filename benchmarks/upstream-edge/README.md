# upstream-edge

A one-task suite for measuring what a fleet `depends_on` edge is worth when it
carries the dependency's answer, against the `-ablate upstream` arm where the
same edge only orders its endpoints.

It lives outside `benchmarks/e2e` on purpose. The prompt fixes the graph shape,
which the shared corpus deliberately never does: this measures the value of the
payload *given* an ordered pair, not how often a model reaches for one. Mixing a
shape-instructed task into the shared corpus would change what that corpus
measures.

```sh
go run ./cmd/e2ebench -suite benchmarks/upstream-edge -bin <reasonix> -json control.json
go run ./cmd/e2ebench -suite benchmarks/upstream-edge -bin <reasonix> -ablate upstream -json ablated.json
```

The downstream item needs one fact the upstream item established. With the edge
carrying it, that item has the fact on its opening turn. Without it, the item
must find the fact again in a tree seeded with three plausible wrong numbers.
