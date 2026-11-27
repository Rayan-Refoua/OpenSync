const balanced = { minWave: .1, minFlux: .25, controlRatio: 1.5, controlMargin: .035, ambiguityRatio: .8, eventToleranceMS: 150 };
const verification = {
  strict: { ...balanced, minWave: .14, minFlux: .32, controlRatio: 1.65, controlMargin: .045 },
  balanced,
  loose: { ...balanced, minWave: .085, minFlux: .22 },
};

export const syncPresets = {
  event: { structure: "chronological", confidence: "balanced", effort: "balanced", timestamps: "estimate", review: "ambiguous" },
  takes: { structure: "clips", confidence: "strict", effort: "balanced", timestamps: "audio", review: "automatic" },
  difficult: { structure: "clips", confidence: "loose", effort: "deep", timestamps: "audio", review: "ambiguous" },
};

export function syncSetupAnswers(config) {
  const analysis = config.analysis || {};
  const current = analysis.verification || balanced;
  const confidence = Object.keys(verification).find(key =>
    Object.keys(balanced).every(field => current[field] === verification[key][field])
  ) || "custom";
  return {
    structure: config.mode || "clips", confidence,
    effort: analysis.recovery || "balanced",
    timestamps: config.fileTimePlacement === false ? "audio" : "estimate",
    review: config.sourceReview || "automatic",
  };
}

export function applySyncSetup(config, answers) {
  const analysis = { ...config.analysis, recovery: answers.effort };
  if (verification[answers.confidence]) analysis.verification = { ...verification[answers.confidence] };
  const next = {
    ...config, analysis,
    mode: answers.structure,
    fileTimePlacement: answers.structure === "chronological" && answers.timestamps === "estimate",
    sourceReview: answers.review,
  };
  if ("referenceSource" in answers) next.referenceSource = answers.referenceSource;
  if (answers.structure === "chronological" && answers.recorderOrder) {
    analysis.disableSearchShortcuts = answers.recorderOrder === "independent";
    analysis.guideRecorderOrder = answers.recorderOrder === "guided";
    if (analysis.disableSearchShortcuts) next.fileTimePlacement = false;
  }
  if (answers.uncertain) {
    next.queueUncertain = answers.uncertain === "queue";
    next.includeProvisional = false;
    if (answers.uncertain === "separate") next.unmatchedPolicy = "separate-sequence";
    if (answers.uncertain !== "separate" && config.unmatchedPolicy === "separate-sequence")
      next.unmatchedPolicy = "append";
  }
  if (answers.drift) {
    next.useOriginalAudio = answers.drift === "original";
    if (!next.useOriginalAudio) {
      next.audioEarlyMS = Number(answers.early);
      next.audioLateMS = Number(answers.late);
    }
  }
  return next;
}
