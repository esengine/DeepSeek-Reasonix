'use strict';

var React = require('react');
var shallow = require('zustand/vanilla/shallow');

function useShallow(selector) {
  const prev = React.useRef(void 0);
  return (state) => {
    const next = selector(state);
    return shallow.shallow(prev.current, next) ? prev.current : prev.current = next;
  };
}

exports.useShallow = useShallow;
