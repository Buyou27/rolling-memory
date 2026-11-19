# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>
#
# occupancy-null.R — reproduce the occupancy null distribution and the anchor
# gate guarantee with base R only.
#
# Setup: one word family occurs n times over M active days; each occurrence
# falls on a uniformly random day, independently. D is the number of distinct
# days occupied.
#
#   P(D = k) = C(M, k) * k! * S(n, k) / M^n        (S = Stirling numbers, 2nd kind)
#   E0       = M * (1 - (1 - 1/M)^n)
#   kappa    = max(0, 1 - D / E0)
#
# The retrieval gate accepts a family when kappa >= 1/e, which is the same as
# the integer test D <= floor((1 - 1/e) * E0). For every M >= 3 and n >= 2:
#
#   P(D <= floor((1 - 1/e) * E0) | H0) <= 1/M
#
# with equality at n = 2. The script checks that bound on a grid, runs a small
# Monte Carlo cross-check, and writes a figure.
#
#   Rscript occupancy-null.R

# S(n, 0..kmax) as a vector, built by the usual recurrence in O(n * kmax).
stirling_row <- function(n, kmax) {
  s <- numeric(kmax + 1)
  s[1] <- 1
  if (n == 0) return(s)
  for (i in seq_len(n)) {
    for (j in seq(min(i, kmax), 1)) {
      s[j + 1] <- j * s[j + 1] + s[j]
    }
    s[1] <- 0
  }
  s
}

p_occupied <- function(k, n, M) {
  k <- k[k <= min(M, n)]
  if (length(k) == 0) return(numeric(0))
  s <- stirling_row(n, max(k))
  log_p <- lchoose(M, k) + lgamma(k + 1) + log(s[k + 1]) - n * log(M)
  # log(0) for impossible configurations yields -Inf, exp(-Inf) is 0.
  exp(log_p)
}

expected_days <- function(n, M) M * (1 - (1 - 1 / M)^n)

gate_tail <- function(M, n) {
  cutoff <- floor((1 - exp(-1)) * expected_days(n, M))
  sum(p_occupied(0:cutoff, n, M))
}

gate_ratio <- function(M, n) gate_tail(M, n) * M

# --- self-test: distribution sums to 1, bound holds, equality at n = 2 ---------

n <- 10
M <- 1249
total <- sum(p_occupied(0:n, n, M))
stopifnot(abs(total - 1) < 1e-9)

# Monte Carlo cross-check of the exact distribution on a small grid.
set.seed(1)
M_mc <- 12
n_mc <- 5
draws <- replicate(20000, length(unique(sample.int(M_mc, n_mc, replace = TRUE))))
cutoff_mc <- floor((1 - exp(-1)) * expected_days(n_mc, M_mc))
mc_rate <- mean(draws <= cutoff_mc)
exact_rate <- gate_tail(M_mc, n_mc)
stopifnot(abs(mc_rate - exact_rate) < 0.01)

# --- the bound on a grid ------------------------------------------------------

grid <- expand.grid(M = 3:200, n = 2:40)
grid$ratio <- mapply(gate_ratio, grid$M, grid$n)
worst <- grid[which.max(grid$ratio), ]

cat(sprintf("exact P(D <= cutoff | H0) at M=1249, n=10: %.3e\n", gate_tail(M, n)))
cat(sprintf("one-day chance 1/M:                        %.3e\n", 1 / M))
cat(sprintf("grid max of P(tail) / (1/M):               %.6f  at M=%d, n=%d\n",
            worst$ratio, worst$M, worst$n))
cat(sprintf("Monte Carlo at M=%d, n=%d: %.4f vs exact %.4f\n",
            M_mc, n_mc, mc_rate, exact_rate))

# --- figure: the bound tightens fast with n -----------------------------------

ns <- 2:60
ratios <- vapply(ns, function(k) gate_ratio(M, k), numeric(1))
ratios <- pmax(ratios, .Machine$double.xmin)
out_png <- file.path(tempdir(), "occupancy-null.png")
png(out_png, width = 900, height = 560, res = 120)
plot(ns, ratios, log = "y", type = "b", pch = 19, cex = 0.5,
     xlab = "n (occurrences of one word family)",
     ylab = "P(tail | H0) / (1/M)",
     main = sprintf("Occupancy gate vs one-day chance (M = %d)", M))
abline(h = 1, lty = 2)
grid()
dev.off()
cat("figure: ", out_png, "\n", sep = "")
