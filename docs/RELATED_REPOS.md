# Related repositories

An index of the repositories that sit beside Nightseam — consumers of its
seam, sources of the patterns it draws on, or neighbours in the same
workspace. One row per repository; one page per repository under
[repos/](repos/) with what it is, how it is laid out and how it relates to
this one.

Paths are local checkouts on the machine this index was written on; the
remote is the durable address. Add a row and a page for each new repository.

| Repository | Local checkout | Remote | Page | Models survey |
|---|---|---|---|---|
| aifunc3 (aifunc-mono) | `C:\Users\julia\Development\aifunc3` | https://github.com/Bitspark/aifunc-mono | [repos/aifunc3.md](repos/aifunc3.md) | [repos/aifunc3-models.md](repos/aifunc3-models.md) |
| bitmachine | `C:\Development\bitspark\bitmachine` | https://gitlab.bitspark.com/bitmachine/bitmachine | [repos/bitmachine.md](repos/bitmachine.md) | [repos/bitmachine-models.md](repos/bitmachine-models.md) |
| plexis | `C:\Development\bitspark\systems\plexis` | https://gitlab.bitspark.com/bitmachine/bitmachine (branch `plexis-main`) | [repos/plexis.md](repos/plexis.md) | [repos/plexis-models.md](repos/plexis-models.md) |

The *page* says what a repository is and how it is laid out. The *models
survey* records everything in it that does what Nightseam does — type
models, generators, wire profiles, conformance, layering — with paths into
the source and a comparison against Nightseam's contract model.
[RELATED_MODELS.md](RELATED_MODELS.md) draws the three surveys together
by concept, with a matrix and the list of what Nightseam could take.

## Notes

- **plexis is not a separate remote.** The `systems\plexis` checkout is a
  second clone of the bitmachine repository on the long-lived `plexis-main`
  branch, with the Plexis runtime workspace at `apps/plexis/`. See
  [repos/plexis.md](repos/plexis.md) for how it diverges from `main`.
- **aifunc3 has older siblings.** `C:\Users\julia\Development\aifunc`
  (`gitlab.bitspark.com/bitspark/aifunc-mono`, plus a `github` remote to
  `Bitspark/aifuncref`) and `...\aifunc2`
  (`gitlab.bitspark.com/Bitspark/aifunc2`) are earlier generations; `aifunc3`
  is the current one. Further copies live under `C:\Development\aifunc` and
  `C:\Development\aifunc2`.
- Only aifunc3 is on GitHub; bitmachine and plexis are on the Bitspark GitLab.
