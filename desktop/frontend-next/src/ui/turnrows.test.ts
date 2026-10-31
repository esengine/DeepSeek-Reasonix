import { describe, expect, it } from "vitest";
import type { Item } from "../state/session_types";
import { foldStats, repeatedSpeakers, sameMembers, transcriptRows } from "./turnrows";

const me = (id: string, text: string, pending?: boolean): Item => ({ t: "user", id, text, pending });
const said = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const ran = (id: string): Item => ({ t: "tool", id, tool: { id, name: "bash" } as never, running: false, children: [] });

const shape = (items: Item[]) =>
  transcriptRows(items).map((r) => ("fold" in r ? `fold:${r.fold.length}` : "item" in r ? `${r.item.t}${r.activity ? `+${r.activity.length}` : ""}` : `work:${r.activity.length}`));

describe("what a turn looks like once it is laid out", () => {
  it("files each run of work under the sentence it followed", () => {
    expect(shape([me("u1", "go"), said("s1", "on it"), ran("t1"), ran("t2"), said("s2", "done")]))
      .toEqual(["user", "say+2", "say"]);
  });

  // A line typed into a running turn used to sit here from the moment it was
  // typed. It split the turn at that point, so every tool call that followed
  // was filed under an instruction the model had not read — the transcript
  // reporting work as done about a line that had not been sent.
  it("leaves a queued line out rather than splitting the turn it was typed into", () => {
    const withQueued = [me("u1", "go"), said("s1", "on it"), me("u2", "also this", true), ran("t1"), said("s2", "done")];
    expect(shape(withQueued)).toEqual(shape([me("u1", "go"), said("s1", "on it"), ran("t1"), said("s2", "done")]));
  });

  it("draws the line as soon as the kernel has taken it", () => {
    expect(shape([me("u1", "go"), said("s1", "on it"), me("u2", "also this")])).toEqual(["user", "say", "user"]);
  });

  // A model that calls tools with no answer yet still streams a message, often
  // with a blank thought. That message draws nothing, so work filed under it
  // would draw nothing either.
  it("files work under the sentence that is drawn, not a blank one before it", () => {
    const blank: Item = { t: "say", id: "s0", text: "", reasoning: "\n", done: true };
    expect(shape([me("u1", "go"), blank, ran("t1"), ran("t2"), said("s1", "found it"), ran("t3"), said("s2", "done")]))
      .toEqual(["user", "say", "say+3", "say"]);
  });

  // A turn the host ran — another agent's message, a group's turn, a schedule —
  // opens with its origin, not the person's line; its work is its own answer's.
  it("files a host-run turn's work under that turn's answer, not the one before", () => {
    const from = (id: string): Item => ({ t: "origin", id, origin: { kind: "group_turn" }, text: "" });
    expect(shape([from("o1"), said("s1", "first answer"), from("o2"), ran("t1"), said("s2", "second answer")]))
      .toEqual(["origin", "say", "origin", "say+1"]);
  });

  it("keeps work under the model that did it while no sentence has come yet", () => {
    const blank: Item = { t: "say", id: "s0", text: "", reasoning: "\n", done: true };
    expect(shape([me("u1", "go"), blank, ran("t1"), ran("t2")])).toEqual(["user", "say+2"]);
  });
});

