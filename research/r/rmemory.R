# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>
#
# rmemory.R — minimal R client for a local Rolling Memory vault.
#
# The memory plane has no R SDK by design: the binary is the only interface, and
# every command can emit JSON. These helpers wrap that surface so an analysis
# session can call recall() without leaving R.
#
# Requires: R >= 4.1 and the jsonlite package. No other dependency.
#
#   source("rmemory.R")
#   rm_paths()
#   rm_recall("why did we drop the vector index")
#
# Nothing here writes to the vault except rm_import() and rm_init().

rm_env <- function(name, default = "") {
  value <- Sys.getenv(name, unset = "")
  if (nzchar(value)) value else default
}

rm_bin <- function() {
  explicit <- rm_env("ROLLING_MEMORY_BIN")
  if (nzchar(explicit)) return(path.expand(explicit))

  on_path <- unname(Sys.which("rolling-memory"))
  if (nzchar(on_path)) return(on_path)

  fallback <- path.expand("~/.local/share/rolling-memory/bin/rolling-memory")
  if (file.exists(fallback)) return(fallback)

  stop(paste(
    "rolling-memory binary not found.",
    "Install it with:",
    "  curl -fsSL https://github.com/rocareer/rolling-memory/raw/main/install.sh | bash",
    "then either add ~/.local/bin to PATH or set ROLLING_MEMORY_BIN.",
    sep = "\n"
  ), call. = FALSE)
}

rm_paths <- function() {
  list(
    bin = rm_bin(),
    db = path.expand(rm_env("ROLLING_MEMORY_DB", "~/.local/share/rolling-memory/memory.db")),
    scope = rm_env("ROLLING_MEMORY_SCOPE", "default")
  )
}

rm_run <- function(args, binary = rm_bin()) {
  # system2() assembles a shell command line and does not quote arguments, so a
  # query containing spaces would arrive as several flags. Quote every token
  # (shell quoting is stripped again before the binary sees it).
  rendered <- vapply(args, shQuote, character(1))
  out <- suppressWarnings(system2(binary, rendered, stdout = TRUE, stderr = TRUE))
  status <- attr(out, "status")
  if (!is.null(status) && status != 0) {
    stop(sprintf(
      "rolling-memory %s failed (exit %d)\n%s",
      args[[1]], status, paste(out, collapse = "\n")
    ), call. = FALSE)
  }
  invisible(out)
}

rm_json <- function(args, binary = rm_bin()) {
  out <- rm_run(c(args, "--json"), binary = binary)
  text <- paste(out, collapse = "\n")
  if (!nzchar(trimws(text))) stop("rolling-memory returned no output", call. = FALSE)
  jsonlite::fromJSON(text, simplifyVector = FALSE)
}

# Create a vault. The scope is bound at init time: a vault answers only for the
# scope it was created with, so pass the same scope to every later call.
rm_init <- function(db = rm_paths()$db, scope = rm_paths()$scope, binary = rm_bin()) {
  rm_run(c("init", "--db", db, "--scope", scope), binary = binary)
}

# Import a JSONL transcript. Assistant rows must carry a terminal
# "stop_reason":"end_turn"; turns without that marker are treated as unfinished
# and skipped.
rm_import <- function(path, db = rm_paths()$db, scope = rm_paths()$scope, binary = rm_bin()) {
  rm_run(c("import", path, "--db", db, "--scope", scope), binary = binary)
}

rm_status <- function(db = rm_paths()$db, scope = rm_paths()$scope, binary = rm_bin()) {
  rm_json(c("status", "--db", db, "--scope", scope), binary = binary)
}

rm_doctor <- function(db = rm_paths()$db, scope = rm_paths()$scope, binary = rm_bin()) {
  rm_json(c("doctor", "--db", db, "--scope", scope), binary = binary)
}

# Query the vault. crystallize = FALSE keeps the query itself out of memory, so
# repeated analyses do not grow the vault.
rm_recall <- function(q, raw_limit = 10L, db = rm_paths()$db, scope = rm_paths()$scope,
                      crystallize = FALSE, binary = rm_bin()) {
  args <- c(
    "recall", "--q", q, "--raw-limit", as.character(raw_limit),
    "--db", db, "--scope", scope
  )
  if (!crystallize) args <- c(args, "--no-crystallize")
  rm_json(args, binary = binary)
}

# Flatten the band list into a data frame: one row per memory band, ordered by
# score. Scores are in Nats (negative log collision probability under the null),
# so they are additive evidence, not a similarity in [0, 1].
rm_bands <- function(q, ...) {
  res <- rm_recall(q, ...)
  if (isTRUE(res$absent) || length(res$bands) == 0) {
    return(data.frame(
      score = numeric(0), subject = character(0), title = character(0),
      content = character(0), stringsAsFactors = FALSE
    ))
  }
  do.call(rbind, lapply(res$bands, function(band) {
    first <- if (length(band$nodes) > 0) band$nodes[[1]]$content else ""
    data.frame(
      score = as.numeric(band$score),
      subject = band$subject,
      title = band$title,
      content = gsub("\n", " ", first),
      stringsAsFactors = FALSE
    )
  }))
}
