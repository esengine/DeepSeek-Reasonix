// Package observe is the read-only posture an unattended run is built under.
//
// Three facts define it and each is a type here. The ceiling (Admits) is the
// only set of tools the run may hold, decided from what each tool declares
// about its own reach and never from its name. A Pending is the record of
// something that needed a person: it is parked for one and is never answered
// by the run, by a mode, or by a rule. Posture is what the run says about how
// its confinement is enforced, so a frontend can show that on a platform
// without an operating-system sandbox the tool set alone holds the line.
//
// Nothing here starts a process, reads configuration or touches the network;
// the controller and the boot assembly apply these types.
package observe
