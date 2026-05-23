import {
  type LifecycleIntentRecommendation,
  type LifecycleIntentSignal,
  evaluateEngineeringLifecycleIntent,
} from "./lifecycle-intent.js";

export interface LifecycleIntentCorpusCase {
  id: string;
  input: string;
  expectedRecommendation: LifecycleIntentRecommendation;
  expectedSignals: LifecycleIntentSignal[];
  language?: "en" | "zh" | "mixed";
  tags?: string[];
}

export interface LifecycleIntentCorpusResult {
  id: string;
  passed: boolean;
  expectedRecommendation: LifecycleIntentRecommendation;
  actualRecommendation: LifecycleIntentRecommendation;
  expectedSignals: LifecycleIntentSignal[];
  actualSignals: LifecycleIntentSignal[];
  missingSignals: LifecycleIntentSignal[];
  modelVisible: false;
  enforced: false;
}

export interface LifecycleIntentCorpusReport {
  total: number;
  passed: number;
  failed: number;
  recommendationAccuracy: number;
  signalRecall: number;
  results: LifecycleIntentCorpusResult[];
}

export function evaluateLifecycleIntentCorpus(
  cases: readonly LifecycleIntentCorpusCase[],
): LifecycleIntentCorpusReport {
  const results = cases.map(evaluateCase);
  const passed = results.filter((item) => item.passed).length;
  const expectedSignalCount = results.reduce((sum, item) => sum + item.expectedSignals.length, 0);
  const missingSignalCount = results.reduce((sum, item) => sum + item.missingSignals.length, 0);

  return {
    total: results.length,
    passed,
    failed: results.length - passed,
    recommendationAccuracy: ratio(
      results.filter((item) => item.expectedRecommendation === item.actualRecommendation).length,
      results.length,
    ),
    signalRecall: ratio(expectedSignalCount - missingSignalCount, expectedSignalCount),
    results,
  };
}

export function formatLifecycleIntentReport(report: LifecycleIntentCorpusReport): string {
  const accuracy = formatPercent(report.recommendationAccuracy);
  const recall = formatPercent(report.signalRecall);
  return `lifecycle intent corpus: ${report.passed}/${report.total} passed; recommendation=${accuracy}; signal-recall=${recall}`;
}

function evaluateCase(testCase: LifecycleIntentCorpusCase): LifecycleIntentCorpusResult {
  const actual = evaluateEngineeringLifecycleIntent(testCase.input);
  const missingSignals = testCase.expectedSignals.filter(
    (signal) => !actual.signals.includes(signal),
  );
  return {
    id: testCase.id,
    passed:
      testCase.expectedRecommendation === actual.recommendation && missingSignals.length === 0,
    expectedRecommendation: testCase.expectedRecommendation,
    actualRecommendation: actual.recommendation,
    expectedSignals: [...testCase.expectedSignals],
    actualSignals: [...actual.signals],
    missingSignals,
    modelVisible: actual.modelVisible,
    enforced: actual.enforced,
  };
}

function ratio(numerator: number, denominator: number): number {
  if (denominator === 0) return 1;
  return numerator / denominator;
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}
