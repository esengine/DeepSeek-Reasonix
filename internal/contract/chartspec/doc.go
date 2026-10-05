// Package chartspec is the closed, versioned description of a chart a model may
// ask a frontend to draw. It owns one verdict, Parse, so every frontend and the
// tool that accepts a spec agree on what is valid, and one deterministic text
// projection, Summary, which is all the model ever reads back.
//
// A spec carries data and a choice among a fixed set of marks. It cannot express
// code, URLs, markup, colours or formats: unknown fields are rejected rather than
// ignored, so a later version's field is never half-honoured by this one.
//
// Every refusal is an *Error with a Code; callers branch on errors.Is against the
// sentinels, never on text.
package chartspec
