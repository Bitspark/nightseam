# Advice for consult
**Research doc:** doc_ab403efb-8e43-47e7-bfc0-ca2667a6ffe1.md
**Document:** doc_ab403efb-8e43-47e7-bfc0-ca2667a6ffe1
**Advisor:** ChatGPT GPT-6 Pro
**Session:** nsess_d2fd9eca-81cd-4b55-b809-6da2a17df7ca
**Run:** run_d9f8ce66-b2c7-4ee0-9f80-f3ff08583d45
**Chat:** https://chatgpt.com/c/6ab286ab-8c74-83ed-be1b-dd9507fc0142
**Verify:** nightfall consult verify run_d9f8ce66-b2c7-4ee0-9f80-f3ff08583d45 --output <this file>

# Response to research 0002

**The decomposition is substantially right, but it needs two distinctions made operational: a change is admitted differently from a release, and a test specification is not the evidence that an implementation satisfies it.**

I would retain the four per-language facts, keep Go and TypeScript as permanent twins, permit additional birth implementations selected by the assumption a change needs to challenge, and retain lockstep releases for the intended release-required cohort.

I would change two parts of your stated leaning:

* **Do not use “new since the last tag” as sufficient permission for a merge-time breach.** Admit specific gaps introduced by a specific change, and prevent already-established coverage from regressing.
* **Do not make registry publication a prerequisite for refusing a release.** A deliberately chosen proving implementation can justify a veto before it has registry consumers. What it should demonstrate is reproducible consumption outside its test harness, not necessarily an upload.

The missing record is not a new *feature catalogue*. It is the relationship between an existing **lane, its changed obligations, its birth implementations, and any remaining gaps**.

The repository-specific findings below concern the supplied excerpts; I have not independently inspected the checkout.

## 1. The decomposition: keep the separation, correct the units

### A scenario specifies evidence; an execution supplies it

Your first noun table calls the scenario the unit of evidence. Strictly, the JSON file specifies an experiment. Evidence is a result from executing a particular revision, with particular implementations, cases, orientations and runner behavior. Your distinction between 79 files and 314 expanded runs already demonstrates why those are different units. 

I would refine the four nouns as follows:

| Noun               | Recommended meaning                                                                                 |
| ------------------ | --------------------------------------------------------------------------------------------------- |
| **Scenario**       | A versioned, executable specification of behavior.                                                  |
| **Profile**        | A named promise over the scenarios assigned to it at a particular definition revision.              |
| **Implementation** | The subject that owes the promise; currently identified by language, but not inherently a language. |
| **Release**        | A certified source-and-definition snapshot, followed by its distribution operations.                |

An execution report is the evidence connecting these things. It need not become a fifth governing concept, but it must exist as more than aggregate counts.

A trustworthy report should identify the tested source, suite and case-table revisions, runner, relevant toolchains, testee variants and expected executions. Every expected execution needs an outcome or an explicit explanation of why evidence is missing. The matrix remains the human-readable projection.

This matters immediately: **the shown `Verdict` function accepts a missing required cell by falling through to `"ok"`**. Other code might prevent that input, but this evaluator does not establish completeness itself. Checking for recorded failures is weaker than checking that all required evidence exists and passed. 

Similarly, the committed matrix should be a labelled presentation snapshot, not the authority for a release unless its provenance matches the release candidate. The stale serial-rule results demonstrate that matching the README to the matrix proves consistency between two reports, not consistency with the code. 

### Profiles are a sound promise unit, provided membership changes are governed

I would keep profiles rather than move to per-scenario promises. But profile membership is contractual, not merely organizational.

With your placement rule, adding `observer` to a scenario’s `needs` can transfer an existing obligation from `core` to `observability`. A core-only implementation could consequently lose an obligation without its promised set changing. That possibility follows directly from the stated placement rule. 

Therefore, a definition-changing lane should expose a machine-generated **obligation diff**: additions, revisions, removals and profile transfers, including which implementations gain or lose requirements. Transfers and removals need the same explicit authority as other changes to promises.

For example, adding observation assertions to a core exchange should normally create an additional observability scenario while preserving the core assertion. Moving the original scenario is appropriate only when removing that core obligation is actually intended.

