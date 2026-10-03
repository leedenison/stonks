---
name: comments
description: How a comment is written in any language, the shape, what a type, field, function, RPC, constant, inline or test comment states, the anti-patterns to strike. Use when writing, changing or reviewing a comment in Go, SQL, protobuf or TypeScript, and in the review before a commit.
---

# Comments

## Shape

A comment is a summary, then any surprises or subtleties.

The first sentence, at most two, says what the thing is or does from the reader's side.
It gives the role the thing plays for its caller, in the terms of the domain, at a level
above the code. That is the whole comment. A further sentence or two is earned only by
what the signature and body do not show: a precondition, an ordering the caller must
respect, a concurrency caveat, a value that means something other than the obvious, a
constraint imposed from outside.

## Failure Mode: Paraphrasing

Do not paraphrase the code. A comment paraphrases when it narrates the body:

* it runs through the steps in order ("sorts ..., then folds ..., and returns ...");
* it restates a conditional the body states as plainly ("where X ...; otherwise ...");
* it names every parameter and return value to account for each;
* it says what the body calls;
* it restates the signature: a type, nil-ness or shape the declaration shows.

## Package Level

A package or file comment holds only what spans the package: the invariants every
function keeps, the conventions every declaration follows, the ordering between files,
and the reasoning behind them where it is not obvious. A sentence about one declaration
belongs on that declaration. 

The test: for each sentence of a package doc, ask which declaration the sentence
concerns. One: move it there and apply the shape test. Several, or none: it stays.

Package and file comments are the specification of the system.

Anti-patterns at this level:

* **Promotion.** A declaration's contract written into the package doc, often leaving
  the declaration with no comment.
* **Stage-labelled paragraphs.** "Order. Choice. Write. Merge." Each label names a
  function.
* **Restating a neighbour's guarantee.** Saying how another package locks, retries or
  recomputes, then also linking it. State the consequence relied on in one clause, then
  the link: "Order against an upload does not matter; see resolve.go and group.go."
* **Restating the schema.** "Its replays row names the source, the scope and the
  administrator."
* **Downstream consequence chains.** An opening sentence narrating what other packages
  do afterwards. The purpose is one clause; effects owned elsewhere are not listed.

## What each kind states

* **Package and file**: as above.
* **Type**: the real world concept modelled, when the name does not carry it, and example
  values when valid values are narrower than the type implies. A term is defined once,
  on its primary artifact (a SQL table or a proto message), and not repeated on derived
  types such as row types.
* **Field**, in a Go struct, a proto message, a SQL table or a TS type: what it means,
  and what absent or empty means when that is not the obvious thing. Do not restate the
  type.
* **Function and RPC**: the behaviour when the name does not carry it. A parameter,
  return or error only when it is surprising: an error that panics, a return that is nil
  on a success path.
* **Constant and variable**: the value's purpose when the name does not say, and the
  source of a number that is tuned rather than derived.
* **Inline**: a reason the code cannot show: an external constraint, a lint suppression,
  the provenance of a fixture modelled on a real export, or why a more direct route was
  not available. Never a narration of the next line.
* **Test and test support**: the scenario the name cannot carry, or why a fixture is
  shaped as it is. The shape rule applies; a helper needs one sentence or none.

## Anti-patterns on declarations

* **Duplication across levels.** A type's comment repeating the package doc's rule for
  it; "Child is safe to call concurrently" in the package doc and on Child. Delete the
  lower copy, or the upper when the fact concerns one declaration.
* **Field-by-field narration in a type comment.** "Source is the datasource that served
  it, and ID the fetch_keys row." The type comment states the concept; a field that needs
  explaining gets its own one-line comment.
* **Test-support narration.** "Returns a wrapper mounting the query client, the clients,
  the auth provider and the activity provider over transport." A helper's comment says
  what the test gets, not what the helper assembles.
* **Exhaustive error cause lists.** A sentinel's comment listing every validation that
  returns it paraphrases the validation code. Keep the cases a caller would not expect.

## What no comment mentions

A comment does not refer to the task, milestone, ADR or pull request behind the code,
nor to the change that produced it.

## Required comments

A language's lint rules or conventions may require a comment on certain declarations;
each language skill says which. Where the name already carries the meaning, that comment
is one sentence, enough to satisfy the rule.

## See also

The `spec` skill for what package and file comments claim beside deferred notes and
spike documents, and the `go`, `protobuf` and `typescript` skills for each language.