describe("folding a turn's work under one line", () => {
  const folded = (items: Item[], running = false) =>
    transcriptRows(items, { running }).map((r) => ("fold" in r ? `fold(${r.fold.length})` : "item" in r ? r.item.t : "work"));

  // A card asking the person for something is not the turn's work: folded, it
  // would wait unseen for an answer nobody knew was owed.
  it("keeps a call that asks the person for something in view", () => {
    const asks: Item = { t: "tool", id: "t1", tool: { id: "t1", name: "request_secret" } as never, running: false, children: [] };
    expect(shape([me("u", "q"), asks, said("s1", "asked for it")])).toEqual(["user", "tool", "say"]);
    expect(folded([me("u", "q"), said("s0", "checking"), ran("t0"), asks, said("s1", "asked for it")])).toEqual(["user", "fold(1)", "tool", "say"]);
  });

  it("leaves only the last sentence outside, with the work and the earlier sentences folded", () => {
    expect(folded([me("u", "q"), said("s1", "let me look"), ran("t1"), said("s2", "found it"), ran("t2"), said("s3", "done")])).toEqual(["user", "fold(2)", "say"]);
  });

  it("folds nothing when the turn is just a question and its answer", () => {
    expect(folded([me("u", "q"), said("s1", "answer")])).toEqual(["user", "say"]);
  });

  it("leaves the turn alone while it is still being worked on, and folds it once it is done", () => {
    const items = [me("u", "q"), said("s1", "looking"), ran("t1"), said("s2", "so far")];
    expect(folded(items, true)).toEqual(["user", "say", "say"]);
    expect(folded(items, false)).toEqual(["user", "fold(1)", "say"]);
  });

  it("only the last turn of a running transcript stays open", () => {
    const items = [me("u1", "a"), said("a1", "x"), ran("ta"), said("a2", "done a"), me("u2", "b"), said("b1", "y"), ran("tb"), said("b2", "half")];
    expect(folded(items, true)).toEqual(["user", "fold(1)", "say", "user", "say", "say"]);
  });

  it("treats a steer as the start of what follows, and keeps it where it was said", () => {
    const steer: Item = { t: "user", id: "st", text: "also", steer: true };
    expect(folded([me("u", "q"), said("s1", "a"), ran("t1"), steer, said("s2", "b"), said("s3", "done")])).toEqual(["user", "say", "user", "fold(1)", "say"]);
  });

  it("counts calls and failures for the line that stands in for the work", () => {
    const rows = transcriptRows([me("u", "q"), said("s1", "a"), ran("t1"), ran("t2"), said("s2", "done")], { running: false });
    const fold = rows.find((r) => "fold" in r)!;
    expect("fold" in fold && foldStats(fold.fold, () => 0)).toEqual({ calls: 2, failures: 0, words: 1 });
  });

  it("keeps the last message that has words outside when the turn ends on thinking alone, as a cancelled turn does", () => {
    const thinking: Item = { t: "say", id: "th", text: "", reasoning: "weighing it", done: true };
    expect(folded([me("u", "q"), said("s1", "looking"), ran("t1"), said("s2", "the answer"), thinking])).toEqual(["user", "fold(1)", "say", "say"]);
  });

  it("folds nothing when the turn never got to words (cancelled while thinking, or failed before answering)", () => {
    const thinking: Item = { t: "say", id: "th", text: "", reasoning: "weighing it", done: true };
    expect(folded([me("u", "q"), ran("t1"), thinking])).toEqual(["user", "say"]);
    const notice: Item = { t: "notice", id: "n", level: "error", text: "request failed" } as never;
    expect(folded([me("u", "q"), ran("t1"), notice])).toEqual(["user", "work", "notice"]);
  });

  it("is off unless asked for, so an unconfigured transcript reads as it always did", () => {
    expect(transcriptRows([me("u", "q"), said("s1", "a"), ran("t1"), said("s2", "done")]).some((r) => "fold" in r)).toBe(false);
  });
});

describe("which sentences repeat a speaker already on screen", () => {
  const by = (id: string, model?: string): Item => ({ t: "say", id, text: id, done: true, model });
  const names = (items: Item[]) => [...repeatedSpeakers(items)];

  it("marks every sentence after the first until the user speaks", () => {
    expect(names([me("u1", "a"), by("s1"), ran("t1"), by("s2"), by("s3"), me("u2", "b"), by("s4")])).toEqual(["s2", "s3"]);
  });

  it("keeps the header where the model changes", () => {
    expect(names([me("u1", "a"), by("s1", "m1"), by("s2", "m2"), by("s3", "m2")])).toEqual(["s3"]);
  });

  it("does not let a queued line or an empty sentence reset or stand in", () => {
    expect(names([me("u1", "a"), by("s1"), me("q", "later", true), { t: "say", id: "e", text: "", done: false }, by("s2")])).toEqual(["s2"]);
  });
});

describe("keeping a set when nothing in it changed", () => {
  it("compares members, not references", () => {
    expect(sameMembers(new Set(["a", "b"]), new Set(["b", "a"]))).toBe(true);
    expect(sameMembers(new Set(["a"]), new Set(["a", "b"]))).toBe(false);
    expect(sameMembers(new Set(["a", "c"]), new Set(["a", "b"]))).toBe(false);
  });
});
it("keeps published action surfaces outside work disclosures",()=>{
 const item: Item={t:"tool",id:"surface",running:false,children:[],tool:{id:"call",name:"render_surface",readOnly:true,args:JSON.stringify({spec_version:1,body:[{kind:"button",label:"Continue",actionId:"continue"}]}),output:'{"nonblocking":true}'}};
 expect(transcriptRows([item])).toEqual([{item}]);
});
it("keeps a completed dashboard visible and leaves failed publications as tools", () => {
 const item: Item = { t: "tool", id: "dashboard", running: false, children: [], tool: { name: "render_surface", readOnly: true, args: JSON.stringify({ spec_version: 1, body: [{ kind: "dashboard", dataset: { categories: ["Jan"], series: [{ id: "sales", label: "Sales", values: [10] }] } } ] }), output: "Displayed" } };
 expect(transcriptRows([item])).toEqual([{ item }]);
 const failed = { ...item, tool: { ...item.tool, err: "refused" } };
 expect(transcriptRows([failed])).toEqual([{ activity: [failed] }]);
});