There is also a dependency distinction worth preserving:

> Needing a live runtime to execute a generator scenario is not, by itself, proof that promising `generator` entails promising every scenario in `live`.

The first is an implementation or execution dependency; the second is a contractual dependency. Your settled finding—that the current generator suite presupposes tunnel and live runtimes—stands. It does not automatically establish entailment between entire profile promises. Any such closure should be an explicit policy choice, especially before bitlink inherits it. 

### “Language” is a convenient identifier, not the fundamental subject

The obligation belongs to an implementation. Language happens to identify it uniquely today.

You already have more than one testee for a language when generated code is involved. Bitlink adds targets that are not protocol runtimes at all. Those are reasons to use stable implementation or target identifiers internally, without prematurely adding a large taxonomy of platforms and variants.  

The same distinction applies to the four facts. Keep `promises`, disposition, permanent-twin membership and reference status separate. Their current coincidence in Go and TypeScript is not a reason to derive one from another.

### You need a change record, not a feature catalogue

“Nothing reads feature” stops being a sufficient argument once merge admission must distinguish intentional incompleteness from regression. Something must identify what changed and which gaps its admission authorizes.

Fortunately, **the lane already exists**.

Extend its machine-readable contract to identify the affected scenario obligations, selected birth implementations, governing decision where needed, and any admitted implementation gaps. Human-facing issues can describe and coordinate that work, but identifiers and revisions should resolve against the tree. Your catch-up issues’ stale case-table references are a concrete reason not to make mutable issue prose the authority. 

This does not require a permanent product-feature ontology, a release-lag clock, or hand-maintained duplication of every scenario. It requires a durable account of an accepted change.

### What must be machine-readable

Anything that changes **which work is required, whether a merge is accepted, or whether a release is permitted** must be machine-readable: all four policy facts; profile placement and explicit dependency rules; lane birth requirements; admitted gaps; evidence identity and completeness; and the release’s distribution inventory.

Goals, rationale, idiom discussions and the explanation of a decision can remain prose. Their enforcement-relevant consequences cannot.

I would also sharpen “the matrix reads no policy” to:

> Evidence collection does not depend on what an implementation promises. Profile grouping reads the scenario catalogue; merge and release verdicts interpret the evidence separately.

Since placement currently lives in the policy file, literal file independence is unnecessary. Semantic independence is what matters. 

## 2. Twins: retain a permanent floor and select additional witnesses

**My ranking is: B-with-A-as-the-floor first; a deliberately diverse fixed cohort second; the fixed pair alone third. B as a replacement for TypeScript, and D, are inconsistent with the supplied standing commitments.**

The operator’s ruling makes Go–TypeScript parity independent of consumer demand and not a per-feature choice. I read that as requiring both, not as prohibiting an additional implementation. Thus your proposed floor is a reasonable reading; replacing TypeScript with another second language is not. 

I would preserve the terminology carefully:

* A **permanent twin** is required for every definition-changing feature lane.
* An **additional birth witness** is required for the obligations identified in a particular lane.

Then the lane’s birth cohort is the permanent twins plus its additional witnesses. This avoids changing the meaning of a per-language `twin` flag every time a lane is cut.

### Select by the assumption being challenged

The selection question should be:

> What assumption could Go and TypeScript share that would make this change look sound when it is not?

That is more useful than “which language belongs to this feature’s cluster?”

For a family-parameter change, the useful additional implementation is the one least helped by the assumptions being made about rendering or witnessing the instantiation law. For a callable-lifetime change, ownership and purity may require different witnesses. For request ordering, the relevant stress is the actual concurrency and emission mechanism—not simply whether the language is statically typed.

Treat the cluster table as a hypothesis-generating guide, not an exhaustive classification. The document itself labels it as your reading; its observed Swift string-identity problem already shows why a language cannot be reduced to its assigned cluster. 

A rotation would distribute participation, but it would not ensure that the important assumption gets challenged. Conversely, naming a third language should not become optional ceremony: where a lane introduces a materially untested host-language assumption, an appropriate additional witness should be required.

