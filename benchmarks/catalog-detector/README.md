# catalog-detector

Who owns "the skills listing is owed again"?

The listing is delivered once. Something has to decide when the model's copy
stopped matching the registry, and there are three candidates: rebuild the
projection and compare it, fingerprint the files' metadata, or trust that every
write announced itself.

```
go run ./benchmarks/catalog-detector -real "$PWD"
```

**Owed is measured, not declared.** The probe renders the block before and after
each transition; owed means those differ. That definition is what makes a
touched mtime and a body-only edit *not* owed — neither changes what the model
would be sent — and it is why the projection detector is the definition rather
than an estimate of it. What the arms measure is where the two cheaper
detectors diverge from it.

Cost is timed without the instrument attached: files and bytes are counted in a
separate walk, because timing a detector with the accounting inside it measures
the accounting. Numbers are macOS/APFS with a warm cache; the ratio between
detectors is what transfers, not the absolute.
