'use strict';

var React = require('react');
var useSyncExternalStoreExports = require('use-sync-external-store/shim/with-selector');
var vanilla = require('zustand/vanilla');

const { useSyncExternalStoreWithSelector } = useSyncExternalStoreExports;
const identity = (arg) => arg;
function useStoreWithEqualityFn(api, selector = identity, equalityFn) {
  const slice = useSyncExternalStoreWithSelector(
    api.subscribe,
    api.getState,
    api.getInitialState,
    selector,
    equalityFn
  );
  React.useDebugValue(slice);
  return slice;
}
const createWithEqualityFnImpl = (createState, defaultEqualityFn) => {
  const api = vanilla.createStore(createState);
  const useBoundStoreWithEqualityFn = (selector, equalityFn = defaultEqualityFn) => useStoreWithEqualityFn(api, selector, equalityFn);
  Object.assign(useBoundStoreWithEqualityFn, api);
  return useBoundStoreWithEqualityFn;
};
const createWithEqualityFn = ((createState, defaultEqualityFn) => createState ? createWithEqualityFnImpl(createState, defaultEqualityFn) : createWithEqualityFnImpl);

exports.createWithEqualityFn = createWithEqualityFn;
exports.useStoreWithEqualityFn = useStoreWithEqualityFn;
