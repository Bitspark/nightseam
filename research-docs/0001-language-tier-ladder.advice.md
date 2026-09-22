# Advice for consult
**Research doc:** doc_ad9a7942-4006-404f-9a30-9ba9fb9bf999.md
**Document:** doc_ad9a7942-4006-404f-9a30-9ba9fb9bf999
**Advisor:** ChatGPT GPT-6 Pro
**Session:** nsess_5c324c27-1d3b-4e92-80a0-a599cfdc3f62
**Run:** run_84bcc7f9-667e-4e83-aad4-18e38ddc1696
**Chat:** https://chatgpt.com/c/6ab26b42-8a64-83eb-a11a-ed1b1c066175
**Verify:** nightfall consult verify run_84bcc7f9-667e-4e83-aad4-18e38ddc1696 --output <this file>

# Response to “The language tier ladder”

**I would drop the lag, retire tier 3 as a numbered rung, and separate tier 1’s development obligation from its consumer promise.** The replacement should describe **which profiles a language promises and whether breaking those promises stops a release**. Keep the reference implementation and the feature-twin cohort as separate roles.

That is closest to **C, amended**, with **B, collapsed**, as the simpler alternative. I would also adopt the record-keeping part of **F**, but reject its suggestion that accepting a failure lets a language continue claiming an unqualified promise.

The central distinction is this:

> The matrix records what happened. The policy commits the project to what must happen before a release. The twin rule decides how features are developed.

Those are three different things. Their current coincidence in Go and TypeScript does not make them one thing.

## 1. What earns a rung above wire?

### A promise is not redundant just because its evidence is already a column

I would qualify the argument against tier 3. “It differs by one bit, and that bit is already visible” is not sufficient to eliminate it. A green `generator` cell says the generated output passed this run; promising `generator` says future failures have a declared consequence. Observation and commitment remain different even when displayed together. The document’s distinction between the matrix and the tier policy already supports that separation. 

The better objection is that **generated coverage does not occupy a useful intermediate position in the dependency structure you actually have**.

Under the supplied evidence, holding `generator` requires both tunnel and live implementations. It is therefore not an inexpensive step between wire support and the component runtimes. Conversely, having those implementations does not itself prove that every standalone tunnel and live scenario passes. Existence, generated-composition coverage, and standalone component conformance should not be silently equated. 

My conclusion is:

**“Generated” deserves a named promise and a visible column, but not its present position on a universal ordinal ladder.** Keep the existing generated conformance obligation intact; do not weaken it merely to manufacture a convenient middle rung.

I would also soften “tier 3 is unreachable as written.” It is reachable after implementing substantially more than its name and written nesting suggest. That is a misleading decomposition, not an impossibility.

### My candidate ranking

| Rank  | Candidate        | Assessment                                                                                                                                                             |
| ----- | ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **1** | **C, amended**   | Declare coverage and release disposition separately. Record reference and twin membership independently. Do not derive release disposition from a “simultaneous” flag. |
| **2** | **B, collapsed** | Publish reference-held coverage and complete-at-every-tag support. Move feature-lane membership out of the consumer ladder.                                            |
| **3** | **D**            | Separating dimensions is useful, but “simultaneous” versus “same-tag” still confuses development timing with released availability.                                    |
| **4** | **E**            | A defensible option only after identifying a consumer need for bounded feature lag. It requires substantially more precise semantics than successive red cells.        |
| **5** | **A**            | Repairing the implementation would leave the conceptual problems intact. Cheap repair is not an argument for retaining the model.                                      |

**F is orthogonal:** useful for recording known failures, dangerous when used to preserve an unqualified coverage claim.

### The amendment C needs

C currently makes release blocking depend on being “simultaneous or complete.” I would instead retain the two facts that already exist in the policy: the promised set and its failure disposition. That recognizes existing machinery rather than introducing another overloaded classification. The document already identifies `requires` and `onFailure` as distinct dimensions. 

Initially, the public presentation could remain small:

* **Reference-held:** the named coverage is exercised and reported; a breach makes the language provisional without stopping the joint release.
* **Complete at every tag:** every profile in the declared completeness universe must hold before release.

