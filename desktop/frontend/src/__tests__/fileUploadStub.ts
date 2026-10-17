type Checks = {
  ok(value: boolean, label: string): void;
  eq(actual: unknown, expected: unknown, label: string): void;
};

export function createFileUploadMock(target: string, { ok, eq }: Checks) {
  return (upload: (file: File) => Promise<string>) => {
    const previous = globalThis.fetch;
    globalThis.fetch = async (input, init = {}) => {
      const url = new URL(String(input), "http://localhost/");
      eq(url.pathname, "/__reasonix_attachment_upload", "ordinary file uses the raw upload endpoint");
      eq(url.searchParams.get("target"), target, "upload carries the source attachment target");
      eq(init.method, "POST", "ordinary file upload uses POST");
      ok(init.body instanceof File, "ordinary file upload sends the File directly");
      eq(new Headers(init.headers).get("content-type"), "application/octet-stream", "ordinary file upload uses raw binary content type");
      const file = init.body as File;
      eq(url.searchParams.get("name"), file.name, "upload preserves the filename");
      const path = await upload(file);
      return new Response(JSON.stringify({ path }), { status: 201, headers: { "content-type": "application/json" } });
    };
    return () => { globalThis.fetch = previous; };
  };
}

export function dispatchPasteFile(textarea: HTMLTextAreaElement, file: File) {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    configurable: true,
    value: { files: [file], items: [], types: ["Files"], getData: () => "" },
  });
  textarea.dispatchEvent(event);
}
