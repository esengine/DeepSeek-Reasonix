// Package catalog asks an endpoint what it serves: the models a configured
// provider lists, and, for an address nobody has described yet, which protocol
// its listing answers to. It reaches the network through model/openai's fetch
// path, so a probe that succeeds proves the call a saved provider will make.
// Configuration only describes providers; this is where they are asked.
package catalog