Selection should occur when the lane is cut. Established mappings can be applied mechanically by the fleet; genuinely new semantic questions go to the operator. That preserves autonomous merging without delegating unresolved policy to whichever agent happens to own the lane.

### Do not confuse a useful witness with a complete port

Today none of the six ports has the upper runtime components or a generator target. Naming one as the birth witness for live or generator work therefore cannot magically make that proof available. 

Where prerequisites are missing, land the prerequisite implementation first, or explicitly accept that building it is part of the feature’s critical path. A handwritten demonstration or a testee stub is not evidence that the real generated API and runtime can realize the design.

The additional witness need not become a permanent twin or promise every profile. It must, however, pass the selected obligations through the real implementation path. Those obligations are acceptance requirements for this change, not a new per-scenario consumer support tier.

### What the precedents actually establish

Apache Arrow is particularly close: format changes require at least two reference implementations and associated integration tests. Its policy distinguishes independent implementations from wrappers, explicitly excluding Python as a second implementation when it merely wraps C++. It does not require every language to implement the change before the format can advance. ([arrow.apache.org][1])

WebAssembly separately identifies the specification, tests, reference interpreter, engines and toolchains. Its standardization phase requires two or more Web VMs passing the tests, where applicable, alongside the updated reference interpreter and other requirements. TC39 likewise requires two compatible implementations passing acceptance tests for Stage 4. Neither process collapses “multiple implementations validate the definition” into “all implementations ship together.” ([GitHub][2])

These are strong precedents for retaining a birth cohort at eight implementations. They are **not comparative evidence that an idiom-selected third implementation produces better definitions than a fixed diverse cohort**. That recommendation follows from Nightseam’s goals and observed divergences, not from a demonstrated universal optimum.

## 3. Merge versus release: admit specific debt and preserve established coverage

**Recommend a strengthened B, with C as its resulting state—not as a competing mechanism.**

Once an incomplete feature merges and release-required implementations remain behind, main is necessarily untaggable. The question is which incompleteness may enter main, and how subsequent merges are prevented from making it worse.

### Why “new since the last tag” is insufficient

Your proposed tolerance needs to distinguish at least three things that file age does not distinguish. 

First, an existing scenario can gain new cases or change meaning through a case table, driver operation or expected result. A filename’s creation date misses that.

Second, a newly added scenario can expose a defect in an old rule. New evidence does not necessarily mean a new obligation.

Third, a port can catch up and then regress before the next tag. Under a blanket last-tag exemption, the regression remains tolerated because the scenario is still “new.”

The third case is decisive. Suppose Rust catches up to the serial rule, and a later lane breaks its implementation before the next release. A merge gate should not treat that as the original, intentionally admitted catch-up gap.

### The merge rule I would adopt

For release-required non-twins, the operative invariant should be:

> Every breach in the candidate is either an outstanding, explicitly admitted gap from the tested base, or a specific gap admitted by this lane’s authorized contract change.

Once an obligation passes, its admission closes. A subsequent failure is a regression, not a reopening of the original permission.

Operationally, I would require four things:

1. **A valid change contract and complete evidence.** The changed obligations, birth requirements and proposed gaps resolve against the candidate, and the report identifies the candidate actually being assessed.
2. **The birth cohort passes.** Permanent twins satisfy their full promises; additional witnesses satisfy the lane’s selected obligations and necessary execution path.
3. **Established coverage is preserved.** A previously passing, unchanged promised obligation in a release-required implementation cannot become non-passing.
4. **Every remaining breach is accounted for.** Existing admitted gaps may remain; new gaps must belong to the lane’s explicitly authorized change. An arbitrary unrelated failure cannot acquire permission merely by appearing in the same pull request.

Marked implementations retain the existing mark semantics. This rule should not quietly promote them into release-required implementations.

A test that discovers an old defect can still be merged with an explicit operator-approved exception where appropriate. But it should not acquire that permission automatically from the date the test was written.

### Keep outcomes truthful, including skips

I would **not** require rewriting a skipped execution into a fictional test failure.

A skip is an execution outcome; breach is its contractual interpretation. Preserve both:

