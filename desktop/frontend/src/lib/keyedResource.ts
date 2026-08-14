export type KeyedResourceStatus = "idle" | "refreshing" | "error";

export type KeyedResource<T> = {
  key: string | null;
  data: T | null;
  status: KeyedResourceStatus;
  requestSeq: number;
  revision: number;
  error: string;
};

export function emptyKeyedResource<T>(): KeyedResource<T> {
  return { key: null, data: null, status: "idle", requestSeq: 0, revision: 0, error: "" };
}

export function beginKeyedResourceRequest<T>(
  current: KeyedResource<T>,
  key: string,
  requestSeq: number,
  revision = current.revision,
): KeyedResource<T> {
  const sameKey = current.key === key;
  return {
    key,
    data: sameKey ? current.data : null,
    status: "refreshing",
    requestSeq,
    revision,
    error: "",
  };
}

export function resolveKeyedResourceRequest<T>(
  current: KeyedResource<T>,
  key: string,
  requestSeq: number,
  data: T,
  revision = current.revision,
): KeyedResource<T> {
  if (current.key !== key || current.requestSeq !== requestSeq) return current;
  return { ...current, data, status: "idle", revision, error: "" };
}

export function rejectKeyedResourceRequest<T>(
  current: KeyedResource<T>,
  key: string,
  requestSeq: number,
  error: string,
): KeyedResource<T> {
  if (current.key !== key || current.requestSeq !== requestSeq) return current;
  return { ...current, status: "error", error };
}
