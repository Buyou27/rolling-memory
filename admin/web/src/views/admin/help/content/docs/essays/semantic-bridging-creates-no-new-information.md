# Semantic Bridging Creates No New Information

**[English](semantic-bridging-creates-no-new-information.md) · [简体中文](../zh-CN/essays/semantic-bridging-creates-no-new-information.md) · [Back to Documentation Hub](../README.md) · [Back to Repository Home](../../README.md)**

The fashion in agent memory is to keep adding comprehension to the write path: extract entities into a graph, summarize sessions with an LLM, resolve "they" and "that project" into proper names at ingestion. Busy as it looks, all of it does the same one thing: **bridging.** And a bridge, however well built, does not add a single bit.

---

## 1. What counts as a bridge

A definition first: **any layer that rewrites your corpus into another form so something else can match against it is a bridge.** Embeddings bridge between wordings; summaries bridge between a long session and a short question; graphs bridge between entities scattered across sessions; coreference resolution bridges between "that project" and its name. Their one common move is to pull different statements of the same thing closer together. The companion piece, Memory Retrieval Is Not Similarity Search, covers bridges made of distance; this one covers bridges made of rewriting. Same river.

---

## 2. In information terms, a bridge produces nothing

One plain fact: **no processing of a corpus can add information about that corpus.** In information theory this is the data processing inequality — processing can preserve information or lose it, never create it. A projection can change coordinates; it cannot change information content. Any conclusion that gets extracted must first hold in the original text: in some sentence, in the surrounding context, or in the model's world knowledge. And your private history is not in any model's world knowledge — no pretrained model knows your cat is named Grace, or that your project was code-named Atlas.

What bridging genuinely achieves is higher **utilization**: making a structure that took effort to see into a structure that's easy to consume — the way an index adds no facts and just makes lookup fast. So the first question about any enrichment layer is which of the two it is:

- **A signpost**: indexes, time, adjacency. It makes what already exists easier to find, passes no judgment about meaning, and can be rebuilt if it breaks.
- **A verdict**: rewrites, extraction, resolution, summaries. It commits on the reader's behalf — keeps one reading, discards the rest, and usually replaces the original.

---

## 3. Coreference is the best example they have

The standard argument against lightweight retrieval goes: it can't even tell what "that project" refers to. Hence LLM extraction.

But look at what the extractor actually does: it **reads the surrounding context.** To swap "that project" for the name, it has to finish the nearby text. And the name was in your history all along — people say the name when they first bring a project up, usually within a few turns of the same session; the vet receipt already puts "Grace" and "cat" side by side, and neither needed resolving, because they were lying together all along. So:

- The information needed for resolution was **in the original text all along**;
- Resolution is freezing, into the index, a bridge the raw text already supports;
- And what it copied is exactly the context that lightweight retrieval was accused of not seeing.

Read-time delivery brings that context back as it was written — the session window, the adjacent turns — and the host model gets both halves: the reference and the name. It doesn't need to be "resolved"; it **resolves on the spot** — this is a model already reading hundreds of your past records, a better reader of context than any ingestion-time extractor, because it is sitting right there.

There's a second, funnier layer: **references in queries need no ingestion-time fixing at all.** "Did we ever finish that project?" — who "that project" is, is written in the *current* conversation, and the host is holding it. When it calls recall, it passes the real name. Guessing at document-side "they" at write time is building a bridge for a pedestrian who never shows up.

(An honest boundary: a passage that refers without ever naming, start to finish, can only be bridged by a reader on site. Such passages exist and are rare — and the reader on site happens to be the only qualified bridge-builder for them anyway.)

---

## 4. Write-time bridging is pre-digestion for questions not yet asked

Do the arithmetic on where comprehension happens.

**Bridging at write time** means, for every document, an extra model reads the full context once, understanding and freezing *all possible future questions* in advance. The questions haven't arrived; the bill is paid anyway. And once the frozen product replaces the original, an extractor's misreading is promoted to permanent memory. One more thing worth saying here: **a summary and its original are never two witnesses — they are one witness, twice.** Keeping both as separate evidence is an echo chamber, not corroboration. The frozen product is also tied to the model version that made it: change the model, and you recompute — or live with two generations side by side.

**Bridging at read time** happens the moment a question arrives, exactly once, over exactly the material that matters. The work is done by the host model already on scene: billed per use, gone when done, no version debt.

The mismatch deserves a name: the **pre-digestion paradox** — an auxiliary model that doesn't know the questions, pre-chewing the text for a strong reasoner that already holds the question. You pay extra compute, and you flatten the hedges and context out of the original in the process. There's an old analogy from architecture: materialized views versus query-time joins. Materialization pays off when **you know what the queries look like** — but the query memory must answer is the one someone springs on you three months from now. Materializing against an unknowable query set means prepaying for all of them. And storage never needed summarizing to stay small: **compression happens at delivery, not at rest.** The vault keeps everything; each retrieval hands over the few hundred tokens this question needs.

---

## 5. What isn't wasted

Bridging isn't wrong; storing the bridge as if it were the memory is. Three things deserve to exist without producing one new bit:

1. **Signposts**: indexes, time, adjacency, session windows — they make the original findable, pass no semantic judgment, and can always be rebuilt.
2. **Form alignment that stays deterministic**: date formats, identifier variants, inflections, numbers and units — aligning these needs no "understanding", only stated rules you can check.
3. **Verbatim delivery**: nothing beautified, nothing rewritten, nothing merged. Understanding happens, on the spot, in the amount the moment needs.

A reusable test: **for any layer that claims to have understood "what it means" on your behalf, ask it two questions — where's the evidence, and can it be re-checked?** The distance between a bridge and a memory lives in those two answers.

---

The whole of it, in one line: all of memory's information is in the original text; graphs, summaries, and coreference resolution create none of it — they only move comprehension from one moment to another. And the moving bill is asymmetric. Pay it early and you must guess every question, replace the original, and follow model versions forever. Pay it late and you pay once — at the moment when a reader stronger than any extractor happens to be on scene.

So we leave the bridge in the original text, and leave the understanding until the question arrives.

---

**Related reading:** [`memory-retrieval-is-not-similarity-search.md`](memory-retrieval-is-not-similarity-search.md) — the companion piece on why similarity shouldn't be memory's primary key · [`token-economics.md`](../token-economics.md) — the write-side and read-side ledgers · [`benchmarks.md`](../benchmarks.md) — public readings and how to reproduce them
