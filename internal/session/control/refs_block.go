package control

import (
	"encoding/xml"
	"fmt"
	"strings"
)

func refAttr(name, value string) xml.Attr {
	return xml.Attr{Name: xml.Name{Local: name}, Value: value}
}

func appendRefBlock(b *strings.Builder, tag string, attrs []xml.Attr, body string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	encoder := xml.NewEncoder(b)
	_ = encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: tag}, Attr: attrs})
	_ = encoder.Flush()
	fmt.Fprintf(b, "\n%s\n</%s>", body, tag)
}
