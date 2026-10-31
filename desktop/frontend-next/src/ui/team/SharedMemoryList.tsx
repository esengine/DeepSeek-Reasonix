import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { SharedMemory, TeamPort } from "../../port/team";
import { pushToast } from "../../state/toasts";

// What the team, or one group, remembers, as the agents wrote it. The person
// can make it forget one; that takes a second press, and nothing here edits a
// memory — agents keep them with remember_shared.
export function SharedMemoryList({ team, tier, group, revision, readOnly, label, empty }: {
  team: TeamPort;
  tier: "team" | "group";
  group?: string;
  revision: number;
  readOnly: boolean;
  label: string;
  empty: string;
}) {
  const [facts, setFacts] = useState<SharedMemory[] | null>(null);
  const [confirming, setConfirming] = useState("");
  useEffect(() => {
    let live = true;
    void team.sharedMemory(tier, group).then((x) => live && setFacts(x), (e) => live && pushToast(reason(e)));
    return () => { live = false; };
  }, [team, tier, group, revision]);
  if (!facts) return null;
  if (facts.length === 0) return <p className="team-note">{empty}</p>;
  const forget = async (name: string) => {
    if (confirming !== name) {
      setConfirming(name);
      return;
    }
    try {
      await team.forgetShared(tier, name, group);
      setFacts((cur) => (cur ?? []).filter((m) => m.name !== name));
    } catch (e) {
      pushToast(reason(e));
    }
    setConfirming("");
  };
  return (
    <ul className="team-shared" aria-label={label}>
      {facts.map((m) => (
        <li key={m.name}>
          <span className="team-shared-text">
            <b>{m.title || m.name}</b>
            <span>{m.description}</span>
          </span>
          {!readOnly && (
            <button type="button" data-action={confirming === m.name ? "team.forget-shared" : "team.forget-shared-ask"} data-target={m.name}
              aria-label={confirming === m.name ? t("确认忘掉 {name}", { name: m.name }) : t("忘掉 {name}", { name: m.name })} onClick={() => void forget(m.name)}>
              {confirming === m.name ? t("确认忘掉") : t("忘掉")}
            </button>
          )}
        </li>
      ))}
    </ul>
  );
}
