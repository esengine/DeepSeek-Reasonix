// usePasteFiles.ts — what a paste carried, and what it becomes.
//
// A copied file and a copied path string are different clipboard payloads, and
// the page never tells them apart by reading the text: the shell reads the
// clipboard's own file formats (text/uri-list, FileNameW, public.file-url) and
// hands over the paths they name. A paste that carries a file then takes the
// route a drop takes, and the host decides what that file becomes — the same
// answer for a pasted file and a dropped one.
//
// Text is left alone. A path pasted as text is text, and the browser pastes it
// as it is.

import { useCallback, type ClipboardEvent } from "react";
import { host } from "../port/host";
import { pasteIsLong } from "./intake";

interface Options {
  // A turn is running. A paste during one is dropped: the box is read only
  // while the session waits.
  submitting: boolean;
  // The files a paste carried: the paths the shell named, and the files the
  // view held, for the ones the shell could not place.
  onFiles: (paths: string[], files: File[]) => void;
  // Text too long for the box, kept beside it instead of in it.
  onLongText: (body: string) => void;
}

export function usePasteFiles({ submitting, onFiles, onLongText }: Options) {
  return useCallback(
    (event: ClipboardEvent<HTMLTextAreaElement>) => {
      if (submitting) {
        event.preventDefault();
        return;
      }
      const data = event.clipboardData;
      const files = [...data.files];
      const body = data.getData("text/plain");
      // What the clipboard carries is the shell's answer to give: it reads the
      // formats that carry files, and never the text. The clipboard names what
      // was copied; the File the view holds may be a copy that belongs to the
      // source application's restricted or short-lived cache, and a path into
      // that cache is not the file the person copied.
      const carried = host().clipboardFiles().catch(() => [] as string[]);
      if (files.length > 0) {
        // The view has a file in hand, so this is a file paste, and its text —
        // the file's own path — does not go into the box.
        event.preventDefault();
        void carried.then((paths) => onFiles(paths, files));
        return;
      }
      // A clipboard that names a file is a file paste even where the view shows
      // none: a platform may hand the file over only as a named path, and put
      // that path in text/plain too. The reference must go in and the path text
      // must not — left alone, the browser pastes the path text beside the
      // reference, doubling it. The view advertises the file synchronously in
      // its own types: a file copy names text/uri-list, and Files where the
      // renderer also holds one. So the default paste is prevented here, before
      // the handler returns; the shell's paths still arrive on the async step.
      const types = data.types ?? [];
      const viewNamesFile = types.includes("text/uri-list") || types.includes("Files");
      void carried.then((paths) => {
        if (paths.length > 0) onFiles(paths, files);
      });
      if (viewNamesFile) {
        event.preventDefault();
        return;
      }
      if (!pasteIsLong(body)) return;
      // Long enough to bury the composer. It still goes with the turn — it is
      // just held beside the box instead of becoming it.
      event.preventDefault();
      onLongText(body);
    },
    [submitting, onFiles, onLongText],
  );
}
