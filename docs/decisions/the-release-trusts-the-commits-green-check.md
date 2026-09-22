# The release trusts the commit's green check

**The question.** The release workflow — the rehearsal and the tag run
alike — ran both tiers, the star, every language gate and the nested
modules on the commit it released, about twenty minutes, before the few
minutes of work only it can do: pack, publish, round trip. Every such
commit had already passed the same suite in `ci.yml`'s `full` job, on its
pull request and again on `main` after the squash; v0.6.0 was three
identical proofs of one SHA, and the reviewer approved a run that then
spent forty minutes before its first packaging step. May a release run
take CI's green check on the exact commit as the evidence RELEASING.md
asks for, or must every run re-prove the tree itself?

**Decided.** The release workflow asks the API for the commit's check runs
before its tiers. A `full` check that concluded green on the SHA, made by
a run of `.github/workflows/ci.yml` whose head is that SHA, proves the
commit: the tiers are skipped and the proving run is named in the log.
A commit with no such check — one no pull request carried, or one whose
run was refused — has its tiers run as before. What only the release
workflow does runs on every run regardless: the build, the packed smokes
and the toolchains they need, the publish, the round trip, the GitHub
release. Nothing decides on the strength of a check of another name,
another workflow's job that happens to be called `full`, or a run of
another commit. The operator's verdict A on
[#618](https://github.com/Bitspark/nightseam/issues/618).

**Why.** The alternative — every run re-proving the tree — bought one thing:
a repetition on a runner whose toolchain images may differ from the pull
request's by a day. That is the risk the packed smokes and the round trip
exist to catch, and both still run on every tag. What it cost was about
seventy minutes per release and the same again for every re-cut
candidate, all of it spent before the first step that could fail for a
reason CI had not already had. Evidence about a commit is a fact the API
reports for that commit, not a property of the run that reads it: the tag
run already reasons this way about the conformance matrix, which it reads
off the tag rather than running the suite for, because the standing a
release is cut at is the tree's and CI holds it fresh. Trusting the
rehearsal alone and re-proving the tag run was considered and set aside
as half the saving for the same argument. RELEASING.md's "the tag is cut
on a tree where they are green too" reads, since this page, "this commit
passed the tiers".

**Serves.** [Declarative](../goals/declarative.md) — what two of anything
would each hold a copy of is stated once as data all of them read. The
proof of a commit is stated once, by CI on the commit, and every run that
needs it reads it there rather than holding a copy of its own.

**Since.** 0.7.0, [#618](https://github.com/Bitspark/nightseam/issues/618)
(verdict A, 2026-09-22) and its lane
[#627](https://github.com/Bitspark/nightseam/issues/627), after
[#613](https://github.com/Bitspark/nightseam/issues/613) ordered the
workflow so that the question has a place to be asked.
