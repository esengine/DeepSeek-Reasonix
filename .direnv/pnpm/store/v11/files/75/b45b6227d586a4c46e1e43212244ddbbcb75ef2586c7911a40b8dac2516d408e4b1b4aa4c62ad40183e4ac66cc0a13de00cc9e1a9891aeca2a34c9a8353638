import { produce } from 'immer';

const immerImpl = (initializer) => (set, get, store) => {
  store.setState = (updater, replace, ...args) => {
    const nextState = typeof updater === "function" ? produce(updater) : updater;
    return set(nextState, replace, ...args);
  };
  return initializer(store.setState, get, store);
};
const immer = immerImpl;

export { immer };
