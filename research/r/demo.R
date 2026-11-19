# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>
#
# demo.R — end-to-end run on a throwaway vault.
#
#   Rscript demo.R          # builds a temp vault, imports 4 turns, runs 2 queries, cleans up
#   Rscript demo.R --keep   # keeps the temp vault and prints its path
#
# The demo never touches your real vault: it passes --db and --scope explicitly.

this_file <- sub("^--file=", "", grep("^--file=", commandArgs(FALSE), value = TRUE))
script_dir <- if (length(this_file) > 0) dirname(normalizePath(this_file)) else getwd()
source(file.path(script_dir, "rmemory.R"))

keep <- "--keep" %in% commandArgs(trailingOnly = TRUE)

work <- tempfile("rm-r-demo-")
dir.create(work)
db <- file.path(work, "memory.db")
scope <- "demo"
transcript <- file.path(work, "session.jsonl")

lines <- c(
  '{"type":"user","message":{"role":"user","content":"课题决定：不做向量化，全部用本地词法统计，因为我们要的是可解释的证据量，不是语义相似度。"}}',
  '{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"证据量用什么单位？"}}',
  '{"type":"user","message":{"role":"user","content":"用纳特。每个候选的分数是负对数碰撞概率，可以直接相加比较。"}}',
  '{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"那时间维度怎么处理？"}}',
  '{"type":"user","message":{"role":"user","content":"时间做事件簇：统计词族占据了多少天，和独立均匀撒点的期望天数比较，用形状量而不是显著性门。"}}',
  '{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"明白，这条记下来了。"}}',
  '{"type":"user","message":{"role":"user","content":"另外把长度归一化去掉，实测排名没变化，说明长度惩罚是伪需求。"}}',
  '{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"好，我去改。"}}'
)
con <- file(transcript, open = "w", encoding = "UTF-8")
writeLines(lines, con = con, useBytes = TRUE)
close(con)

cat("vault: ", db, "\n", sep = "")
cat("scope: ", scope, "\n\n", sep = "")

rm_init(db = db, scope = scope)
receipt <- rm_import(transcript, db = db, scope = scope)
cat(paste(receipt, collapse = "\n"), "\n\n")

for (q in c("为什么不用向量化", "长度归一化 排名")) {
  cat("Q: ", q, "\n", sep = "")
  bands <- rm_bands(q, db = db, scope = scope)
  if (nrow(bands) == 0) {
    cat("  (no band)\n\n")
    next
  }
  for (i in seq_len(min(3, nrow(bands)))) {
    cat(sprintf("  %8.2f  %s\n", bands$score[i], substr(bands$content[i], 1, 56)))
  }
  cat("\n")
}

status <- rm_status(db = db, scope = scope)
memories <- if (!is.null(status$corpus$memories)) status$corpus$memories else NA
cat(sprintf("vault: memories = %s, mode = %s, size = %.1f KB\n",
            memories, status$mode, status$db_size_bytes / 1024))

if (keep) {
  cat("\nkept: ", work, "\n", sep = "")
} else {
  unlink(work, recursive = TRUE)
  cat("\ncleaned: ", work, "\n", sep = "")
}