```text
execution:       skipped — required operation unavailable
promise:         breached
merge admission: allowed — identified catch-up gap
release:         blocked
```

The green result means that the **merge admission rule** passed. It does not mean the implementation conforms. The matrix cell stays red, and the release-readiness result stays blocking.

This honors the ban on green skips more faithfully than changing the raw result’s spelling. The requirement is that missing proof cannot be presented as conformance—not that every non-execution must masquerade as an executed assertion failure. Your vocabulary already treats skips in promised profiles as red. 

Known-gap annotations can supply the record format: implementation, obligation revision, reason, issue and expiry. But distinguish their effects. A merge admission may permit integration; it must not waive a refusing implementation’s release obligation. Expiry should force reconsideration, not silently erase the gap.

### This is more than changing the cleanup callback

The cleanup’s use of `Matrix.Blocking` is the visible coupling, but the document identifies other unconditional failures: missing toolchains, startup failures and promised-capability checks. Unit suites and smokes also participate in `full`.  

A newly owed capability must be representable as missing evidence for the relevant obligations, rather than failing before the admission rule can inspect it. Conversely, an unexpectedly broken testee or unrelated unit-test failure must not become an unlimited “port catching up” exception.

Separate collection from evaluation throughout the execution path, not merely at the final verdict.

### Validate against the main that will actually receive the change

Your current protection deliberately allows merges without requiring an up-to-date branch. That matters more once acceptance depends on outstanding gaps and previously established coverage. Two lanes can each be acceptable against an older base while their combined result is unacceptable. 

The admission decision therefore needs validation against the actual integration candidate. A merge queue, where available, is one implementation: GitHub documents that it tests the change against the latest target branch and changes ahead of it in the queue. An equivalent final integration check with a protected expected-base condition would serve the same purpose. This serializes final admission, not implementation work or human review. ([GitHub Docs][3])

Use fresh evidence for that candidate. Do not compare failure counts: exchanging one failing obligation for another is a regression even when the total stays constant.

### Ranking the alternatives

**Strengthened B plus untaggable-main C comes first.** It preserves the distinction you want while making regression prevention explicit.

**A comes second as a coherent alternative.** Carrying all implementations in one feature lane does not inherently violate “a lane lands alone”; they can all belong to that one change. Its real cost is abandoning the intended separation between birth participation and release participation.

**D becomes appropriate when shipping fixes independently of unfinished main becomes necessary.** It is not justified merely to avoid naming development debt now.

**Bare C—merge whatever passes in the twins—is too permissive.** It permits unrelated regressions in release-required ports.

**E comes last under the settled model.** A `requiredFrom` version changes when a promise binds, rather than just where its implementation work happens. It introduces another release-dependent promise mechanism after you have deliberately retired lag.

This recommendation does require more information than blanket twin-only merging. That information is necessary to distinguish an admitted gap from a regression. It does not require a separate feature catalogue or a hand-maintained per-tag list: the execution manifest and lane contract already identify the obligations.

## 4. Release coupling: retain the veto, but put it before publication

### Keep lockstep for the intended cohort, not automatically for all eight

For Nightseam’s stated purpose, I would retain a common certified release and permit the four pilots to become release-required once their promises hold and the operator accepts the ongoing cost. I would not automatically extend that decision to Java and Swift.

The justification is not simply the lack of downstream consumers. It is that you deliberately want difficult implementations to constrain the definition, including C++ and Haskell. Making those implementations part of release certification is a legitimate project-quality commitment. 

**An implementation does not need registry customers to be worth listening to before declaring a version complete.**

Your proposed publication prerequisite would make an external distribution choice determine whether that internal commitment is permitted. Those are different decisions.

### Require reproducible consumption, not necessarily a registry artifact

The distribution table already defeats a simple published/unpublished Boolean. Go is distributed through tags rather than uploads. C++ is consumed as a pinned checkout. Python, Rust and Haskell exercise packaged consumption without publishing; Java and Swift have different remaining packaging limitations. 

Before promotion, I would require a documented, reproducible way to consume the implementation outside its conformance harness. For a source-distributed library, a pinned checkout and genuine external consumer smoke can satisfy that requirement.

