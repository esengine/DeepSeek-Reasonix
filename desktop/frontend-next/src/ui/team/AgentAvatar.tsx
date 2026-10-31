import { useState, type CSSProperties } from "react";
import { t } from "../../i18n";
import type { TeamActivity, TeamAvatar } from "../../port/team";
import "../../styles/team-motion.css";

// Each glyph is drawn on a 24-unit square with room for two eyes at y=12.
const GLYPHS: Record<string, string> = {
  pebble: "M12 3a9 9 0 1 1 0 18 9 9 0 0 1 0-18z",
  squircle: "M7 3h10a4 4 0 0 1 4 4v10a4 4 0 0 1-4 4H7a4 4 0 0 1-4-4V7a4 4 0 0 1 4-4z",
  whale: "M2 13c0-5 4.5-8 10-8s10 3 10 8c0 4-3.5 6-8 6H9c-4 0-7-2-7-6zM17 5c.5-2 3-3 4.5-2-1.5.5-2 1.5-2 3z",
  star: "M12 2l2.9 6.2 6.8.8-5 4.6 1.3 6.7L12 17l-6 3.3 1.3-6.7-5-4.6 6.8-.8z",
  moon: "M14 3a9 9 0 1 0 7 14A8 8 0 0 1 14 3z",
  bunny: "M8 2c1.5 0 2 3 2 6h4c0-3 .5-6 2-6s2 4 1 7c2 1 3 3 3 6 0 4-4 7-8 7s-8-3-8-7c0-3 1-5 3-6-1-3-.5-7 1-7z",
  kitty: "M4 3l4 4h8l4-4v11c0 4-4 7-8 7s-8-3-8-7z",
  gem: "M7 3h10l4 6-9 12L3 9z",
  hex: "M12 2l9 5v10l-9 5-9-5V7z",
  cushion: "M5 4c4-2 10-2 14 0 2 4 2 12 0 16-4 2-10 2-14 0-2-4-2-12 0-16z",
  saucer: "M12 5c3 0 5 2 5 4.5 3 .5 5 2 5 3.5C22 15.5 17.5 18 12 18S2 15.5 2 13c0-1.5 2-3 5-3.5C7 7 9 5 12 5z",
  peanut: "M12 2c3 0 5 2 5 5 0 1.5-.8 2.6-1.6 3.5.8.9 1.6 2 1.6 3.5 0 3-2 5-5 5s-5-2-5-5c0-1.5.8-2.6 1.6-3.5C7.8 9.6 7 8.5 7 7c0-3 2-5 5-5z",
};

export const AVATAR_GLYPHS = Object.keys(GLYPHS);
export const AVATAR_COLORS = ["cat-1", "cat-2", "cat-3", "cat-4", "cat-5"];

/** What a picker calls a glyph; a colour is named by its place, since its hue follows the theme. */
export function glyphName(glyph: string): string {
  const names: Record<string, string> = {
    pebble: t("圆石"), squircle: t("圆角方块"), whale: t("鲸鱼"), star: t("星星"), moon: t("月亮"), bunny: t("兔子"),
    kitty: t("猫咪"), gem: t("宝石"), hex: t("六边形"), cushion: t("软垫"), saucer: t("飞碟"), peanut: t("花生"),
  };
  return names[glyph] ?? glyph;
}

// A look nobody picked is derived from the id, so it stays the same everywhere
// the agent is drawn and across restarts.
function hash(id: string): number {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h;
}

export function avatarOf(id: string, avatar: TeamAvatar, main = false): { glyph: string; color: string } {
  const glyph = avatar.glyph && GLYPHS[avatar.glyph] ? avatar.glyph : main ? "whale" : AVATAR_GLYPHS[hash(id) % AVATAR_GLYPHS.length];
  const color = avatar.color && AVATAR_COLORS.includes(avatar.color) ? avatar.color : AVATAR_COLORS[hash(id + "c") % AVATAR_COLORS.length];
  return { glyph, color };
}

export interface AvatarSubject {
  id: string;
  avatar: TeamAvatar;
  main?: boolean;
  activity?: TeamActivity;
}

// An agent's mark. Badges say what the sidebar row needs at a glance: main or
// admin, working, or waiting on the person.
export function AgentAvatar({ agent, size = 28, star = agent.main, badges = true }: { agent: AvatarSubject; size?: number; star?: boolean; badges?: boolean }) {
  const { glyph, color } = avatarOf(agent.id, agent.avatar, agent.main);
  const state = agent.activity?.state ?? "idle";
  const [broken, setBroken] = useState<string | null>(null);
  const photo = agent.avatar.image && agent.avatar.image !== broken ? agent.avatar.image : undefined;
  return (
    <span className="team-avatar" data-color={color} data-state={badges ? state : "idle"} data-photo={photo ? "" : undefined} style={{ "--avatar-size": `${size}px`, "--avatar-phase": `${hash(agent.id) % 9000}ms` } as CSSProperties} aria-hidden="true">
      {photo ? (
        <img src={photo} alt="" draggable={false} onError={() => setBroken(photo)} />
      ) : (
        <svg viewBox="0 0 24 24">
          <path d={GLYPHS[glyph]} />
          <circle className="team-avatar-eye" cx="9.5" cy="12" r="1.3" />
          <circle className="team-avatar-eye" cx="14.5" cy="12" r="1.3" />
        </svg>
      )}
      {badges && star && <span className="team-avatar-star"><svg viewBox="0 0 10 10"><path d="M5 .5l1.4 2.9 3.1.4-2.3 2.2.6 3.1L5 7.6 2.2 9.1l.6-3.1L.5 3.8l3.1-.4z" /></svg></span>}
      {badges && state === "working" && <span className="team-avatar-dot" />}
      {badges && state === "needs_you" && <span className="team-avatar-ask">?</span>}
    </span>
  );
}

// Several members drawn as one overlapping cluster, for a group's row.
export function GroupAvatar({ members, size = 28 }: { members: AvatarSubject[]; size?: number }) {
  const shown = members.slice(0, 3);
  return (
    <span className="team-cluster" data-count={shown.length} style={{ "--avatar-size": `${size}px` } as CSSProperties} aria-hidden="true">
      {shown.map((m) => (
        <AgentAvatar key={m.id} agent={m} size={Math.round(size * 0.62)} badges={false} />
      ))}
    </span>
  );
}
