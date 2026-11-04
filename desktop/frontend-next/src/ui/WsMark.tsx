import { StudioIcon } from "./StudioIcon";

export function WsMark({ open }: { open: boolean | undefined }) {
  return (
    <>
      <StudioIcon name="down" className="wschev" data-open={open ? "" : undefined} data-leaf={open === undefined ? "" : undefined} />
      <StudioIcon name="folder" className="wsfolder" />
    </>
  );
}
