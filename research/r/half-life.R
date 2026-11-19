# Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
# Author: Albert <albert@rocareer.com>
#
# half-life.R — the self-explanation half-life, and what it takes to estimate
# it instead of plugging in a number.
#
# Model: a gap between two surface forms is bridged when one event carries both.
# Each event bridges it with probability beta, independently, so the waiting
# time G is geometric on {1, 2, ...}:
#
#   P(G = g) = (1 - beta)^(g - 1) * beta
#
# The continuous half-life is ln(2) / -ln(1 - beta). The integer median is
# ceil(ln(0.5) / ln(1 - beta)) — the two differ, so report which one you mean.
#
# The interesting statistical problem is not the formula, it is beta: real
# corpora are right-censored (a gap that was never bridged before the corpus
# ends contributes no bridge time), and a naive bridge count is biased low.
#
#   Rscript half-life.R

k_half <- function(beta) log(0.5) / log1p(-beta)
k_median <- function(beta) ceiling(log(0.5) / log1p(-beta))

# --- closed form vs simulation ------------------------------------------------

beta0 <- 0.144
set.seed(1)
draws <- rgeom(200000, beta0) + 1
sim_median <- median(draws)
sim_mean <- mean(draws)

cat(sprintf("beta = %.3f\n", beta0))
cat(sprintf("continuous half-life ln2/-ln(1-b): %.3f events\n", k_half(beta0)))
cat(sprintf("integer median (simulated):        %.0f events\n", sim_median))
cat(sprintf("mean waiting time 1/beta:          %.3f events\n", 1 / beta0))
stopifnot(sim_median == k_median(beta0))

# --- why censoring matters ----------------------------------------------------

# A corpus stops at K_max opportunities. Gaps still unbridged there are
# censored: they tell us beta is small, but not how small. Work on a realistic
# sample size: a personal corpus yields hundreds to thousands of gaps, not
# hundreds of thousands.
censor_at <- 3
n_est <- 2000
g <- draws[seq_len(n_est)]
observed <- pmin(g, censor_at)
bridged <- g <= censor_at

naive <- mean(bridged)
cat(sprintf("\ncensored at K_max = %d, n = %d gaps\n", censor_at, n_est))
cat(sprintf("naive bridge share (biased):       %.4f  (true %.4f)\n", naive, beta0))

# Right-censored geometric likelihood:
#   bridged at g: (g - 1) * log(1 - beta) + log(beta)
#   censored at K: K * log(1 - beta)
negloglik <- function(beta) {
  if (beta <= 0 || beta >= 1) return(Inf)
  ll <- sum(ifelse(bridged,
                   (observed - 1) * log1p(-beta) + log(beta),
                   observed * log1p(-beta)))
  -ll
}
fit <- optimize(negloglik, interval = c(1e-6, 1 - 1e-6))
cat(sprintf("censored MLE:                      %.4f\n", fit$minimum))

# --- uncertainty: bootstrap over gaps ----------------------------------------

boot <- replicate(2000, {
  idx <- sample.int(n_est, n_est, replace = TRUE)
  o <- observed[idx]
  b <- bridged[idx]
  f <- optimize(function(beta) {
    if (beta <= 0 || beta >= 1) return(Inf)
    -sum(ifelse(b, (o - 1) * log1p(-beta) + log(beta), o * log1p(-beta)))
  }, interval = c(1e-6, 1 - 1e-6))
  f$minimum
})
ci <- quantile(boot, c(0.025, 0.975))
cat(sprintf("bootstrap 95%% CI for beta:         [%.4f, %.4f]\n", ci[[1]], ci[[2]]))
cat(sprintf("implied half-life CI:              [%.2f, %.2f] events\n",
            k_half(ci[[2]]), k_half(ci[[1]])))

# --- sensitivity: the constant is not a constant ------------------------------

cat("\n beta   half-life  integer median\n")
for (b in c(0.05, 0.10, 0.144, 0.20, 0.30)) {
  cat(sprintf(" %.3f  %9.2f  %14d\n", b, k_half(b), k_median(b)))
}
