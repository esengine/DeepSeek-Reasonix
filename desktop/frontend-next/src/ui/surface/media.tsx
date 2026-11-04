import { useContext, useState } from "react";
import { Button, Modal, Stack, Text, UnstyledButton } from "@mantine/core";
import { SurfaceMediaContext } from "./media-context";
import type { SurfaceImage } from "../../port/wire";
import { imageSourceURL } from "./rich-spec";
import { t } from "../../i18n";


export function OptionImage({ image, fit = "cover" }: { image?: SurfaceImage; fit?: "cover" | "contain" }) {
  const port = useContext(SurfaceMediaContext);
  if (!image || !port?.workspaceImageURL) return null;
  const src = port.workspaceImageURL(image.path);
  return <PhotoCard key={src} src={src} image={image} fit={fit} />;
}

function SourceLink({ source }: { source?: string }) {
  const url = imageSourceURL(source);
  return url ? <a className="rich-option-source" href={url.href} target="_blank" rel="noopener noreferrer">{t("chat.image_source")} · {url.hostname}</a> : null;
}

function PhotoCard({ src, image, fit }: { src: string; image: SurfaceImage; fit: "cover" | "contain" }) {
  const [opened, setOpened] = useState(false);
  return <div>
    <MediaImage src={src} alt={image.alt} fit={fit} open={() => setOpened(true)} />
    <SourceLink source={image.source} />
    <Modal data-action-click="surface.explore" data-action-keydown="surface.explore" opened={opened} onClose={() => setOpened(false)} title={image.alt} size="xl" centered withinPortal={false} closeOnEscape={false} closeButtonProps={{ "aria-label": t("chat.close_image") }} onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); setOpened(false); } }}>
      <MediaImage key={opened ? "open" : "closed"} src={src} alt={image.alt} fit="contain" />
      <SourceLink source={image.source} />
    </Modal>
  </div>;
}

function MediaImage({ src, alt, fit, open }: { src: string; alt: string; fit: "cover" | "contain"; open?: () => void }) {
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState(false);
  const image = <img key={attempt} src={src} alt={alt} loading={open ? "lazy" : "eager"} decoding="async" style={{ objectFit: fit }} onError={() => setFailed(true)} />;
  if (failed) return <div className="rich-option-media"><Stack gap="xs" align="center"><Text size="sm" c="dimmed">{t("chat.image_unavailable_2")}</Text><Button variant="subtle" size="xs" data-action="surface.explore" onClick={() => { setAttempt(value => value + 1); setFailed(false); }}>{t("chat.retry_image")}</Button></Stack></div>;
  return open ? <UnstyledButton className="rich-option-media rich-photo-trigger" aria-label={t("chat.view_image_alt", { alt })} data-action="surface.explore" onClick={open}>{image}</UnstyledButton> : <div className="rich-option-media rich-photo-viewer">{image}</div>;
}
