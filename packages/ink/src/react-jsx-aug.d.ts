/** JSX intrinsic-element augmentation for the React module. */

import 'react';

declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      'ink-box': Record<string, unknown>;
      'ink-text': Record<string, unknown>;
      'ink-link': Record<string, unknown>;
      'ink-raw-ansi': Record<string, unknown>;
    }
  }
}
