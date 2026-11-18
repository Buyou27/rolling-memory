// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

// Score regime calibration constants & named Shannon self-information bit-field laws.
const (
	// ReferencePeakLexScore is the reference peak lexical score used to derive the dimensionless alpha.
	ReferencePeakLexScore = 15.0

	// MinDimensionlessScaleFloor is the lower bound of the dimensionless scale factor.
	MinDimensionlessScaleFloor = 0.55

	// BaselineDirectXOHostScore is the reference score threshold for a compact direct-XO host band.
	BaselineDirectXOHostScore = 12.0

	// BaselineSubordinateBandScore is the reference floor for subordinate bands.
	BaselineSubordinateBandScore = 8.5

	// BaselineInverseEpisodeAnchorScore is the reference floor for inverse episode anchors.
	BaselineInverseEpisodeAnchorScore = 9.0

	// BaselinePeakBandConfidenceScore is the reference floor for peak band confidence.
	BaselinePeakBandConfidenceScore = 6.5

	// BaselineLeadCompetitionScore is the reference floor for lead competition.
	BaselineLeadCompetitionScore = 8.0

	// BaselineMultiTurnChainScore is the reference floor for multi-turn chains.
	BaselineMultiTurnChainScore = 6.0

	// BaselineUpwardContinuationScore is the reference floor for upward continuation.
	BaselineUpwardContinuationScore = 5.5

	// BaselineReverseReachScore is the reference floor for reverse reach.
	BaselineReverseReachScore = 5.0

	// BaselineStrongRowSignalScore is the reference threshold for strong row signals.
	BaselineStrongRowSignalScore = 4.5

	// BaselineCompactChainScore is the reference floor for compact chains.
	BaselineCompactChainScore = 4.0

	// BaselineIntraChainPurityScore is the reference floor for intra-chain purity.
	BaselineIntraChainPurityScore = 3.5

	// BaselineReachExpansionScore is the reference floor for reach expansion.
	BaselineReachExpansionScore = 3.0

	// CompactBandMaxRows is the maximum row count (12) for a band to be treated as a compact entity host.
	CompactBandMaxRows = 12
)

// ScoreRegime carries the calibrated floors actually used by band assembly and topology boosting.
type ScoreRegime struct {
	ObservedMaxScore          float64
	Alpha                     float64
	DirectXOHostFloor         float64
	InverseEpisodeAnchorFloor float64
	SubordinateBandFloor      float64
	LeadCompetitionFloor      float64
	PeakBandConfidenceFloor   float64
	MultiTurnChainFloor       float64
	UpwardContinuationFloor   float64
	ReverseReachFloor         float64
	StrongRowSignalFloor      float64
	CompactChainFloor         float64
	IntraChainPurityFloor     float64
	ReachExpansionFloor       float64
}

// Named Dimensionless Constants & Operators of the Shannon Self-Information Bit Field (Laws 1–4):
const (
	// PhaseHalfBandwidthBits (Delta_tau = 7/4 * h_lat = 3.5 bits) is the symmetric phase-transition
	// half-bandwidth centered at AtomicConceptEntropyBits (tau_atom = 8.0b):
	//   SingleAlternationMaxBits      = tau_atom - Delta_tau = 4.5b
	//   OverStemmingDivergenceMaxBits = tau_atom + Delta_tau = 11.5b
	PhaseHalfBandwidthBits = 3.5

	// IntraLexemeMIDecayKappa0 (kappa_0 = log2(3/2) ≈ 0.5849625) is the universal dimensionless
	// power-law decay constant governing conditional entropy H_cond(r_m | r_{1..m-1}) = H_0(r_m) * m^(-lambda(r_m))
	// across continuous ideographic and syllabic compounds (Law 2).
	IntraLexemeMIDecayKappa0 = 0.5849625007211562
)
