// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode } from "react";
import type { BorderStyle, BorderStyleName } from "../layout/borders.js";
import type { AlignItems, FlexWrap, JustifyContent } from "../layout/node.js";
import { Box as RsxBox } from "../react/components.js";
import { fgCode } from "./colors.js";

export interface InkBoxProps {
  readonly children?: ReactNode;
  readonly flexDirection?: "column" | "row" | "column-reverse" | "row-reverse";
  readonly flexGrow?: number;
  readonly flexShrink?: number;
  readonly flexBasis?: number | `${number}%` | "auto";
  readonly flexWrap?: FlexWrap;
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
  readonly borderStyle?: BorderStyle | BorderStyleName;
  readonly borderColor?: string;
  readonly borderTopColor?: string;
  readonly borderBottomColor?: string;
  readonly borderLeftColor?: string;
  readonly borderRightColor?: string;
  readonly borderTop?: boolean;
  readonly borderBottom?: boolean;
  readonly borderLeft?: boolean;
  readonly borderRight?: boolean;
  readonly width?: number | `${number}%`;
  readonly height?: number | `${number}%`;
  readonly minWidth?: number | `${number}%`;
  readonly minHeight?: number | `${number}%`;
  readonly maxWidth?: number | `${number}%`;
  readonly maxHeight?: number | `${number}%`;
  readonly justifyContent?: JustifyContent;
  readonly alignItems?: AlignItems;
  readonly alignSelf?: AlignItems;
  readonly alignContent?: AlignItems | "space-between" | "space-around";
  readonly gap?: number;
  readonly columnGap?: number;
  readonly rowGap?: number;
  readonly display?: "flex" | "none";
}

export function Box(props: InkBoxProps): React.ReactElement {
  const borderColor = fgCode(props.borderColor);
  const borderTopColor = fgCode(props.borderTopColor);
  const borderBottomColor = fgCode(props.borderBottomColor);
  const borderLeftColor = fgCode(props.borderLeftColor);
  const borderRightColor = fgCode(props.borderRightColor);
  return (
    <RsxBox
      flexDirection={props.flexDirection ?? "row"}
      flexGrow={props.flexGrow}
      flexShrink={props.flexShrink}
      flexBasis={props.flexBasis}
      flexWrap={props.flexWrap}
      justifyContent={props.justifyContent}
      alignItems={props.alignItems}
      alignSelf={props.alignSelf}
      alignContent={props.alignContent}
      width={props.width}
      height={props.height}
      minWidth={props.minWidth}
      minHeight={props.minHeight}
      maxWidth={props.maxWidth}
      maxHeight={props.maxHeight}
      gap={props.gap}
      columnGap={props.columnGap}
      rowGap={props.rowGap}
      display={props.display}
      padding={props.padding}
      paddingX={props.paddingX}
      paddingY={props.paddingY}
      paddingTop={props.paddingTop}
      paddingBottom={props.paddingBottom}
      paddingLeft={props.paddingLeft}
      paddingRight={props.paddingRight}
      margin={props.margin}
      marginX={props.marginX}
      marginY={props.marginY}
      marginTop={props.marginTop}
      marginBottom={props.marginBottom}
      marginLeft={props.marginLeft}
      marginRight={props.marginRight}
      borderStyle={props.borderStyle}
      borderColor={borderColor ? [borderColor] : undefined}
      borderTopColor={borderTopColor ? [borderTopColor] : undefined}
      borderBottomColor={borderBottomColor ? [borderBottomColor] : undefined}
      borderLeftColor={borderLeftColor ? [borderLeftColor] : undefined}
      borderRightColor={borderRightColor ? [borderRightColor] : undefined}
      borderTop={props.borderTop}
      borderBottom={props.borderBottom}
      borderLeft={props.borderLeft}
      borderRight={props.borderRight}
    >
      {props.children}
    </RsxBox>
  );
}
