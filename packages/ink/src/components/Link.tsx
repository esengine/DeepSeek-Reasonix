import React, { type ReactNode } from 'react';
import { supportsHyperlinks } from '../supports-hyperlinks.js';
import Text from './Text.js';

export type Props = {
  /** Visible link text. Defaults to `url` when omitted. */
  readonly children?: ReactNode;
  /** Target URL. Emitted verbatim into the OSC 8 escape. */
  readonly url: string;
  /** What to render when the terminal does not advertise OSC 8 support. */
  readonly fallback?: ReactNode;
};

/** Renders a clickable hyperlink using OSC 8 escapes. */
export default function Link({ children, url, fallback }: Props) {
  const content = children ?? url;

  if (supportsHyperlinks()) {
    return (
      <Text>
        <ink-link href={url}>{content}</ink-link>
      </Text>
    );
  }

  return <Text>{fallback ?? content}</Text>;
}
