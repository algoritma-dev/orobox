# License analysis: GPL-3.0 and Orobox

> **Scope note.** This document is an analysis, not a decision. It exists to give
> maintainers and prospective adopters a clear, sourced picture of what GPL-3.0
> actually requires in Orobox's specific usage pattern, and what changing the
> license would practically involve. Whether to keep GPL-3.0, dual-license, or
> relicense is a decision for the project's maintainers and copyright holders —
> nothing here should be read as a recommendation to change anything.

## 1. What GPL-3.0 means for someone *using* Orobox as an external dev tool

Orobox is invoked from outside a user's OroCommerce project: you run
`orobox up`, `orobox init`, `orobox deploy`, etc. from the command line against
a bundle or project directory. Orobox's Go source is never imported, linked,
compiled into, or otherwise combined with the user's PHP/JS code. The two are
separate programs that communicate the way separate programs normally do —
one invokes the other as a subprocess, passes it command-line arguments and
files on disk, and reads its exit status and output.

This is precisely the case the GPL's own FAQ addresses under the concept of
**"mere aggregation"** and separate-program communication. From the FSF's
official GPL FAQ (gnu.org), the entry on aggregates vs. modified versions
states:

> "An 'aggregate' consists of a number of separate programs, distributed
> together on the same CD-ROM or other media. The GPL permits you to create
> and distribute an aggregate, even when the licenses of the other software
> are nonfree or GPL-incompatible."

and, on how to tell whether two pieces of software are "separate programs"
or one combined work:

> "By contrast, pipes, sockets and command-line arguments are communication
> mechanisms normally used between two separate programs."

Source: FSF, *Frequently Asked Questions about the GNU Licenses*,
<https://www.gnu.org/licenses/gpl-faq.html#MereAggregation> (accessed 2026-09-02).

Applied to Orobox: a project or bundle that is merely *operated on* by
Orobox — started, stopped, installed, tested, deployed — via CLI invocation,
Docker orchestration, and file-system access, is not linked with Orobox and
is not a derivative work of it. The standard FSF/OSI position is that this
kind of "use a GPL'd tool from the outside" pattern does not extend the GPL
to the tool's output or to the software being operated on. Compilers,
build tools, editors, and CI runners licensed under the GPL (e.g. GCC) are
the canonical examples: compiling proprietary code with GCC does not make
that code GPL, because GCC is invoked, not linked in.

**Conclusion for this use case:** a developer or company using Orobox to
develop, test, and deploy a proprietary OroCommerce bundle or project incurs
no GPL obligation on that bundle's or project's own code. GPL-3.0 obligations
attach to *copies of Orobox itself* (see §2), not to what you build with it.

This is an analysis of the general FSF position and Orobox's own
architecture (subprocess/CLI invocation, no shared address space, no
compiled/linked artifacts crossing the boundary). It is not a substitute for
independent legal advice if your organization needs a formal opinion.

## 2. What GPL-3.0 means for someone who modifies and redistributes Orobox itself

This is the ordinary copyleft case, and it is unambiguous:

- If you take Orobox's source, modify it, and distribute the modified
  version (as a binary, a fork, a hosted service that ships copies, etc.),
  the modified version must also be licensed under GPL-3.0 (or, per Orobox's
  own license grant, "any later version" if you choose that option).
- You must make the corresponding source code available to recipients of the
  binary, under the same terms.
- You must preserve copyright notices and the license text, and document
  significant changes you made to the files.
- You may not add further restrictions on recipients' exercise of the GPL's
  granted rights (GPL-3.0 §10), and if you distribute it embedded in
  hardware, GPL-3.0's anti-tivoization terms (§6) apply to install
  information for that hardware.
- There is no obligation to publish changes you make for purely private,
  non-distributed use (running a patched Orobox internally, without handing
  copies to anyone outside your organization, does not trigger the
  source-availability requirement) — but as soon as you distribute the
  modified tool outside that boundary, the copyleft terms apply in full.

In short: **Orobox itself, and anything derived from its source, stays
GPL-3.0.** This is the mechanism by which the project stays free software
and prevents proprietary forks — it is unrelated to §1's analysis of merely
*using* the tool.

## 3. GPL-3.0 vs MIT vs Apache-2.0: adoption friction

None of this is about whether GPL-3.0 is legally deficient for Orobox's use
case — per §1, it isn't. The friction is practical and reputational: many
companies' legal/procurement/dependency-scanning policies treat "GPL" as a
blanket red flag for any dependency, including dev-only tools that are never
linked into shipped code, simply because distinguishing "linked dependency"
from "invoked external tool" requires a case-by-case legal read that most
automated or blanket policies don't perform. That caution is what drives
license-switch conversations for tools like this — not a defect in the
GPL-3.0 analysis above.

