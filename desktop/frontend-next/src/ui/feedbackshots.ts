import type { FeedbackImage, FeedbackLimits } from "../port/feedback";

export const SHOT_TYPES = ["image/png", "image/jpeg"] as const;

export type ShotRefusal = "format" | "too_large" | "too_many";

export interface Shot extends FeedbackImage {
  id: string;
  contentType: string;
  bytes: number;
  preview: string;
}

export interface Refused {
  name: string;
  why: ShotRefusal;
}

export function isShotType(type: string): boolean {
  return (SHOT_TYPES as readonly string[]).includes(type);
}

export function admit(held: number, files: File[], limits: Pick<FeedbackLimits, "images" | "uploadBytes">): { taken: File[]; refused: Refused[] } {
  const taken: File[] = [];
  const refused: Refused[] = [];
  for (const file of files) {
    if (!isShotType(file.type)) refused.push({ name: file.name, why: "format" });
    else if (file.size > limits.uploadBytes) refused.push({ name: file.name, why: "too_large" });
    else if (held + taken.length >= limits.images) refused.push({ name: file.name, why: "too_many" });
    else taken.push(file);
  }
  return { taken, refused };
}

let serial = 0;

export function readShot(file: File): Promise<Shot> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.onload = () => {
      const url = String(reader.result);
      resolve({
        id: `shot-${++serial}`,
        name: file.name || (file.type === "image/png" ? "screenshot.png" : "screenshot.jpg"),
        contentType: file.type,
        dataBase64: url.slice(url.indexOf(",") + 1),
        bytes: file.size,
        preview: url,
      });
    };
    reader.readAsDataURL(file);
  });
}

export function payload(shots: Shot[]): FeedbackImage[] {
  return shots.map(({ name, dataBase64 }) => ({ name, dataBase64 }));
}

export function megabytes(bytes: number): string {
  const mb = bytes / (1 << 20);
  return Number.isInteger(mb) ? String(mb) : mb.toFixed(1);
}