Record distribution separately: package coordinates, source-consumption instructions, artifacts, version checks and smoke commands. One implementation may have several packages, so even operationally a Boolean is too small.

A registry artifact is a useful deliverable. It is neither necessary nor sufficient proof of a meaningful implementation.

### What the veto really purchases

A refusing disposition means accepting that **an unrelated fix may wait because another implementation has not fulfilled the current definition**. There is no gate mechanism that removes that cost while preserving the same promise.

You cannot simultaneously guarantee all four of these:

> Features merge before all required implementations catch up; every release-required implementation holds the current definition at every release; releases come only from current main; unrelated fixes can always ship immediately.

At least one must give.

Under my recommendation, immediate release availability gives. When that becomes unacceptable, the honest alternatives are to finish or revert the unfinished definition change, release a separately maintained compatible line, or change the release promise explicitly.

This also requires a scheduling rule: when a release is desired, the fleet must prioritize closing admitted gaps rather than indefinitely adding new ones. “Untaggable main” is a tolerable state, not a useful permanent operating condition.

Price promotion using observed behavior: time spent blocking otherwise-ready candidates, first-attempt toolchain reliability, catch-up latency, repeated semantic redesign and operator intervention. The document supplies incidents and rough whole-workflow timings, but not enough data to calculate a per-port release cost.  

Separate semantic and infrastructure costs. Attempt every language’s suite, but isolate toolchains and outcomes. An unavailable required implementation prevents certification; an unavailable marked implementation should produce explicit missing evidence and provisional status—not an accidental global veto merely because its installer was a hard-failing shell step.

### The current “tag gate” is on the wrong side of the tag

This is the most urgent lifecycle correction.

The workflow starts on a pushed `v*` tag. The preflight and reviewer gate therefore cannot refuse creation of that tag; they can only stop later workflow operations. Go is already consumed from module tags. The tag is not just an internal request to publish something else.  

I would define the sequence as:

**Prepare an exact candidate → collect and certify its evidence → approve publication → create public release tags and publish the tested artifacts → verify distribution.**

The release gate must run before the first externally consumable release marker. Subsequent publishing must use the certified source and artifacts, not rebuild mutable main.

This does not make multi-package distribution atomic. Partial publication remains possible and needs an explicit state and recovery procedure. A post-publication round trip is valuable, but it is confirmation and incident detection—not proof that nothing escaped before the gate.

### What other projects show

Protocol Buffers demonstrates that coordinated releases remain workable across multiple runtimes, while also separating language-specific major versions from a shared minor/patch release identity. Coordination does not require every ecosystem’s API version to be identical. ([Protocol Buffers][4])

Apache Arrow demonstrates an actual move away from coupling: its Go implementation’s October 23, 2024 release announcement explains that leaving the monorepo would detach its version from C++ and permit fewer major releases and more minor/patch releases. Arrow separately versions the format and libraries. ([Apache Arrow][5])

OpenTelemetry explicitly permits independent versions for different language implementations and for an implementation versus the specification it implements. That is a valid model, but it does not supply Nightseam’s proposed “complete at every common release” guarantee. ([OpenTelemetry][6])

My ranking is therefore **certified lockstep first for this project; independent implementation releases against identified definition revisions second when release autonomy becomes a requirement; automatic per-language omission last under the current promise**. Omission can be a coherent policy, but calling it “refuse” would conceal that you have changed what is refused.

Independent releases transfer work to compatibility declarations and consumer selection. They do not remove that work.

## 5. What you have not asked

### Who authorizes changes to the rules themselves?

Machine-readable policy is necessary but insufficient when agents can also edit that policy, the tests and the enforcers.

The operator owns promises, while ordinary lanes merge without human approval. Those two facts require an authority boundary: a lane must not authorize its own demotion, delete its own obligation, or weaken its own witness requirement simply by editing a parseable file.  

Bind enforcement-changing edits to an operator-authorized decision through a mechanism the candidate cannot self-approve. Ordinary implementation lanes can remain review-free.

Also give both enforcers shared behavioral fixtures covering missing cells, unknown implementations, absent testees, profile transfers, admitted gaps and stale evidence. Implementing the policy twice is acceptable; letting the two implementations define different policies is not.

