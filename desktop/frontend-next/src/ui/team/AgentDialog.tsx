import { useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { AgentEdit, TeamAgent, TeamPort, TeamView } from "../../port/team";
import { StudioIcon } from "../StudioIcon";
import { AVATAR_COLORS, AVATAR_GLYPHS, AgentAvatar, avatarOf, glyphName } from "./AgentAvatar";
import { PERMISSIONS } from "./permissions";
import { cropPhoto, PhotoError } from "./photo";
import { TeamDialog } from "./TeamDialog";


// Creating an agent, or changing one. Only the person reaches the permission,
// main and delete controls: no agent opens this dialog.
export function AgentDialog({ team, agent, onClose, onSaved }: {
  team: TeamPort;
  agent?: TeamAgent;
  onClose: () => void;
  onSaved: (view: TeamView) => void;
}) {
  const start = agent ? avatarOf(agent.id, agent.avatar, agent.main) : { glyph: AVATAR_GLYPHS[1], color: AVATAR_COLORS[0] };
  const [name, setName] = useState(agent?.name ?? "");
  const [role, setRole] = useState(agent?.role ?? "");
  const [instructions, setInstructions] = useState(agent?.instructions ?? "");
  const [glyph, setGlyph] = useState(start.glyph);
  const [color, setColor] = useState(start.color);
  const [permission, setPermission] = useState(agent?.permission ?? "");
  const [main, setMain] = useState(!!agent?.main);
  const [pinned, setPinned] = useState(!!agent?.pinned);
  const [hidden, setHidden] = useState(!!agent?.hidden);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [error, setError] = useState("");
  // undefined leaves the photo as it is, null takes it off, a data URL replaces it.
  const [photo, setPhoto] = useState<string | null | undefined>(undefined);
  const worn = photo === undefined ? agent?.avatar.image : photo ?? undefined;

  const run = async (write: () => Promise<TeamView>) => {
    setBusy(true);
    setError("");
    try {
      onSaved(await write());
    } catch (e) {
      setError(reason(e));
      setBusy(false);
    }
  };
  const save = () => {
    const edit: AgentEdit = { name: name.trim(), role: role.trim(), instructions, avatar: { glyph, color } };
    if (!agent) {
      return run(async () => {
        const next = await team.createAgent(edit);
        const made = photo ? next.agents.find((a) => a.name === edit.name) : undefined;
        return made && photo ? team.setPhoto(made.id, photo) : next;
      });
    }
    return run(async () => {
      const next = await team.updateAgent(agent.id, { ...edit, permission, main, pinned, hidden });
      if (photo === undefined) return next;
      return photo ? team.setPhoto(agent.id, photo) : team.clearPhoto(agent.id);
    });
  };
  const pickPhoto = async (file: File | undefined) => {
    if (!file) return;
    setError("");
    try {
      setPhoto(await cropPhoto(file));
    } catch (e) {
      setError(e instanceof PhotoError && e.reason === "type" ? t("只能用 PNG、JPEG、WebP 或 GIF 图片") : t("这张图片太大了"));
    }
  };
  const shuffle = () => {
    setGlyph(AVATAR_GLYPHS[Math.floor(Math.random() * AVATAR_GLYPHS.length)]);
    setColor(AVATAR_COLORS[Math.floor(Math.random() * AVATAR_COLORS.length)]);
  };

  return (
    <TeamDialog title={agent ? t("Agent 设置") : t("新建 Agent")} busy={busy} onClose={onClose} onSubmit={() => void save()} target={agent?.id}
      footer={<>
        {agent && (
          <button type="button" className="team-danger" data-action={confirmDelete ? "team.agent-delete" : "team.agent-delete-ask"} data-target={agent.id} disabled={busy}
            onClick={() => (confirmDelete ? void run(() => team.deleteAgent(agent.id)) : setConfirmDelete(true))}>
            {confirmDelete ? t("确认删除") : t("删除")}
          </button>
        )}
        <button type="button" data-action="layer.dismiss" disabled={busy} onClick={onClose}>{t("取消")}</button>
        <button type="submit" className="team-save" disabled={busy || !name.trim()}>
          {agent ? t("保存") : t("创建")}
        </button>
      </>}>
      <div className="team-look" role="group" aria-label={t("头像")}>
        <AgentAvatar agent={{ id: agent?.id ?? "new", avatar: { glyph, color, image: worn } }} size={40} badges={false} />
        <div className="team-look-pick">
          <div className="team-look-row" role="group" aria-label={t("形状")}>
            {AVATAR_GLYPHS.map((g) => (
              <button key={g} type="button" data-action="team.avatar-glyph" data-value={g} aria-pressed={g === glyph} aria-label={glyphName(g)} title={glyphName(g)} onClick={() => setGlyph(g)}>
                <AgentAvatar agent={{ id: g, avatar: { glyph: g, color } }} size={24} badges={false} />
              </button>
            ))}
          </div>
          <div className="team-look-row" role="group" aria-label={t("颜色")}>
            {AVATAR_COLORS.map((c, i) => (
              <button key={c} type="button" data-action="team.avatar-color" data-value={c} aria-pressed={c === color} aria-label={t("颜色 {n}", { n: i + 1 })} onClick={() => setColor(c)}>
                <span className="team-swatch" data-color={c} />
              </button>
            ))}
            <button type="button" data-action="team.avatar-shuffle" aria-label={t("随机")} title={t("随机")} onClick={shuffle}>
              <StudioIcon name="refresh" />
            </button>
          </div>
          <div className="team-look-photo">
            <label className="team-photo-pick">
              <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" data-action-change="team.photo-pick"
                onChange={(ev) => { void pickPhoto(ev.target.files?.[0]); ev.target.value = ""; }} />
              {t("上传照片")}
            </label>
            {worn && (
              <button type="button" className="team-photo-remove" data-action="team.photo-remove" onClick={() => setPhoto(null)}>
                {t("移除照片")}
              </button>
            )}
          </div>
        </div>
      </div>
      {agent?.usage && agent.usage.requests > 0 && (
        <p className="team-note">
          {t("用量：输入 {prompt} · 输出 {completion} tokens · 缓存命中 {hit}%", {
            prompt: agent.usage.promptTokens.toLocaleString(),
            completion: agent.usage.completionTokens.toLocaleString(),
            hit: agent.usage.promptTokens ? Math.round((agent.usage.cacheHitTokens / agent.usage.promptTokens) * 100) : 0,
          })}
        </p>
      )}
      <label>{t("名字")}
        <input data-action="team.agent-name" value={name} maxLength={40} autoFocus onChange={(ev) => setName(ev.target.value)} />
      </label>
      <label>{t("角色")}
        <input data-action="team.agent-role" value={role} maxLength={24} placeholder={t("例如：写手")} onChange={(ev) => setRole(ev.target.value)} />
      </label>
      <label>{t("指令")}
        <textarea data-action="team.agent-instructions" value={instructions} placeholder={t("这个 Agent 长期遵循的工作方式")} onChange={(ev) => setInstructions(ev.target.value)} />
      </label>
      {agent && (
        <>
          <label>{t("权限")}
            <select data-action-change="team.agent-permission" data-target={agent.id} value={permission} onChange={(ev) => setPermission(ev.target.value)}>
              {PERMISSIONS.map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}
            </select>
          </label>
          <label className="team-check">
            <input type="checkbox" data-action="team.agent-main" data-target={agent.id} checked={main} onChange={(ev) => setMain(ev.target.checked)} />
            {t("主 Agent：可以创建、修改和停止其他 Agent，管理所有群聊")}
          </label>
          <label className="team-check">
            <input type="checkbox" data-action="team.agent-pin" data-target={agent.id} checked={pinned} onChange={(ev) => setPinned(ev.target.checked)} />
            {t("置顶")}
          </label>
          <label className="team-check">
            <input type="checkbox" data-action="team.agent-hide" data-target={agent.id} checked={hidden} onChange={(ev) => setHidden(ev.target.checked)} />
            {t("在侧栏隐藏")}
          </label>
        </>
      )}
      {error && <p className="team-error" role="alert">{error}</p>}
    </TeamDialog>
  );
}