| | GPL-3.0 | MIT | Apache-2.0 |
| --- | --- | --- | --- |
| Copyleft | Yes — modified/derivative distributions must stay GPL-3.0 and disclose source | No | No |
| Patent grant | Implicit, tied to distribution (§11) | None | Explicit, express grant with defensive termination |
| Can be embedded in proprietary products | No, if linked/combined; yes if merely invoked as a separate tool (§1) | Yes, with attribution only | Yes, with attribution and notice of changes |
| Typical corporate policy stance | Often auto-blocked or requires legal review, regardless of actual linkage | Usually pre-approved, minimal friction | Usually pre-approved, minimal friction |
| Signal sent to adopters | "This tool, and forks of it, must stay open" | "Do whatever you want" | "Do whatever you want, plus explicit patent safety" |
| Contributor expectations | Contributions typically expected to be compatible with GPL-3.0 | Effectively public domain-like, low friction to contribute | Same low friction, plus patent clarity for contributors |
| Best fit when... | The goal is to guarantee the tool (and its derivatives) remains free/open forever | The goal is maximum adoption with minimal legal review | The goal is maximum adoption but with explicit patent protection |

For a standalone external dev tool like Orobox — never linked into the
projects it operates on — the *legal* exposure to adopters is effectively
the same under all three licenses (none, per §1). The difference is entirely
in how procurement/legal teams *perceive* the license label at a glance, and
in whether the maintainers want to guarantee that forks of Orobox itself
stay open (which only GPL-3.0 does).

## 4. Ready-to-paste FAQ entry (for README)

> **Does using Orobox affect the license of my bundle?**
> No. Orobox is a development tool you invoke from the outside — via the
> command line, against your project's files and containers — never linked
> into, imported by, or distributed with your code. The GPL's own FAQ treats
> this kind of separate-program invocation (command-line arguments, pipes,
> subprocess exec) as "mere aggregation," not a combined work
> (see the [GPL FAQ](https://www.gnu.org/licenses/gpl-faq.html#MereAggregation)).
> GPL-3.0 governs Orobox's own source and any modified copies of Orobox you
> distribute; it has no bearing on the license of the OroCommerce bundle or
> project you build, test, or deploy with it — proprietary, MIT, or anything
> else.

## 5. Changing the license: the procedural constraint

Relicensing Orobox (fully or partially — e.g. dual-licensing, or moving to
MIT/Apache-2.0) is not a documentation change. Because copyright in each
contributed line is held by whoever wrote it, relicensing requires the
affirmative consent of **every** copyright holder who has contributed code
under the current GPL-3.0 terms, unless their contribution is rewritten out
of the codebase entirely. This is true regardless of how small a
contribution is.

Running `git log --format='%an <%ae>' | sort -u` in this repository on
2026-09-02 returns **3** distinct author identities:

- `Raffaele Carelle <raffaele.carelle@algoritma.it>`
- `Raffaele Carelle <raffaele.carelle@gmail.com>`
- `coderabbitai[bot] <136622811+coderabbitai[bot]@users.noreply.github.com>`

The first two entries are the same person under two email addresses, so in
practice there is currently **one human copyright holder** on record for
this repository's commit history. The `coderabbitai[bot]` entry is an
automated code-review bot; whether its commits (if any carry copyrightable
content, e.g. suggested-fix commits rather than review comments) constitute
a contribution requiring separate consent is a question for the maintainer
to evaluate, but as a matter of caution it is listed here rather than
silently excluded.

Practically, this means: today, relicensing is procedurally simple, because
there is effectively one contributor to clear. That will stop being true the
moment the project accepts external pull requests without a Contributor
License Agreement (CLA) or a Developer Certificate of Origin (DCO) plus
explicit license terms — at that point, every future contributor becomes
another copyright holder whose consent (or whose code's removal) would be
needed to relicense later. Projects that want to preserve the option to
relicense in the future commonly adopt a CLA precisely to keep this
tractable.

## 6. Bottom line

- Using Orobox from outside your project does not put your project's code
  under GPL-3.0. This is well-supported by the FSF's own FAQ on separate
  programs and mere aggregation (§1).
- Modifying and redistributing Orobox itself does carry full GPL-3.0
  copyleft obligations (§2).
- The GPL-3.0 label creates real, if legally imprecise, adoption friction
  through blanket corporate policies — that friction, not a legal problem
  with the current license, is the actual argument that would motivate a
  license conversation (§3).
- Relicensing is currently simple in principle (one human copyright holder
  today, per `git log`), but becomes progressively harder with every
  external contribution accepted without a CLA/DCO (§5).
- **This document does not recommend a license change.** It is provided so
  that the decision — if and when the maintainers choose to consider it —
  is made with accurate legal and procedural context, not guesswork.
