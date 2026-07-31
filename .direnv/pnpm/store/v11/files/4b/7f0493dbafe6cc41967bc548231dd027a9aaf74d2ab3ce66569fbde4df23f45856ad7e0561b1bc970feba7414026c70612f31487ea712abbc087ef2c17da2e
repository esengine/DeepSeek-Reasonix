"use strict";
"use client";
/**
 * @license lucide-react v1.21.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */

import { forwardRef, useState, useEffect, createElement } from 'react';
import dynamicIconImports from './dynamicIconImports.mjs';
import Icon from './Icon.mjs';

const iconNames = Object.keys(dynamicIconImports);
async function getIconNode(name) {
  if (!(name in dynamicIconImports)) {
    throw new Error("[lucide-react]: Name in Lucide DynamicIcon not found");
  }
  const icon = await dynamicIconImports[name]();
  return icon.__iconNode;
}
const DynamicIcon = forwardRef(
  ({ name, fallback: Fallback, ...props }, ref) => {
    const [iconNode, setIconNode] = useState();
    useEffect(() => {
      getIconNode(name).then(setIconNode).catch((error) => {
        console.error(error);
      });
    }, [name]);
    if (iconNode == null) {
      if (Fallback == null) {
        return null;
      }
      return createElement(Fallback);
    }
    return createElement(Icon, {
      ref,
      ...props,
      iconNode
    });
  }
);

export { DynamicIcon as default, iconNames };
//# sourceMappingURL=DynamicIcon.mjs.map
