const PRIORITY_PATTERN = /权利要求|发明内容|技术方案|实施方式|架构|机制|方法|实验|评估|结果|结论|安全|风险|architecture|mechanism|method|implementation|experiment|evaluation|result|conclusion|security|risk/iu;

function headingTitle(text, fallback) {
  const match = String(text).match(/^#{1,6}\s+(.+)$/mu);
  return String(match?.[1] || fallback).trim().replace(/--/gu, "-").slice(0, 120);
}

function splitByHeadings(markdown) {
  const matches = [...markdown.matchAll(/^#{1,6}\s+.+$/gmu)];
  if (matches.length < 4) return [];
  const starts = matches.map((match) => match.index);
  const sections = [];
  if (starts[0] > 0) starts.unshift(0);
  for (let index = 0; index < starts.length; index += 1) {
    const start = starts[index];
    const end = starts[index + 1] ?? markdown.length;
    const text = markdown.slice(start, end).trim();
    if (text) sections.push({ start, end, text, title: headingTitle(text, `片段 ${sections.length + 1}`) });
  }
  return sections;
}

function splitBySize(markdown, targetCharacters) {
  const sections = [];
  let start = 0;
  while (start < markdown.length) {
    let end = Math.min(markdown.length, start + targetCharacters);
    if (end < markdown.length) {
      const newline = markdown.lastIndexOf("\n", end);
      if (newline > start + Math.floor(targetCharacters * 0.6)) end = newline + 1;
    }
    const text = markdown.slice(start, end).trim();
    if (text) sections.push({ start, end, text, title: headingTitle(text, `连续片段 ${sections.length + 1}`) });
    start = Math.max(end, start + 1);
  }
  return sections;
}

function priorityScore(section) {
  let score = PRIORITY_PATTERN.test(section.title) ? 4 : 0;
  if (PRIORITY_PATTERN.test(section.text.slice(0, 1_000))) score += 2;
  if (/^#{1,3}\s/mu.test(section.text)) score += 1;
  return score;
}

function selectSectionIndexes(sections, targetCount) {
  const last = sections.length - 1;
  const selected = new Set([0, Math.min(1, last), Math.floor(last / 4), Math.floor(last / 2), Math.floor(last * 3 / 4), last]);
  const ranked = sections
    .map((section, index) => ({ index, score: priorityScore(section) }))
    .sort((left, right) => right.score - left.score || left.index - right.index);
  for (const item of ranked) {
    if (selected.size >= targetCount) break;
    if (item.score > 0) selected.add(item.index);
  }
  for (let index = 0; selected.size < targetCount && index < targetCount; index += 1) {
    selected.add(Math.round(index * last / Math.max(1, targetCount - 1)));
  }
  for (let index = 0; selected.size < targetCount && index < sections.length; index += 1) selected.add(index);
  return [...selected].filter((index) => index >= 0 && index < sections.length).sort((a, b) => a - b).slice(0, targetCount);
}

function clippedSource(text, budget) {
  if (text.length <= budget) return text;
  if (budget < 80) return text.slice(0, Math.max(0, budget));
  const omission = "\n\n[…原文中段省略…]\n\n";
  const available = budget - omission.length;
  const head = Math.ceil(available * 0.68);
  return `${text.slice(0, head)}${omission}${text.slice(text.length - (available - head))}`;
}

export function normalizeEvidenceText(value) {
  return String(value ?? "").normalize("NFKC").replace(/\s+/gu, " ").trim();
}

export function verifySourceQuote(quote, sourceText) {
  const normalizedQuote = normalizeEvidenceText(quote);
  if (normalizedQuote.length < 8) return false;
  return normalizeEvidenceText(sourceText).includes(normalizedQuote);
}

export function sampleMarkdownForAnalysis(value, options = {}) {
  const markdown = String(value ?? "");
  const maxCharacters = Math.max(1_000, Number(options.maxCharacters) || 60_000);
  let sections = splitByHeadings(markdown);
  if (!sections.length) sections = splitBySize(markdown, Math.max(2_000, Math.floor(maxCharacters / 8)));
  if (markdown.length <= maxCharacters) {
    return {
      markdown,
      metadata: {
        strategy: "full",
        sourceCharacters: markdown.length,
        analysisCharacters: markdown.length,
        selectedSourceCharacters: markdown.length,
        totalSections: sections.length,
        selectedSections: sections.length,
        coveragePositions: sections.map((section) => Number((section.start / Math.max(1, markdown.length)).toFixed(4))),
      },
    };
  }

  const maximumSelectedSections = Math.min(sections.length, 36);
  let targetCount = Math.min(sections.length, 10);
  let indexes = selectSectionIndexes(sections, targetCount);
  while (targetCount < maximumSelectedSections) {
    const selectedCharacters = indexes.reduce((sum, index) => sum + sections[index].text.length, 0);
    if (selectedCharacters >= maxCharacters * 0.94) break;
    targetCount += 1;
    indexes = selectSectionIndexes(sections, targetCount);
  }
  const markers = indexes.map((sectionIndex) => `<!-- intelifar-source-section ${sectionIndex + 1}/${sections.length}: ${sections[sectionIndex].title} -->\n`);
  const separatorCharacters = Math.max(0, indexes.length - 1) * 2;
  const sourceBudget = Math.max(indexes.length, maxCharacters - markers.reduce((sum, marker) => sum + marker.length, 0) - separatorCharacters);
  let remaining = sourceBudget;
  let selectedSourceCharacters = 0;
  const pieces = indexes.map((sectionIndex, position) => {
    const section = sections[sectionIndex];
    const remainingSections = indexes.length - position;
    const budget = Math.max(1, Math.floor(remaining / remainingSections));
    const source = clippedSource(section.text, budget);
    remaining -= source.length;
    selectedSourceCharacters += source.length;
    return `${markers[position]}${source}`;
  });
  const sampled = pieces.join("\n\n").slice(0, maxCharacters);
  return {
    markdown: sampled,
    metadata: {
      strategy: "section-balanced",
      sourceCharacters: markdown.length,
      analysisCharacters: sampled.length,
      selectedSourceCharacters,
      totalSections: sections.length,
      selectedSections: indexes.length,
      coveragePositions: indexes.map((index) => Number((sections[index].start / Math.max(1, markdown.length)).toFixed(4))),
    },
  };
}
