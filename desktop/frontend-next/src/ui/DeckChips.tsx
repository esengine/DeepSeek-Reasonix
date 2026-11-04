import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { JobEntry, JobOutput } from "../port/port";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { agentsIn } from "./panels/derive";
import { AgentDeck } from "./AgentDeck";
import { AgentTranscript } from "./panels/AgentTranscript";
import { JobDeck } from "./JobDeck";
import { JobOutputView } from "./panels/JobOutputView";
import type { Task } from "./panels/Agents";
import { useDismiss } from "./dismiss";

export type Deck = "" | "agents" | "jobs";

/** What is running on this session's behalf but is not in the transcript: work
 *  handed to a sub-agent, and processes left running in the background. The
 *  readings sit at the right end of the run rail, beside the other numbers
 *  about this turn, and each opens on hover the way the context reading does —
 *  one gesture for every summary on this row. */
export function DeckChips({ tasks, jobs, open, onOpen, onCancelJob, onReadJob, onOpenRun, onStopCall, onClearJobs, onOpenJob }: { tasks: Task[]; jobs: JobEntry[]; open: Deck; onOpen: (next: (was: Deck) => Deck) => void; onCancelJob?: (id: string) => Promise<void>; onReadJob?: (id: string) => Promise<JobOutput>; onOpenRun?: (call: string) => Promise<boolean>; onStopCall?: (callId: string) => void; onClearJobs?: (ids: string[]) => void; onOpenJob?: (job: JobEntry) => void }) {
  const agentsBox = useRef<HTMLDivElement>(null);
  const jobsBox = useRef<HTMLDivElement>(null);
  const [viewing, setViewing] = useState("");
  const [viewingJob, setViewingJob] = useState("");
  const viewed = viewing ? tasks.find((x) => x.id === viewing) : undefined;
  useEffect(() => {
    if (viewing && !viewed) setViewing("");
  }, [viewing, viewed]);
  const closeView = useCallback(() => {
    setViewing("");
    agentsBox.current?.querySelector<HTMLElement>('[data-action="deck.agents"]')?.focus();
  }, []);
  const shut = useCallback(() => onOpen(() => ""), [onOpen]);
  const openTask = async (id: string) => {
    const call = tasks.find((x) => x.id === id)?.tool.id;
    if (call && onOpenRun && (await onOpenRun(call))) return;
    setViewing(id);
  };
  useDismiss(open === "agents", agentsBox, shut);
  useDismiss(open === "jobs", jobsBox, shut);
  const liveAgents = useMemo(() => agentsIn(tasks.filter((x) => x.running)), [tasks]);
  const liveJobs = useMemo(() => jobs.filter((j) => j.status === "running").length, [jobs]);
  // A failure stays loud until the list has been opened on it.
  const [seenFailed, setSeenFailed] = useState<ReadonlySet<string>>(() => new Set());
  const failedJobs = useMemo(() => jobs.filter((j) => j.status === "failed" && !seenFailed.has(j.id)).length, [jobs, seenFailed]);
  useEffect(() => {
    if (open !== "jobs") return;
    const failed = jobs.filter((j) => j.status === "failed" && !seenFailed.has(j.id));
    if (failed.length) setSeenFailed((was) => new Set([...was, ...failed.map((j) => j.id)]));
  }, [open, jobs, seenFailed]);
  if (tasks.length === 0 && jobs.length === 0) return null;

  return (
    <>
      {tasks.length > 0 && (
        <div ref={agentsBox} className="studio-deck-anchor" data-open={open === "agents" ? "" : undefined}>
          <button
            type="button"
            className="studio-deckchip"
            data-action="deck.agents"
            aria-label={t("common.subagents")}
            title={t("common.subagents")}
            data-live={liveAgents ? "" : undefined}
            aria-expanded={open === "agents"}
            aria-haspopup="dialog"
            onClick={() => onOpen((was) => (was === "agents" ? "" : "agents"))}
          >
            <StudioIcon name="branch" />
            {liveAgents > 0 && <i className="agent-live" aria-hidden="true" />}
            <b>{liveAgents || agentsIn(tasks)}</b>
            <span>{liveAgents ? t("subagent.subagents_running") : t("subagent.subagents_all_finished")}</span>
          </button>
          <div className="studio-deck-pop" role="dialog" aria-label={t("common.subagents")}>
            <AgentDeck tasks={tasks} onStop={onStopCall} onOpen={(id) => { shut(); void openTask(id); }} />
          </div>
        </div>
      )}
      {viewed && <AgentTranscript task={viewed} onClose={closeView} />}
      {viewingJob && onReadJob && jobs.some((j) => j.id === viewingJob) && (
        <JobOutputView job={jobs.find((j) => j.id === viewingJob)!} read={onReadJob} onClose={() => setViewingJob("")} onCancel={onCancelJob} />
      )}
      {jobs.length > 0 && (
        <div ref={jobsBox} className="studio-deck-anchor" data-open={open === "jobs" ? "" : undefined}>
          <button
            type="button"
            className="studio-deckchip"
            data-action="deck.jobs"
            aria-label={t("common.background_jobs")}
            title={t("common.background_jobs")}
            data-live={liveJobs ? "" : undefined}
            data-failed={!liveJobs && failedJobs ? "" : undefined}
            aria-expanded={open === "jobs"}
            aria-haspopup="dialog"
            onClick={() => onOpen((was) => (was === "jobs" ? "" : "jobs"))}
          >
            {liveJobs > 0 && <i className="agent-live" aria-hidden="true" />}
            <StudioIcon name="play" />
            <b>{liveJobs || failedJobs || jobs.length}</b>
            <span>{liveJobs ? t("subagent.background_jobs_running") : failedJobs ? t("subagent.background_jobs_failed") : t("subagent.background_jobs_done")}</span>
          </button>
          <div className="studio-deck-pop" role="dialog" aria-label={t("common.background_jobs")}>
            <JobDeck jobs={jobs} onCancel={onCancelJob} onClear={onClearJobs} onView={onOpenJob || onReadJob ? (id) => { shut(); const job = jobs.find((j) => j.id === id); if (onOpenJob && job) onOpenJob(job); else setViewingJob(id); } : undefined} />
          </div>
        </div>
      )}
    </>
  );
}
