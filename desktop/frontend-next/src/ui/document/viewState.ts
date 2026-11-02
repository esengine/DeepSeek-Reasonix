export interface DocumentViewState {
  top?: number;
  left?: number;
  page?: number;
  zoom?: number;
  sheet?: number;
}

export function documentState(states: Map<string, DocumentViewState>, path: string): DocumentViewState {
  let state = states.get(path);
  if (!state) { state = {}; states.set(path, state); }
  return state;
}
