// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode } from "react";
import type { BorderStyleName } from "../layout/borders.js";
import { Box as RsxBox } from "../react/components.js";
import { fgCode } from "./colors.js";

export interface InkBoxProps {
  readonly children?: ReactNode;
  readonly flexDirection?: "row" | "column";
  readonly flexGrow?: number;
  readonly flexShrink?: number;
  readonly padding?: number;
  readonly paddingX?: number;
  readonly paddingY?: number;
  readonly paddingTop?: number;
  readonly paddingBottom?: number;
  readonly paddingLeft?: number;
  readonly paddingRight?: number;
  readonly margin?: number;
  readonly marginX?: number;
  readonly marginY?: number;
  readonly marginTop?: number;
  readonly marginBottom?: number;
  readonly marginLeft?: number;
  readonly marginRight?: number;
  readonly borderStyle?: BorderStyleName;
  readonly borderColor?: string;
  readonly borderTop?: boolean;
  readonly borderBottom?: boolean;
  readonly borderLeft?: boolean;
  readonly borderRight?: boolean;
  readonly width?: number | string;
  readonly height?: number | string;
  readonly minWidth?: number;
  readonly justifyContent?: "flex-start" | "flex-end" | "center" | "space-between";
  readonly alignItems?: "flex-start" | "flex-end" | "center" | "stretch";
  readonly gap?: number;
}

export function Box(props: InkBoxProps): React.ReactElement {
  const padTop = pickPad(props.paddingTop, props.paddingY, props.padding);
  const padBottom = pickPad(props.paddingBottom, props.paddingY, props.padding);
  const padLeft = pickPad(props.paddingLeft, props.paddingX, props.padding);
  const padRight = pickPad(props.paddingRight, props.paddingX, props.padding);
  const marginTop = pickPad(props.marginTop, props.marginY, props.margin);
  const marginBottom = pickPad(props.marginBottom, props.marginY, props.margin);
  const marginLeft = pickPad(props.marginLeft, props.marginX, props.margin);
  const marginRight = pickPad(props.marginRight, props.marginX, props.margin);

  const borderColor = fgCode(props.borderColor);
  const inner = (
    <RsxBox
      flexDirection={props.flexDirection}
      flexGrow={marginTop || marginBottom || marginLeft || marginRight ? undefined : props.flexGrow}
      justifyContent={props.justifyContent}
      width={typeof props.width === "number" ? props.width : undefined}
      height={typeof props.height === "number" ? props.height : undefined}
      paddingTop={padTop}
      paddingBottom={padBottom}
      paddingLeft={padLeft}
      paddingRight={padRight}
      borderStyle={props.borderStyle}
      borderColor={borderColor ? [borderColor] : undefined}
      borderTop={props.borderTop}
      borderBottom={props.borderBottom}
      borderLeft={props.borderLeft}
      borderRight={props.borderRight}
    >
      {props.children}
    </RsxBox>
  );

  if (!(marginTop || marginBottom || marginLeft || marginRight)) return inner;

  return (
    <RsxBox
      flexGrow={props.flexGrow}
      paddingTop={marginTop}
      paddingBottom={marginBottom}
      paddingLeft={marginLeft}
      paddingRight={marginRight}
    >
      {inner}
    </RsxBox>
  );
}

function pickPad(...values: ReadonlyArray<number | undefined>): number {
  for (const v of values) if (v !== undefined) return v;
  return 0;
}
