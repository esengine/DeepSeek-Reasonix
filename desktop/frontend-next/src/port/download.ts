// Handing bytes to the browser. It lives beside the ports rather than inside
// one because it is not a way of reaching the kernel — it is what any port does
// when the host it runs in has no shell to write the file for it.
export function download(name: string, content: string, type = "application/json") {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
