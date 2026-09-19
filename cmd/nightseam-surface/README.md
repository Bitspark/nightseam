# Compare generated Go surfaces

From the repository root:

```sh
go run ./cmd/nightseam-surface /path/to/regenerated-checkout /path/to/previous-checkout
```

This compares the exported declarations of every `api/go/**/*_generated.go`
file shared by the two checkouts. Implementation changes and private names
do not affect the comparison. A new file is reported as absent from the
previous checkout; removed files are outside this comparison.

Changed surfaces print both sets of declarations and exit nonzero. Invalid
source, unreadable files, missing arguments, and checkouts with no shared
generated Go files also fail. The command uses the same declaration reader
as the generator's surface goldens; its fixture tests run in the fast tier.