Tier 4 remains the `core`-only instance of the first category, with its existing consequences. A reference-held language may declare generated coverage without needing another number. A language that happens to pass everything does not thereby become release-required.

There is a ratification issue here: **C is not entirely compatible with the earlier rejection of finer-than-four promises.** Giving arbitrary profile subsets contractual consequences makes distinctions that the earlier decision assigned only to progress. The operator should explicitly resolve that tension. No new P5 is necessary, but that does not make the policy change merely editorial. 

Should that earlier restriction remain absolute, choose collapsed B and limit named coverage bundles to the existing promise vocabulary.

Publication can be coarser than policy, but it must not hide an actionable difference. A consumer should be able to discover the exact promised profiles and whether their failure blocks a release without decoding a tier number.

### What survives in bitlink?

The **contract structure** survives; the **wire-based ordering does not**.

A database target cannot occupy a rung whose prerequisite is speaking the wire. The successor needs to distinguish what applies to a target kind, what the project promises within that scope, and what the tests observed. A runtime obligation that does not apply to PostgreSQL should be *not applicable*, not skipped, failed, or silently counted as passed. The document expressly establishes that these targets are not runtimes. 

“Complete” would consequently mean complete against an operator-approved target-kind contract—not complete against whatever subset the implementation happens to advertise. The target must not be allowed to shrink its own denominator.

Keep Nightseam’s current generator dependencies for Nightseam. Do not export them merely because bitlink inherits the profile name. The successor will need substantive assertions about rendered output; the supplied document does not yet establish what those assertions are.

## 2. Should timeliness be an axis?

**Timeliness can be a meaningful promise. This particular lag should be removed.**

I would not make the strongest argument “the code could not carry it.” The code can carry historical state when supplied with the appropriate inputs. Nor does a one-to-two-day allowance automatically lack value.

The stronger problem is that the implemented rule does not measure the promised property.

### The existing mechanism does not establish bounded completeness

The current gate notices failures outside the immediately required profiles, but not missing-feature skips. It also treats failures in different profiles on consecutive releases as the same lag episode. Those are documented properties, not hypothetical defects.  

Consequently:

**A missing capability represented by skips does not start the catch-up clock.** Other profile dependencies may force some implementation work, but the lag mechanism itself supplies no completeness deadline for those skips.

**Two different new gaps can exhaust the allowance even when neither is individually late.**

**A newly discovered defect and an intentionally deferred new feature are treated alike**, although a consumer may reasonably accept delayed access to new functionality while rejecting regressions in functionality already promised.

The mechanism is therefore closer to “no two consecutive releases with certain failures” than “all features arrive within one release.”

A genuine lag promise would need to identify the feature obligations introduced at each release and require older obligations to hold by their deadline. Missing support would count alongside failed support. A new test for an old requirement could not automatically restart that requirement’s clock. Merely comparing the same aggregate cell would still be insufficient.

That is a coherent design, but the document supplies no consumer requirement that justifies choosing it now.

### Complete-at-tag already delivers the consumer-facing P4

There is a distinction between being complete in one observed release and promising completeness at every release. But once a language promises to hold the current, growing suite at **every tag**, features necessarily arrive there before their first release. That satisfies P4’s written availability promise, regardless of which implementation PR merged first. P4 does not say “the same pull request”; the twin rule does.  

Tell the consumer:

> Every component covered by this commitment is available in this language in the release that introduces it. Implementations may be completed in separate development lanes.

There is no lost consumer distinction to preserve between the top two rows of B. Their distinction belongs in contributor policy.

### B has a hidden workflow cost

Candidate B says a complete language may receive a feature in a later PR while the tag waits. **That is not possible under the existing PR gate without another workflow decision.** The Go runner runs on every CI star, and blocking verdicts fail the job. If all six languages require every newly added scenario, a PR containing only the first two implementations becomes unmergeable.  

You must either stage the implementations together before merge, or distinguish merge eligibility from release readiness. In the latter arrangement, CI must still report incomplete coverage truthfully; an unfinished release commitment must not become a green skip.

Otherwise, “same-tag but not twins” changes the prose while leaving an effective all-language twin obligation in the merge gate.

