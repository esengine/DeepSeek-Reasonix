import {
  findRegexCodeMatches,
  type RegexSearchRequest,
  type RegexSearchResponse,
} from "./codeSearch";

const workerScope = globalThis as unknown as {
  onmessage: ((event: MessageEvent<RegexSearchRequest>) => void) | null;
  postMessage: (response: RegexSearchResponse) => void;
};

workerScope.onmessage = (event) => {
  workerScope.postMessage(findRegexCodeMatches(event.data));
};
