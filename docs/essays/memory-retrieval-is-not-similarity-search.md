# Memory Retrieval Is Not Similarity Search

**[English](memory-retrieval-is-not-similarity-search.md) · [简体中文](../zh-CN/essays/memory-retrieval-is-not-similarity-search.md) · [Back to Documentation Hub](../README.md) · [Back to Repository Home](../../README.md)**

Open the README of almost any agent-memory project and the first paragraph is the same: slice conversations into chunks, run them through an embedding model, store the vectors, and return the chunks with the smallest cosine distance at query time. It became the default for a good reason — the model, the index, and the store all have off-the-shelf parts, and you can demo something in an afternoon. We built it that way first, too. The trouble starts when you stop asking "which chunks look like this query?" and start asking what memory is actually for.

---

## 1. A question and its answer never look alike

Here's a common case. The question is: why did the nightly build start dying last Tuesday? The store holds a few dozen messages about the build — status updates, cache discussions, back-and-forth about CI minutes — and one line: `ci: bump toolchain to 1.91 — nightly now dies at link stage`.

That line is the answer. It has almost no words in common with the question, and even its topic overlap with a few dozen chatty messages about "the build" is unremarkable. Rank everything by similarity and the chatter wins; the decisive line sinks, or never surfaces.

The reason is simple: **similarity is symmetric.** It measures how much two texts are about the same thing. Questions and answers are not symmetric — an answer's value is precisely the part the question didn't contain. The less an answer looks like the question, the more it can be worth.

---

## 2. Repetition looks relevant

Embedding spaces are smooth: near-duplicates collapse together. That's a feature for deduplication and a liability for ranking.

Your own history is never short of near-duplicates — retries, periodic status lines, the same bug discussed three times in three sessions. The rare exception — the specific note that appears once — sits in the embedding space as an outlier. The most-said content beats the one-off; that's what a distance metric does. But memory's whole value is the one-off: you don't remember the fifty days the deploy was fine, you remember the one day it wasn't.

There's a plain phrase for this: **a term that appears in nearly every document carries almost no information; a term that appears in one document carries almost all of it.** Repetition lowers information, while similarity treats repetition as evidence of relevance. So the two quantities point in opposite directions exactly where memory matters most.

---

## 3. Memory questions are event questions

*When did we decide this? Who introduced the retry loop? What changed after the incident?* These are the questions people actually ask a memory system. They are questions about events — a time, an actor, a change.

An embedding is a bag of topics with the timeline flattened out: last year's incident and today's fix can sit within a hair of each other, in no particular order. You can bolt on a recency decay or a date filter, and you should — but every bolt-on admits the core score isn't answering the question. Time, entities, and change are structural; they belong in the index, not sprinkled on top of a distance.

---

## 4. A model inside your memory format

There's a quieter cost to embeddings: they make **a specific model part of your memory format.** Vectors mean something only to the model that produced them. Change the embedder, or let the service retire the old model, and yesterday's vectors become a dialect nothing can read — re-embed the entire history, or keep two retrieval dialects alive indefinitely. Notes you intend to keep for years shouldn't depend on which model snapshot happened to be serving when you wrote them.

For memory that's supposed to be local, the embedding call adds two more dependencies: a model on the write path (or a batch queue — worse, because "just saved" stops meaning "just searchable"), usually one on the read path, and a service to keep alive. Remove them and what's left is small enough to audit: verbatim text, an index built from your own corpus, offline, milliseconds, deterministic.

---

## 5. What we rank on instead

This isn't an argument that similarity is useless; it's an argument that it shouldn't be memory's primary key. The primary key should be **discriminating power** — how far does this passage move the odds that it's the answer — not how close it sits to the query.

That quantity is computable from your own corpus: term weight comes from adaptive corpus self-information instead of a static external vocabulary or a pretrained prior; the same question asked twice returns the same results; and every ranked item can be unfolded into the evidence that carried it — which rare terms matched, which time window, which entity. **A retrieval system you can interrogate is a retrieval system you can fix.** Rolling Memory's default local path is built on exactly this: ranking by corpus self-information and deterministic cross-form alignment, no vectors, and no model calls on the retrieval path.

---

## 6. The cost, stated plainly

Similarity's real strength is paraphrase: ask about a "car that won't start" and it connects to "engine fails to crank." If memory's daily job were translation, that strength would be decisive. But you rarely paraphrase your own past; you forget it. The queries that matter are anchored in your own vocabulary — your repo names, your error codes, your dates — which is exactly where dense spaces are blurriest (two adjacent error codes, two neighboring ports, two dates a week apart) and where frequency-based arithmetic is sharpest. Where vocabulary genuinely drifts, deterministic morphology and corpus-level co-occurrence recover most of it.

What remains — questions built purely from figures of speech, with zero lexical overlap — is a real, thin tail. We'd rather know how thin it is than pretend it isn't there. **Every design pays its tax somewhere; we'd rather pay it on that tail than on every write and every read.**

---

So: similarity tells you which passage *looks like* your question. Memory has to tell you which fact *changes your answer*. The first is a demo you can build in an afternoon; the second is the product. If your agent's memory keeps serving the most-said version of things, that's the distance metric doing its job — and the memory failing yours.

---

**Related reading:** [`benchmarks.md`](../benchmarks.md) — how these choices measure up on public benchmarks, and how to reproduce the numbers · [`background-vs-grep.md`](../background-vs-grep.md) — the boundary between local memory and `grep` · [`token-economics.md`](../token-economics.md) — the arithmetic of a single round trip.