Rust provides a useful, bounded comparison: its target policy records temporary disabling of targets in nightly development during the introduction of `u128` and `i128`, with restoration expected before stable release or possible demotion/removal. That is development freedom combined with an explicit support decision—not continued advertising of an unmet stable guarantee. ([Rust Documentation][1])

Finally, the supplied scenario counts and cadence do not establish how many days this choice will cost. Lane structure and achievable parallelism matter. Measure feature-to-release delay under the chosen workflow rather than treating expanded test counts as effort estimates.

## 3. How should reference, twins, and promised implementations relate?

Keep all three roles distinct.

| Role                                | The question it answers                                                       |
| ----------------------------------- | ----------------------------------------------------------------------------- |
| **Reference implementation**        | What tool do we use to investigate and settle an implementation disagreement? |
| **Feature-twin cohort**             | Which implementations must participate before a feature lane is accepted?     |
| **Release-required implementation** | Which unmet promises prevent a tag?                                           |

The document already distinguishes these roles, although only two are represented explicitly in policy. 

Go may occupy all three roles and TypeScript the latter two. A pilot can become release-required without becoming a permanent participant in every feature’s initial design.

I would avoid calling Go *normative* without qualification. Your standing goal says the reference is a disagreement-resolution tool, not the definition’s home. When the implementations disagree, the resolution should ultimately be expressed in the protocol, tables, or scenarios—not merely “Go does this.” 

The WebAssembly comparison supports separating these roles more strongly than it supports a permanent top tier. Its phase-4 entry requirements distinguish implemented-and-tested Web VMs, a toolchain where applicable, an updated specification, and an updated reference interpreter. They do not require the same permanent pair of implementations to originate every proposal together. 

For Nightseam, keeping Go and TypeScript as the permanent twins may still be the right deliberate experiment. It is simply **a development obligation, not superior released coverage**.

Nor should passing one release automatically conscript another language into that obligation. Passing establishes present conformance. Admitting a language to the twin cohort commits future development work and therefore remains an operator decision.

## 4. What should a red cell cost, and how does a language stop paying?

### Preserve the difference between evidence and permission

I recommend these consequences:

| Situation                                                        | Consequence                                                                               |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Required scenario fails or skips for a release-required language | Refuse the tag.                                                                           |
| Required testee cannot be built, preventing required evidence    | Refuse the tag for a release-required language; report absence for a nonblocking one.     |
| Promised profile fails in the reference-held category            | Ship the joint release, but mark the language provisional and identify the unmet promise. |
| Failure or skip outside the promised set                         | Preserve it as evidence without inventing a release obligation.                           |
| Known accepted failure                                           | Annotate the failure; do not turn it into coverage.                                       |

This keeps tier 4’s disposition unchanged and preserves both existing rulings: required skips are red, and the gate must enforce exactly the promise made.  

An untiered language should not share the same interpretation as a promised-but-broken language. “No promise assigned” and “promise currently unmet” need distinct presentation, even when neither blocks the release. The existing `provisional` verdict conflates them. 

### Adopt F’s knowledge, not its fiction

The six ports’ serial-number gap is an excellent reason to record known failures. One shared issue can explain the underlying change while retaining the exact affected outcomes for each language. It is not a reason to display successful core conformance. 

Protobuf’s mechanism supports more discipline than simply listing expected failures: its current runner detects unexpectedly passing tests, entries matching no tests, and mismatched failure messages. It also distinguishes required and recommended tests, so its policy should not be reduced to “every unlisted failure is fatal.” 

For Nightseam, I would start with an operator-reviewed annotation identifying the language, relevant expanded outcomes, expected failure, issue, reason for acceptance, and review or expiry point. The outcome remains failed. An unexpected pass means the annotation must be removed; it does not mean the implementation should be made to fail again.

This need not be a second policy authority or even a separate file.

A **release waiver** is a different action. It authorizes a release despite an unmet requirement. Should you allow one, the affected release must be visibly qualified, with exact scope and expiry. It cannot retain an unqualified “complete; every scenario passes” claim.

That is why I reject F’s proposed “known gap without going provisional” unless another equally explicit qualified status replaces provisional. **Accepting the risk does not make the promise true.**

