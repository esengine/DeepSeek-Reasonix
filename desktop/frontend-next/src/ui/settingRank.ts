/** The words a setting can be found by, already put in the reader's language. */
export interface SettingTerms {
  titles: string[];
  sections: string[];
  keywords: string[];
}

const CJK = /[㐀-鿿]/;

export const normalize = (s: string): string => s.normalize("NFKC").toLowerCase().trim();

const bigrams = (s: string): Set<string> => {
  const out = new Set<string>();
  const chars = [...s].filter((c) => CJK.test(c));
  for (let i = 0; i + 1 < chars.length; i++) out.add(chars[i] + chars[i + 1]);
  return out;
};

/** How well one word of a query finds a setting, 0 for not at all. Three ways
 *  in, strongest first: the word is (part of) one of the setting's names; the
 *  word contains names it knows, which is how people type a sentence; the word
 *  shares most of its character pairs with a name, which survives a verb or a
 *  particle the catalogue does not use. */
function wordScore(terms: SettingTerms, raw: string): number {
  const word = normalize(raw);
  if (!word) return 0;
  const fields: [string, number][] = [
    ...terms.titles.map((x): [string, number] => [normalize(x), 10]),
    ...terms.keywords.map((x): [string, number] => [normalize(x), 0]),
    ...terms.sections.map((x): [string, number] => [normalize(x), -40]),
  ].filter(([x]) => x.length > 0);

  let best = 0;
  for (const [name, bonus] of fields) {
    let s = 0;
    if (name === word) s = 100;
    else if (name.startsWith(word)) s = 85;
    else if (name.includes(word)) s = 70;
    if (s) best = Math.max(best, s + bonus);
  }

  const covered = new Set<number>();
  let hits = 0;
  let nameBonus = 0;
  for (const [name, bonus] of fields) {
    if (name.length < 2 || bonus < 0) continue;
    for (let from = word.indexOf(name); from !== -1; from = word.indexOf(name, from + 1)) {
      for (let i = from; i < from + name.length; i++) covered.add(i);
      hits++;
    }
    if (word.includes(name)) nameBonus = Math.max(nameBonus, bonus);
  }
  if (hits > 0) best = Math.max(best, 30 + Math.round(50 * (covered.size / word.length)) + nameBonus);

  const pairs = bigrams(word);
  if (pairs.size >= 2) {
    const matched = new Set<string>();
    for (const [name, bonus] of fields) {
      if (bonus < 0) continue;
      const mine = bigrams(name);
      for (const p of pairs) if (mine.has(p)) matched.add(p);
    }
    const share = matched.size / pairs.size;
    // The pairs a name shares are weaker evidence than the name itself, so
    // they break a tie between settings that found the word in the same way.
    if (best > 0) best += Math.round(20 * share);
    else if (share >= 0.5) best = 20 + Math.round(40 * share);
  }
  return best;
}

/** How well a query finds a setting. Every word of the query has to find it;
 *  the score adds up what each word found. 0 means it is not a match. */
export function rankTerms(terms: SettingTerms, query: string): number {
  const words = normalize(query).split(/[\s,，、]+/).filter(Boolean);
  if (words.length === 0) return 0;
  let total = 0;
  for (const word of words) {
    const s = wordScore(terms, word);
    if (s === 0) return 0;
    total += s;
  }
  return total;
}
