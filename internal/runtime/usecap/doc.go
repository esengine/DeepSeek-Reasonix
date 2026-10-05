// Package usecap is the use_capability proxy: the one fixed tool through which
// a model reaches MCP servers, skills and dispatch-only tools without their
// schemas entering the request, and the session runtime that connects MCP
// servers on demand and shares their processes across agents. Each agent gets
// its own frontend, so ledgers and audits stay separate.
package usecap
