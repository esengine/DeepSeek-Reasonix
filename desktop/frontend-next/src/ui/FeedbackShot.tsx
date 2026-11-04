import { t } from "../i18n";
import { ImagePreview } from "./ImagePreview";
import { StudioIcon } from "./StudioIcon";
import type { Shot } from "./feedbackshots";

export function FeedbackShot({ shot, disabled, onRemove }: { shot: Shot; disabled?: boolean; onRemove: () => void }) {
  return (
    <li>
      <ImagePreview src={shot.preview} name={shot.name} className="fbk-shot-view">
        <img src={shot.preview} alt={shot.name} />
      </ImagePreview>
      <span className="fbk-shot-name">{shot.name}</span>
      <button type="button" className="fbk-shot-x" data-action="feedback.shot.remove" data-target={shot.id} disabled={disabled}
        aria-label={t("feedback.remove_screenshot_name", { name: shot.name })} onClick={onRemove}>
        <StudioIcon name="close" />
      </button>
    </li>
  );
}
