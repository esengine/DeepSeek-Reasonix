/** One message in both languages, side by side, so neither can be edited
 *  without the other in view. */
export interface Message {
  zh: string;
  en: string;
}

export type Messages = Record<string, Message>;