### Make provisional informative, not punitive

For a nonblocking language, credibility comes from showing what failed, its consumer effect, the responsible issue, and the operator’s disposition. A useful statement is:

> The wire commitment is currently unmet because per-request serial numbers are not implemented; the affected scenarios remain failed.

That tells an adopter more than a repeated “provisional” label.

Do not promote a language while its proposed commitment has accepted gaps. Require an explicit operator review of continuing gaps. At that review, the choices are repair, reduce an above-baseline commitment, continue visibly broken support, or withdraw the implementation.

There is an unavoidable limit here:

> **Keeping tier 4 permanently nonblocking means it can remain broken indefinitely unless the operator changes its participation.**

No better label or waiver format eliminates that possibility. Making an expired tier-4 gap block the next tag would reintroduce `stop-next` and change the baseline the question declares fixed.

### Demotion and removal should change future promises, not history

I would require an explicit operator decision for demotion, recording the affected commitment, effective release, reason, and consequences for package publication. A test failure should not silently edit the promise it violated.

Removal is a separate decision: stop future publication for that implementation, identify its last release and status, and preserve historical packages and conformance evidence rather than erasing them.

Rust’s 2025 demotion of `i686-pc-windows-gnu` illustrates the distinction: the project announced reduced testing guarantees while continuing to distribute the compiler and standard library. Demotion did not itself mean disappearance. Go’s porting policy likewise makes removal after a broken release an explicit choice, not an automatic consequence. ([Rust Blog][2])

Given the document’s stated absence of an installed base, you do not need elaborate compatibility machinery now. Before relying consumers exist, however, decide what notice a withdrawal requires. A one-release notice measured in one or two days should not acquire credibility merely by resembling another project’s release-count rule.

## 5. Corrections to the prior-art reading

The survey is useful, but several conclusions need narrowing.

**Node’s tier-2 entry is materially incorrect.** Its current building policy says test failures on both tier-1 and tier-2 platforms block releases. Tier 2 permits infrastructure issues to delay delivery of binaries. That separates behavioral conformance from artifact availability; it does not generally permit tier-2 test failures to ship. 

**Rust’s middle tier guarantees a different property rather than delayed satisfaction of the highest tier.** Tier 2 guarantees builds; tier 1 guarantees builds and tests. The transferable lesson is to name the guarantee precisely—not to import a build-only rung that Nightseam has already rejected. ([Rust Documentation][3])

**Kubernetes has a more relevant timing comparison than API removal notice: version skew.** Its policy specifies supported version relationships between communicating components, including an older kubelet and a newer API server. That is an interoperability envelope for deployment and upgrades, not a feature-delivery deadline. Deprecation notice protects continued use; catch-up lag promises future availability. They should not be treated as the same axis. ([Kubernetes][4])

**OpenTelemetry’s stability labels are substantive promises, not merely ungated words.** Its specification imposes compatibility requirements on stable APIs and SDK interfaces. It also permits independent versions across languages and between implementations and the specification. Those are different answers to different consumer questions, not weaker versions of Nightseam’s conformance gate. ([OpenTelemetry][5])

**Arrow’s status matrix is not entirely binary in meaning.** Its checkmarks have qualifications, including unsupported nested dictionaries and size restrictions. That supports publishing precise limitations beside coverage, but a status table alone does not establish the release consequences of those limitations. ([Apache Arrow][6])

I would therefore replace “a ladder whose only worthwhile consumer is the release gate” with:

> A support classification is useful when it communicates a specific commitment and the project has an accountable mechanism for honoring it.

For Nightseam’s conformance promises, that mechanism should indeed be the release gate. For API stability or withdrawal notice, other mechanisms can be meaningful. The surveyed examples also do not establish a reliable prevalence claim about how rarely projects promise timeliness.

## 6. What the framing hides

### The star is not proof of arbitrary pairwise interoperability

P1 promises interoperation with other implementations, while routine CI pairs each implementation with Go. The nightly tests non-reference pairs, but their failures are filed against the scenario.  

Agreement with a reference is not logically transitive. Two implementations may each work with a permissive reference and fail with each other.