### “Holds against Go” is not the same claim as “interoperates with every required implementation”

Passing against a reference is not transitive. Two implementations can each work with a permissive Go implementation yet disagree with one another.

Your current definition of *holds* is precise about the star, while all-pairs failures only create issues and are not read at release time. That is a real boundary on what the release certifies. 

Either describe the guarantee narrowly, or strengthen release evidence for the interoperability combinations you intend to guarantee. At minimum, a known reproducible all-pairs incompatibility between required implementations should trigger an explicit release decision, not disappear behind green star cells.

This is not merely hypothetical as a category of risk: Arrow’s February 16, 2025 patch release repaired C++/Python/R inability to read Parquet files produced by particular Rust versions. That incident illustrates why independently versioned implementations still need cross-implementation compatibility evidence; it does not establish that independent versioning caused the bug. ([Apache Arrow][7])

### Does the suite prove the property whose name it carries?

The supplied serial scenario tests rejection of particular incoming serial sequences. By itself, it does not demonstrate that a real sender allocates and emits serials correctly under competing calls. The document mentions accompanying unit tests, so this is not a finding that sender-side coverage is absent—only that the displayed wire scenario does not prove that property. 

Apply the same discipline to `generator`: socket exchanges, generated API usability and the instantiation law are different proof obligations. A profile name should not imply evidence that none of its constituent tests actually supplies.

### Bitlink needs applicability, not permissive skipping

Replacing “language” with “target” is not sufficient. A database renderer cannot truthfully fail—or pass—a promise about being a duplex runtime. Your successor description already exposes that mismatch. 

Distinguish an obligation that is **inapplicable to the declared target role** from an applicable obligation that is unimplemented. Applicability must come from the definition and policy, not from a testee declining to announce a capability.

In particular, separate shared declaration-rendering laws from generated-RPC integration requirements before bitlink adopts the `generator` name. Do not silently give the same profile two meanings, and do not force non-runtime targets to inherit tunnel/live obligations merely through that name.

### Same-version completeness does not answer the 1.0 compatibility question

A common version makes one tested combination easy to identify. It does not establish whether an older generated client can use a newer runtime, or whether peers from different releases interoperate.

Those are additional dimensions, not additional support tiers. Protocol Buffers’ separate generated-code/runtime compatibility rules are a useful example of making that boundary explicit. ([Protocol Buffers][8])

Nightseam need not promise cross-version compatibility now. It should avoid letting the lockstep number be mistaken for such a promise when 1.0 arrives.

## The two rules I would build around

**Merge admission:** a lane supplies its required birth evidence, preserves established required coverage, and identifies every deliberately unfinished obligation.

**Release certification:** every release-required implementation supplies complete, passing evidence for its promises against the exact candidate, before that candidate becomes publicly released.

Those rules preserve the distinction the original ladder obscured. Twins constrain where the definition is first proven. Release-required implementations constrain when the project may certify it. Publication determines how consumers obtain it. None needs to stand in for the others.

[1]: https://arrow.apache.org/docs/format/Changing.html "Changing the Apache Arrow Format Specification — Apache Arrow v25.0.1"
[2]: https://github.com/WebAssembly/meetings/blob/main/process/phases.md "meetings/process/phases.md at main · WebAssembly/meetings · GitHub"
[3]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue "Managing a merge queue - GitHub Docs"
[4]: https://protobuf.dev/support/version-support/ "Version Support | Protocol Buffers Documentation"
[5]: https://arrow.apache.org/blog/2024/10/23/arrow-go-18.0.0-release/ "Apache Arrow Go 18.0.0 Release | Apache Arrow"
[6]: https://opentelemetry.io/docs/specs/otel/versioning-and-stability/ "Versioning and stability for OpenTelemetry clients | OpenTelemetry"
[7]: https://arrow.apache.org/blog/2025/02/16/19.0.1-release/ "Apache Arrow 19.0.1 Release | Apache Arrow"
[8]: https://protobuf.dev/support/cross-version-runtime-guarantee/ "Cross-Version Runtime Guarantee | Protocol Buffers Documentation"
