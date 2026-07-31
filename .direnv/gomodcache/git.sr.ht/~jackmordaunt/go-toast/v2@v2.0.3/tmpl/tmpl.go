// Package tmpl wraps several templates that are used to generate notifications.
//
// The primary template describes the XML structure that the Windows Runtime expects
// to consume. The powershell template describes a script that we can use to execute
// the notification if the Windows Runtime is unavailable.
//
// For more information about the xml schema:
// https://learn.microsoft.com/en-us/uwp/schemas/tiles/toastschema/schema-root
package tmpl

import (
	_ "embed"
	"text/template"
)

//go:embed xml.go.tmpl
var xml string

//go:embed powershell.go.tmpl
var powershell string

// XMLTemplate describes the XML content that the Windows Runtime uses to build
// toast notifications.
var XMLTemplate = template.Must(template.New("toast-xml").Parse(xml))

// ScriptTemplate describes the Powershell script that will invoke a toast notification
// given some XML.
var ScriptTemplate = template.Must(template.New("script").Parse(powershell))