Keep the star as the routine economical check. But decide what a **known** failure between two release-required implementations means. On a mutually promised profile, filing an issue should not discharge the promise. The failure must be resolved, shown to be a test error, or explicitly qualify the release.

That would enforce P1, not introduce an unrelated stricter standard.

### Recomputing a verdict does not establish that the evidence is current

Recomputing from cells is the right rule, but evidence also needs to correspond to the candidate source, suite, and policy.

The shown Go verdict reads the language’s assignment from policy; the shown JavaScript gate selects the tier from `row.tier`. Without a consistency check, those inputs can disagree even though neither trusts a stored verdict.  

The document does not establish whether the complete release path verifies those identities. I would make that an acceptance condition for the rewrite, together with rejecting filtered or incomplete evidence. A present cell containing zero failures is not proof that its required scenarios ran.

Both enforcers should be held to the same decision fixtures, including missing evidence, required skips, absence, unknown assignments, and qualified exceptions. Sharing policy data is insufficient unless interpretation is also held.

### Early failures can accidentally enforce a stronger promise

The described hello check kills a run when advertised capability falls short, before the ordinary nonblocking disposition is applied. Likewise, row-level absence can include a generated testee even where generated coverage is not promised. These deserve checking against the “exactly the promise” ruling.  

Missing required capability must remain visible and disqualify the relevant coverage claim. But the final release consequence should come from policy, not accidentally from which setup path noticed it first.

Preserve the distinction between absent and failed. Also preserve which obligations the absent testee prevents you from measuring.

### Profile placement can change the effective promise

Because need-based placement takes precedence over layer-based placement, adding an observability need can move a scenario out of `core` and into `observability`. A `core`-only commitment would then cease to require that scenario. 

Thus, **changing profile membership is potentially changing a promise**, not merely reorganizing tests. Hold those edits accordingly.

This also limits how literally to read profiles as independent consumer components: they are currently disjoint accounting groups even where the behavior under test is cross-cutting.

### “Complete” needs an explicit universe—and auth must not be smuggled into it

Candidate C’s “auth joins as one more profile” is not the current plan. The record says auth is outside the tier obligations, depends on identity-library availability, and is planned for a separate harness rather than a sixth matrix column. 

Keep that boundary unless the operator reopens the decision.

More generally, define which obligations “complete” quantifies over. New scenarios within a promised profile should bind that promise. Adding an entirely new profile or independently held component should be an explicit policy decision. Neither an implementation’s missing prerequisite nor an incidental registry edit should silently redefine completeness.

## Recommended operator decision

I would approve **modified C**, with **collapsed B as its initial public presentation**:

Keep tier 4’s behavior. Remove release lag. Preserve generated coverage as an explicit promise rather than a numbered intermediate rank. Reserve “complete at every tag” for release-required coverage of the declared complete set. Record reference and twin membership independently. Add reviewed known-gap annotations without converting failures into passes, and make demotion and withdrawal explicit prospective decisions.

Promote the four pilots into complete-at-every-tag obligations only when their coverage holds and the operator accepts the continuing release cost—not because the planned assignment already says tier 2. The document makes clear that all six ports are currently tier 4. 

Given the stated purpose of stressing the model with the hard pilots, **I favor accepting that eventual six-language release barrier**. But do not impose six-language feature birth by accident.

**Let the promised implementations determine release readiness. Let the chosen twins determine feature birth. Let the matrix report what actually happened.**

[1]: https://doc.rust-lang.org/rustc/target-tier-policy.html "Target Tier Policy - The rustc book"
[2]: https://blog.rust-lang.org/2025/05/26/demoting-i686-pc-windows-gnu/ "Demoting i686-pc-windows-gnu to Tier 2 | Rust Blog"
[3]: https://doc.rust-lang.org/rustc/platform-support.html?utm_source=chatgpt.com "Platform Support - The rustc book"
[4]: https://kubernetes.io/releases/version-skew-policy/ "Version Skew Policy | Kubernetes"
[5]: https://opentelemetry.io/docs/specs/otel/versioning-and-stability/ "Versioning and stability for OpenTelemetry clients | OpenTelemetry"
[6]: https://arrow.apache.org/docs/status.html "Implementation Status — Apache Arrow v25.0.1"
