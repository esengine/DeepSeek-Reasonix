import { useCallback, useEffect, useMemo, useReducer } from "react";
import type { QuestionAnchor } from "./transcriptGrouping";
import type { TranscriptKernel } from "./transcriptKernel";
import { TranscriptNavigation } from "./transcriptNavigation";
import type { HistoryLoadTrigger } from "./useController";

export function useTranscriptHistoryNavigation({ kernel, requestOlder,
  loadingOlderHistory, running, loadedByTurn, jump,
}: {
  kernel: TranscriptKernel;
  requestOlder: (turn?: number, trigger?: HistoryLoadTrigger) => Promise<boolean>;
  loadingOlderHistory: boolean;
  running: boolean;
  loadedByTurn: ReadonlyMap<number, QuestionAnchor>;
  jump: (question: QuestionAnchor) => boolean;
}) {
  const navigation = useMemo(() => new TranscriptNavigation(kernel), [kernel]);
  const [, refresh] = useReducer((value: number) => value + 1, 0);
  const current = navigation.current;
  useEffect(() => () => {
    const request = navigation.current;
    if (!request) return;
    navigation.complete(request);
    kernel.cancelActive("question-navigation-unmounted");
  }, [kernel, navigation]);
  const navigate = useCallback((question: QuestionAnchor) => {
    const request = navigation.start(question);
    if (jump(question)) navigation.locate(request, refresh);
    refresh();
  }, [jump, navigation]);
  useEffect(() => {
    if (!navigation.owns(current) || current.status !== "pending") return;
    const loaded = loadedByTurn.get(current.question.turn);
    if (loaded && jump(loaded)) {
      navigation.locate(current, refresh);
      refresh();
      return;
    }
    if (loadingOlderHistory || running || current.attemptedPage === loadedByTurn) return;
    current.attemptedPage = loadedByTurn;
    void requestOlder(current.question.turn + 1, "question-jump").then((loadedPage) => {
      if (!navigation.owns(current)) return;
      if (!loadedPage) navigation.fail(current);
      refresh();
    });
  }, [current, jump, loadedByTurn, loadingOlderHistory, navigation, requestOlder, running]);
  const retry = useCallback(() => {
    const request = navigation.current;
    if (!request) { void requestOlder(undefined, "retry"); return; }
    request.status = "pending";
    request.attemptedPage = undefined;
    refresh();
  }, [navigation, requestOlder]);
  return { pendingQuestion: current && current.status !== "failed" ? current.question : null, navigate, requestOlder, retry };
}
